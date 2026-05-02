// Package world 管理世界状态和 Tick 驱动的模拟循环。
package world

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/claw-works/fable/internal/agent"
	"github.com/claw-works/fable/internal/llm"
	"github.com/claw-works/fable/internal/schema"
	"github.com/claw-works/fable/internal/storage"
	"gopkg.in/yaml.v3"
)

// StreamEvent 是流式推送的增量事件。
type StreamEvent struct {
	Type       string              `json:"type"`                  // "agent_update" | "event"
	AgentState *schema.AgentState  `json:"agent_state,omitempty"`
	Text       string              `json:"text,omitempty"`
	GameTime   string              `json:"game_time"`
	Tick       int                 `json:"tick"`
}

// World 表示整个模拟世界。
type World struct {
	mu            sync.RWMutex
	WorldID       string
	WorldDir      string
	SaveName      string
	Manifest      *schema.WorldManifest
	Config        schema.WorldConfig
	Agents        map[string]*agent.Agent
	State         schema.WorldState
	History       []schema.WorldState
	Snapshots     map[int]map[string]schema.AgentSnapshot // tick -> agentID -> snapshot
	OnTick        func(schema.WorldState)       // tick 完成后推送完整状态
	OnEvent       func(StreamEvent)             // 增量事件实时推送
	Player        *schema.PlayerConfig
	PlayerState   *schema.PlayerState
	PendingAction *schema.PlayerAction
	Mode          schema.SimulationMode
	Conversation  *schema.ConversationSession
	Autopilot     bool              // 托管模式：LLM 替玩家决策
	llm           *llm.Client
}

// Load 从指定目录加载世界配置和角色配置。
func Load(dir string, saveName string, llmClient *llm.Client) (*World, error) {
	wData, err := os.ReadFile(dir + "/world.yaml")
	if err != nil {
		return nil, fmt.Errorf("read world.yaml: %w", err)
	}
	var wCfg schema.WorldConfig
	if err := yaml.Unmarshal(wData, &wCfg); err != nil {
		return nil, fmt.Errorf("parse world.yaml: %w", err)
	}

	aData, err := os.ReadFile(dir + "/agents.yaml")
	if err != nil {
		return nil, fmt.Errorf("read agents.yaml: %w", err)
	}
	var aFile schema.AgentsFile
	if err := yaml.Unmarshal(aData, &aFile); err != nil {
		return nil, fmt.Errorf("parse agents.yaml: %w", err)
	}

	agents := make(map[string]*agent.Agent, len(aFile.Agents))
	locations := make(map[string][]string)
	for _, cfg := range aFile.Agents {
		agents[cfg.ID] = agent.New(cfg, llmClient)
		locations[cfg.InitLocation] = append(locations[cfg.InitLocation], cfg.ID)
	}

	// 可选：加载 manifest.yaml
	var manifest *schema.WorldManifest
	if mData, err := os.ReadFile(dir + "/manifest.yaml"); err == nil {
		var m schema.WorldManifest
		if err := yaml.Unmarshal(mData, &m); err != nil {
			log.Printf("warn: parse manifest.yaml: %v", err)
		} else {
			manifest = &m
		}
	}

	worldID := filepath.Base(dir)

	// 构建初始 Agent 状态列表
	initAgents := make([]schema.AgentState, 0, len(agents))
	for _, ag := range agents {
		initAgents = append(initAgents, ag.State)
	}

	w := &World{
		WorldID:   worldID,
		WorldDir:  dir,
		SaveName:  saveName,
		Manifest:  manifest,
		Config:    wCfg,
		Agents:    agents,
		Mode:      schema.ModeIdle,
		Snapshots: make(map[int]map[string]schema.AgentSnapshot),
		llm:       llmClient,
		State: schema.WorldState{
			Tick:      0,
			GameTime:  "Day1 08:00",
			Locations: locations,
			Agents:    initAgents,
		},
	}

	// 尝试恢复存档
	if saved, err := storage.LoadLatestSave(); err == nil {
		w.State = saved.State
		w.Player = saved.Player
		w.PlayerState = saved.PlayerState
		if w.Player != nil && w.PlayerState != nil && w.PlayerState.Name == "" {
			w.PlayerState.Name = w.Player.Name
		}
		for _, a := range saved.State.Agents {
			if ag, ok := w.Agents[a.AgentID]; ok {
				ag.State = a
			}
		}
		log.Printf("[world] 已恢复存档: tick=%d, time=%s", saved.State.Tick, saved.State.GameTime)
	}

	return w, nil
}

