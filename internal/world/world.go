// Package world 管理世界状态和 Tick 驱动的模拟循环。
package world

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"

	"github.com/claw-works/fable/internal/agent"
	"github.com/claw-works/fable/internal/llm"
	"github.com/claw-works/fable/internal/schema"
	"gopkg.in/yaml.v3"
)

// World 表示整个模拟世界。
type World struct {
	mu            sync.RWMutex
	Manifest      *schema.WorldManifest  // 可选，来自 manifest.yaml
	Config        schema.WorldConfig
	Agents        map[string]*agent.Agent
	State         schema.WorldState
	History       []schema.WorldState
	OnTick        func(schema.WorldState) // Tick 完成后的回调（用于推送 WebSocket）
	Player        *schema.PlayerConfig    // nil 表示旁观模式
	PlayerState   *schema.PlayerState
	PendingAction *schema.PlayerAction    // 等待玩家输入的 Tick
}

// Load 从指定目录加载世界配置和角色配置。
func Load(dir string, llmClient *llm.Client) (*World, error) {
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

	return &World{
		Manifest: manifest,
		Config:   wCfg,
		Agents: agents,
		State: schema.WorldState{
			Tick:      0,
			GameTime:  "Day1 08:00",
			Locations: locations,
		},
	}, nil
}

// TickResponse 是 Tick 返回给调用方的结构。
type TickResponse struct {
	State            schema.WorldState `json:"state"`
	WaitingForPlayer bool              `json:"waiting_for_player"`
	PlayerState      *schema.PlayerState `json:"player_state,omitempty"`
}

// Tick 执行一次世界更新。
func (w *World) Tick(ctx context.Context) TickResponse {
	w.mu.Lock()
	defer w.mu.Unlock()

	// 玩家模式：若无待处理行动则暂停等待
	if w.Player != nil && w.PendingAction == nil {
		return TickResponse{State: w.State, WaitingForPlayer: true, PlayerState: w.PlayerState}
	}

	w.State.Tick++
	w.State.GameTime = w.advanceTime(w.State.GameTime)

	// 处理玩家行动
	var playerEvent string
	if w.Player != nil && w.PendingAction != nil {
		playerEvent = w.applyPlayerAction()
		w.PendingAction = nil
	}

	var agentStates []schema.AgentState
	var events []string
	if playerEvent != "" {
		events = append(events, playerEvent)
	}
	locations := make(map[string][]string)

	// 将玩家加入地点
	if w.PlayerState != nil {
		locations[w.PlayerState.Location] = append(locations[w.PlayerState.Location], w.Player.ID)
	}

	for id, a := range w.Agents {
		// 将玩家行为注入 prompt：临时添加到世界事件中
		origEvents := w.State.Events
		if playerEvent != "" {
			w.State.Events = append(w.State.Events, playerEvent)
		}
		state, err := a.Think(ctx, w.State)
		w.State.Events = origEvents

		if err != nil {
			log.Printf("agent %s error: %v", id, err)
			state = a.State
			state.Tick = w.State.Tick
		}
		agentStates = append(agentStates, state)
		locations[state.Location] = append(locations[state.Location], id)

		if state.Dialogue != nil {
			events = append(events, fmt.Sprintf("%s 说：%s", a.Config.Name, *state.Dialogue))
		}
		events = append(events, fmt.Sprintf("%s 在%s%s", a.Config.Name, state.Location, state.Action))
	}

	w.State.Agents = agentStates
	w.State.Locations = locations
	w.State.Events = events
	w.History = append(w.History, w.State)

	if w.OnTick != nil {
		w.OnTick(w.State)
	}
	return TickResponse{State: w.State, PlayerState: w.PlayerState}
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
		target := ""
		if act.Target != nil {
			target = *act.Target
		}
		return fmt.Sprintf("【玩家】%s 对 %s 说：%s", w.Player.Name, target, act.Content)
	case "act":
		ps.Action = act.Content
		return fmt.Sprintf("【玩家】%s %s", w.Player.Name, act.Content)
	case "skip":
		ps.Action = "静静观察"
		return ""
	}
	return ""
}

// SubmitAction 提交玩家行动。
func (w *World) SubmitAction(action schema.PlayerAction) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.PendingAction = &action
}

// JoinPlayer 玩家加入世界。
func (w *World) JoinPlayer(cfg schema.PlayerConfig) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.Player = &cfg
	w.PlayerState = &schema.PlayerState{
		PlayerID: cfg.ID,
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