// TickResponse 是 Tick 返回给调用方的结构。
type TickResponse struct {
	State            schema.WorldState `json:"state"`
	WaitingForPlayer bool              `json:"waiting_for_player"`
	PlayerState      *schema.PlayerState `json:"player_state,omitempty"`
}

// Tick 执行一次世界更新。NPC 并行推理，每个完成后立即推送。
func (w *World) Tick(ctx context.Context) TickResponse {
	log.Println("[tick] 尝试获取锁...")
	w.mu.Lock()
	log.Println("[tick] 已获取锁")

	w.State.Tick++
	w.State.GameTime = w.advanceTime(w.State.GameTime)
	log.Printf("[tick %d] 时间推进到 %s", w.State.Tick, w.State.GameTime)

	// 通知前端 tick 开始
	if w.OnEvent != nil {
		w.OnEvent(StreamEvent{Type: "tick_start", GameTime: w.State.GameTime, Tick: w.State.Tick})
	}

	// 处理玩家行动（有就处理，没有就跳过）
	var playerEvent string
	if w.Player != nil && w.PendingAction != nil {
		playerEvent = w.applyPlayerAction()
		w.PendingAction = nil
		log.Printf("[tick %d] 玩家行动: %s", w.State.Tick, playerEvent)
	} else if w.Player != nil && w.Autopilot {
		// 托管模式：LLM 替玩家决策
		if act, err := w.autopilotDecide(ctx); err != nil {
			log.Printf("[tick %d] 托管决策失败: %v", w.State.Tick, err)
		} else {
			w.PendingAction = act
			playerEvent = w.applyPlayerAction()
			w.PendingAction = nil
			log.Printf("[tick %d] 托管行动: %s", w.State.Tick, playerEvent)
		}
	} else if w.Player != nil {
		log.Printf("[tick %d] 玩家在场但无行动，继续推理", w.State.Tick)
	}

	// 快照当前状态供 NPC 推理用
	snapshot := w.State
	if playerEvent != "" {
		snapshot.Events = append(append([]string{}, snapshot.Events...), playerEvent)
	}
	agentList := make([]*agent.Agent, 0, len(w.Agents))
	agentIDs := make([]string, 0, len(w.Agents))
	for id, a := range w.Agents {
		agentList = append(agentList, a)
		agentIDs = append(agentIDs, id)
	}
	onEvent := w.OnEvent
	playerState := w.PlayerState
	player := w.Player

	// 释放锁，开始并行推理（不持锁）
	w.mu.Unlock()
	log.Printf("[tick %d] 开始并行推理，共 %d 个 NPC", snapshot.Tick, len(agentList))

	// 立即推送玩家事件
	if playerEvent != "" && onEvent != nil {
		onEvent(StreamEvent{Type: "event", Text: playerEvent, GameTime: snapshot.GameTime, Tick: snapshot.Tick})
		// 同时推送结构化的玩家状态，让前端能显示行动/对话/位置
		if playerState != nil {
			ps := &schema.AgentState{
				AgentID:  "player",
				Name:     player.Name,
				Tick:     snapshot.Tick,
				GameTime: snapshot.GameTime,
				Location: playerState.Location,
				Action:   playerState.Action,
				Target:   playerState.Target,
				Dialogue: playerState.Dialogue,
				Emotion:  "—",
			}
			onEvent(StreamEvent{Type: "agent_update", AgentState: ps, GameTime: snapshot.GameTime, Tick: snapshot.Tick})
		}
	}

	// 并行推理
	type agentResult struct {
		id    string
		state schema.AgentState
	}
	results := make(chan agentResult, len(agentList))
	for i, a := range agentList {
		go func(id string, ag *agent.Agent) {
			log.Printf("[tick %d] NPC %s 开始推理...", snapshot.Tick, id)
			state, err := ag.Think(ctx, snapshot)
			if err != nil {
				log.Printf("[tick %d] NPC %s 推理失败: %v", snapshot.Tick, id, err)
				state = ag.State
				state.Tick = snapshot.Tick
				state.GameTime = snapshot.GameTime
			} else {
				log.Printf("[tick %d] NPC %s 推理完成: %s %s", snapshot.Tick, id, state.Location, state.Action)
			}
			results <- agentResult{id: id, state: state}
			// 立即推送这个 NPC 的结果
			if onEvent != nil {
				onEvent(StreamEvent{
					Type:       "agent_update",
					AgentState: &state,
					GameTime:   snapshot.GameTime,
					Tick:       snapshot.Tick,
				})
			}
		}(agentIDs[i], a)
	}

	// 收集所有结果
	var agentStates []schema.AgentState
	var events []string
	if playerEvent != "" {
		events = append(events, playerEvent)
	}
	locations := make(map[string][]string)
	if playerState != nil && player != nil {
		locations[playerState.Location] = append(locations[playerState.Location], player.Name)
	}

	for range agentList {
		r := <-results
		agentStates = append(agentStates, r.state)
		agName := r.state.Name
		locations[r.state.Location] = append(locations[r.state.Location], agName)
		if r.state.Dialogue != nil {
			if ag, ok := w.Agents[r.id]; ok {
				events = append(events, fmt.Sprintf("%s 说：%s", ag.Config.Name, *r.state.Dialogue))
			}
		}
		if ag, ok := w.Agents[r.id]; ok {
			events = append(events, fmt.Sprintf("%s 在%s%s", ag.Config.Name, r.state.Location, r.state.Action))
		}
	}

	// 重新加锁，更新最终状态
	w.mu.Lock()
	w.State.Agents = agentStates
	w.State.Locations = locations
	w.State.Events = events
	w.History = append(w.History, w.State)

	// 保存快照
	snapMap := make(map[string]schema.AgentSnapshot, len(w.Agents))
	for id, a := range w.Agents {
		snapMap[id] = a.Snapshot()
	}
	w.Snapshots[w.State.Tick] = snapMap

	finalState := w.State

	// 检测是否有 NPC 对玩家说话 → 暂停等待玩家响应
	if w.Player != nil && w.Mode == schema.ModeRunning {
		for _, a := range agentStates {
			if a.Target != nil && (*a.Target == w.Player.Name) && a.Dialogue != nil {
				w.Mode = schema.ModePaused
				log.Printf("[tick %d] NPC %s 对玩家说话，世界暂停等待响应", w.State.Tick, a.AgentID)
				break
			}
		}
	}

	ps := w.PlayerState
	w.mu.Unlock()

	// 推送完整状态（tick 结束）
	if w.OnEvent != nil {
		w.OnEvent(StreamEvent{Type: "tick_end", GameTime: finalState.GameTime, Tick: finalState.Tick})
	}
	if w.OnTick != nil {
		w.OnTick(finalState)
	}

	// 自动存档
	saveData := schema.SaveData{State: finalState, Player: w.Player, PlayerState: ps}
	if err := storage.SaveTick(saveData); err != nil {
		log.Printf("[tick %d] 存档失败: %v", finalState.Tick, err)
	}

	// 反思：每 N 个 tick 触发一次，等待全部完成后再返回
	if agent.ShouldReflect(finalState.Tick) {
		log.Printf("[tick %d] 触发反思...", finalState.Tick)
		var wg sync.WaitGroup
		for id, a := range w.Agents {
			wg.Add(1)
			go func(id string, a *agent.Agent) {
				defer wg.Done()
				rctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
				defer cancel()
				if err := a.Reflect(rctx, finalState); err != nil {
					log.Printf("[tick %d] NPC %s 反思失败: %v", finalState.Tick, id, err)
				} else {
					log.Printf("[tick %d] NPC %s 反思完成", finalState.Tick, id)
				}
			}(id, a)
		}
		wg.Wait()
		log.Printf("[tick %d] 全部反思完成", finalState.Tick)
	}

	return TickResponse{State: finalState, PlayerState: ps}
}

// applyPlayerAction 执行玩家行动，返回事件描述。
func (w *World) applyPlayerAction() string {
	act := w.PendingAction
	ps := w.PlayerState
	ps.Tick = w.State.Tick
	ps.GameTime = w.State.GameTime

	switch act.Type {
	case "move":
		if act.Location != nil {
			ps.Location = *act.Location
			ps.Action = "移动到" + *act.Location
			return fmt.Sprintf("【玩家】%s 来到了%s", w.Player.Name, *act.Location)
		}
	case "talk":
		ps.Action = "与人交谈"
		ps.Target = act.Target
		ps.Dialogue = &act.Content
		targetName := ""
		if act.Target != nil {
			targetName = *act.Target
			// 尝试把 ID 转为名字
			if a, ok := w.Agents[targetName]; ok {
				targetName = a.Config.Name
			}
		}
		return fmt.Sprintf("【玩家】%s 对 %s 说：%s", w.Player.Name, targetName, act.Content)
	case "act":
		ps.Action = act.Content
		return fmt.Sprintf("【玩家】%s %s", w.Player.Name, act.Content)
	case "skip":
		ps.Action = "静静观察"
		return ""
	}
	return ""
}

// SubmitAction 提交玩家行动。如果世界暂停中，自动恢复运行。
func (w *World) SubmitAction(action schema.PlayerAction) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.PendingAction = &action
	if w.Mode == schema.ModePaused {
		w.Mode = schema.ModeRunning
		log.Printf("[world] 玩家提交行动，恢复运行")
	}
}

// GetMode 返回当前运行模式。
func (w *World) GetMode() schema.SimulationMode {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.Mode
}

// SetMode 设置运行模式。
func (w *World) SetMode(m schema.SimulationMode) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.Mode = m
}

// JoinPlayer 玩家加入世界。
func (w *World) JoinPlayer(cfg schema.PlayerConfig) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.Player = &cfg
	w.PlayerState = &schema.PlayerState{
		PlayerID: cfg.ID,
		Name:     cfg.Name,
		Tick:     w.State.Tick,
		GameTime: w.State.GameTime,
		Location: cfg.InitLocation,
		Action:   "刚刚到达",
	}
}

// LeavePlayer 玩家离开世界。
func (w *World) LeavePlayer() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.Player = nil
	w.PlayerState = nil
	w.PendingAction = nil
}

// GetState 返回当前世界状态的副本。
func (w *World) GetState() schema.WorldState {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.State
}

// advanceTime 将游戏时间推进 TimeStep 分钟。
func (w *World) advanceTime(current string) string {
	var day, hour, min int
	fmt.Sscanf(current, "Day%d %d:%d", &day, &hour, &min)
	min += w.Config.TimeStep
	for min >= 60 {
		min -= 60
		hour++
	}
	for hour >= 24 {
		hour -= 24
		day++
	}
	return fmt.Sprintf("Day%d %02d:%02d", day, hour, min)
}

// autopilotDecide 用 LLM 替玩家生成行动。
func (w *World) autopilotDecide(ctx context.Context) (*schema.PlayerAction, error) {
	p := w.Player
	ps := w.PlayerState

	// 收集同地点的人
	var nearby []string
	if ids, ok := w.State.Locations[ps.Location]; ok {
		for _, id := range ids {
			if id != p.ID && id != p.Name {
				nearby = append(nearby, id)
			}
		}
	}

	// 收集可去的地点
	var locs []string
	for _, loc := range w.Config.Locations {
		locs = append(locs, loc.Name)
	}

	prompt := fmt.Sprintf(
		"当前时间：%s\n你现在在：%s\n周围的人：%s\n可去的地点：%s\n最近发生的事：\n",
		w.State.GameTime, ps.Location,
		strings.Join(nearby, "、"),
		strings.Join(locs, "、"),
	)
	for _, e := range w.State.Events {
		prompt += "- " + e + "\n"
	}

	messages := []llm.Message{
		{Role: "system", Content: fmt.Sprintf(
			"你是%s，%s，性格：%s。背景：%s\n"+
				"请决定下一步行动。以 JSON 回复，包含字段："+
				"type(\"move\"/\"talk\"/\"act\"/\"skip\")，"+
				"location(move时目标地点)，target(talk时目标人名)，content(talk/act的内容)。",
			p.Name, p.Occupation, p.Personality, p.Backstory,
		)},
		{Role: "user", Content: prompt},
	}

	resp, err := w.llm.ChatJSON(ctx, messages)
	if err != nil {
		return nil, err
	}

	var result struct {
		Type     string  `json:"type"`
		Location *string `json:"location,omitempty"`
		Target   *string `json:"target,omitempty"`
		Content  string  `json:"content"`
	}
	if err := json.Unmarshal([]byte(resp), &result); err != nil {
		return nil, fmt.Errorf("parse autopilot: %w", err)
	}

	return &schema.PlayerAction{
		Type:     result.Type,
		Location: result.Location,
		Target:   result.Target,
		Content:  result.Content,
	}, nil
}

// RestoreToTick 恢复到指定 tick 的快照状态。
func (w *World) RestoreToTick(tick int) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	snapMap, ok := w.Snapshots[tick]
	if !ok {
		return fmt.Errorf("tick %d 没有快照", tick)
	}

	// 恢复每个 Agent 的状态
	for id, snap := range snapMap {
		if a, ok := w.Agents[id]; ok {
			a.RestoreSnapshot(snap)
		}
	}

	// 恢复世界状态到该 tick 的历史记录
	for _, h := range w.History {
		if h.Tick == tick {
			w.State = h
			break
		}
	}

	// 截断历史到该 tick
	for i, h := range w.History {
		if h.Tick > tick {
			w.History = w.History[:i]
			break
		}
	}

	log.Printf("[world] 已恢复到 tick %d", tick)
	return nil
}

// GetSnapshotTicks 返回所有有快照的 tick 列表。
func (w *World) GetSnapshotTicks() []int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	ticks := make([]int, 0, len(w.Snapshots))
	for t := range w.Snapshots {
		ticks = append(ticks, t)
	}
	return ticks
}

// FindPath 用 BFS 找两点间最短路径，返回路径和总 Tick 消耗。
func (w *World) FindPath(from, to string) ([]string, int) {
	if from == to {
		return []string{from}, 0
	}
	// 构建邻接表
	adj := make(map[string][]schema.Connection)
	for _, loc := range w.Config.Locations {
		adj[loc.Name] = loc.Connected
	}

	type node struct {
		name string
		path []string
		cost int
	}
	visited := map[string]bool{from: true}
	queue := []node{{name: from, path: []string{from}, cost: 0}}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, conn := range adj[cur.name] {
			if conn.Target == to {
				return append(cur.path, to), cur.cost + conn.Distance
			}
			if !visited[conn.Target] {
				visited[conn.Target] = true
				queue = append(queue, node{
					name: conn.Target,
					path: append(append([]string{}, cur.path...), conn.Target),
					cost: cur.cost + conn.Distance,
				})
			}
		}
	}
	return nil, 0
}

// StartConversation 开始玩家和 NPC 的对话，切换到 slow 模式。
func (w *World) StartConversation(playerID, npcID string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.Conversation != nil && w.Conversation.Active {
		return fmt.Errorf("已有进行中的对话")
	}
	// 检查 NPC 是否存在
	if _, ok := w.Agents[npcID]; !ok {
		return fmt.Errorf("NPC %s 不存在", npcID)
	}
	// 检查玩家和 NPC 是否在同一地点
	if w.PlayerState != nil {
		npcLoc := w.Agents[npcID].State.Location
		if w.PlayerState.Location != npcLoc {
			return fmt.Errorf("你不在 %s 所在的地点", npcID)
		}
	}
	w.Conversation = &schema.ConversationSession{
		PlayerID:  playerID,
		NPCid:     npcID,
		StartTick: w.State.Tick,
		Active:    true,
	}
	w.Mode = schema.ModePaused
	return nil
}

// AddConversationTurn 添加一轮对话。
func (w *World) AddConversationTurn(speaker, content string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.Conversation == nil || !w.Conversation.Active {
		return
	}
	w.Conversation.History = append(w.Conversation.History, schema.ConversationTurn{
		Speaker: speaker,
		Content: content,
		Tick:    w.State.Tick,
	})
}

// EndConversation 结束对话，摘要注入当前 Tick 事件，恢复正常模式。
func (w *World) EndConversation() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.Conversation == nil || !w.Conversation.Active {
		return ""
	}
	// 生成摘要
	npcName := w.Conversation.NPCid
	if a, ok := w.Agents[w.Conversation.NPCid]; ok {
		npcName = a.Config.Name
	}
	summary := fmt.Sprintf("玩家与%s进行了一段对话（共%d轮）", npcName, len(w.Conversation.History))
	w.State.Events = append(w.State.Events, summary)
	w.Conversation.Active = false
	w.Mode = schema.ModeRunning
	return summary
}

// ConversationReply 让 NPC 用 LLM 回复对话，返回回复内容。
func (w *World) ConversationReply(ctx context.Context) (string, error) {
	w.mu.RLock()
	conv := w.Conversation
	if conv == nil || !conv.Active {
		w.mu.RUnlock()
		return "", fmt.Errorf("没有进行中的对话")
	}
	ag, ok := w.Agents[conv.NPCid]
	if !ok {
		w.mu.RUnlock()
		return "", fmt.Errorf("NPC %s 不存在", conv.NPCid)
	}
	// 构建对话 prompt
	var historyText string
	for _, t := range conv.History {
		if t.Speaker == "player" {
			historyText += fmt.Sprintf("玩家：%s\n", t.Content)
		} else {
			historyText += fmt.Sprintf("%s：%s\n", ag.Config.Name, t.Content)
		}
	}
	w.mu.RUnlock()

	messages := []llm.Message{
		{Role: "system", Content: fmt.Sprintf(
			"你是%s，%s，性格：%s。背景：%s\n你正在和一个来到小镇的旅人对话。请用简短自然的口语回复，保持角色性格。只回复对话内容，不要加任何格式。",
			ag.Config.Name, ag.Config.Occupation, ag.Config.Personality, ag.Config.Backstory,
		)},
		{Role: "user", Content: fmt.Sprintf("以下是对话记录：\n%s\n请以%s的身份回复：", historyText, ag.Config.Name)},
	}

	reply, err := ag.LLMClient().Chat(ctx, messages)
	if err != nil {
		return "", err
	}

	w.AddConversationTurn(conv.NPCid, reply)
	return reply, nil
}
