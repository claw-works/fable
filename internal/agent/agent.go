// Package agent 实现 NPC Agent 的核心逻辑，包括分层记忆、规划、反思和行动。
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/claw-works/fable/internal/llm"
	"github.com/claw-works/fable/internal/schema"
)

const (
	shortTermCap   = 20 // 短期记忆容量
	longTermCap    = 50 // 长期记忆超过此数触发压缩
	compressBatch  = 20 // 每次压缩最早的 N 条为摘要
	reflectEvery   = 6  // 每 N 个 tick 触发一次反思
)

// Agent 表示一个由 LLM 驱动的 NPC。
type Agent struct {
	Config          schema.AgentConfig
	ShortTermMemory []string            // 最近事件，滚动窗口
	LongTermMemory  []string            // 重要事件，反思后沉淀
	CurrentPlan     *schema.DailyPlan   // 当天计划
	LongTermGoal    string              // 长期目标
	Relations       map[string]schema.Relation // 动态关系
	State           schema.AgentState
	llm             *llm.Client
}

// New 创建一个新的 Agent。
func New(cfg schema.AgentConfig, llmClient *llm.Client) *Agent {
	// 从静态配置初始化关系
	relations := make(map[string]schema.Relation, len(cfg.Relationships))
	for id, label := range cfg.Relationships {
		relations[id] = schema.Relation{
			TargetID: id, Label: label, Affinity: 50, Description: label,
		}
	}
	return &Agent{
		Config:          cfg,
		ShortTermMemory: []string{cfg.Backstory},
		LongTermMemory:  []string{},
		Relations:       relations,
		LongTermGoal:    "",
		State: schema.AgentState{
			AgentID:  cfg.ID,
			Name:     cfg.Name,
			Location: cfg.InitLocation,
			Emotion:  "平静",
		},
		llm: llmClient,
	}
}

// LLMClient 返回 Agent 的 LLM 客户端。
func (a *Agent) LLMClient() *llm.Client { return a.llm }

// Think 让 Agent 根据当前世界状态进行思考并产生行动。
func (a *Agent) Think(ctx context.Context, world schema.WorldState) (schema.AgentState, error) {
	filtered := FilterWorldStateForAgent(world, a.Config.ID)
	prompt := a.buildPrompt(filtered)

	messages := []llm.Message{
		{Role: "system", Content: a.buildSystemPrompt()},
		{Role: "user", Content: prompt},
	}

	resp, err := a.llm.ChatJSON(ctx, messages)
	if err != nil {
		return a.State, fmt.Errorf("agent %s think: %w", a.Config.ID, err)
	}

	// 用 RawMessage 容错解析，Claude 可能返回非预期类型
	var raw struct {
		Location        string              `json:"location"`
		Action          string              `json:"action"`
		Target          *string             `json:"target"`
		Dialogue        *string             `json:"dialogue"`
		Emotion         string              `json:"emotion"`
		InnerThought    string              `json:"inner_thought"`
		MemoryUpdate    json.RawMessage     `json:"memory_update"`
		RelationChanges json.RawMessage     `json:"relation_changes"`
	}
	if err := json.Unmarshal([]byte(resp), &raw); err != nil {
		log.Printf("[agent %s] 原始响应(%d chars):\n%s", a.Config.ID, len(resp), resp)
		return a.State, fmt.Errorf("agent %s parse response: %w", a.Config.ID, err)
	}

	result := schema.AgentState{
		Location:     raw.Location,
		Action:       raw.Action,
		Target:       raw.Target,
		Dialogue:     raw.Dialogue,
		Emotion:      raw.Emotion,
		InnerThought: raw.InnerThought,
	}
	result.MemoryUpdate = parseStringArray(raw.MemoryUpdate)
	json.Unmarshal(raw.RelationChanges, &result.RelationChanges) // best effort

	result.AgentID = a.Config.ID
	result.Name = a.Config.Name
	result.Tick = world.Tick + 1
	result.GameTime = world.GameTime

	// 更新短期记忆（滚动窗口）
	a.ShortTermMemory = append(a.ShortTermMemory, result.MemoryUpdate...)
	if len(a.ShortTermMemory) > shortTermCap {
		a.ShortTermMemory = a.ShortTermMemory[len(a.ShortTermMemory)-shortTermCap:]
	}

	// 应用关系变化
	a.applyRelationChanges(result.RelationChanges)

	a.State = result
	return result, nil
}

// Reflect 反思：总结短期记忆为长期记忆，评估计划，调整目标。
// 由 World 在每 N 个 tick 后调用。
func (a *Agent) Reflect(ctx context.Context, world schema.WorldState) error {
	if len(a.ShortTermMemory) == 0 {
		return nil
	}

	prompt := a.buildReflectPrompt(world)
	messages := []llm.Message{
		{Role: "system", Content: fmt.Sprintf(
			"你是%s，%s。请根据最近的经历进行反思。"+
				"以 JSON 格式回复，包含字段："+
				"important_memories(数组，从近期经历中提炼的重要记忆)，"+
				"plan(对象，包含 day:int, goals:[]string, steps:[]对象{time,action,location})，"+
				"long_term_goal(字符串，你的长期目标，可以调整或保持不变)，"+
				"emotion(字符串，反思后的情绪状态)。",
			a.Config.Name, a.Config.Personality,
		)},
		{Role: "user", Content: prompt},
	}

	resp, err := a.llm.ChatJSON(ctx, messages)
	if err != nil {
		return fmt.Errorf("agent %s reflect: %w", a.Config.ID, err)
	}

	var result struct {
		ImportantMemories []string         `json:"important_memories"`
		Plan              schema.DailyPlan `json:"plan"`
		LongTermGoal      string           `json:"long_term_goal"`
		Emotion           string           `json:"emotion"`
	}
	if err := json.Unmarshal([]byte(resp), &result); err != nil {
		return fmt.Errorf("agent %s parse reflect: %w", a.Config.ID, err)
	}

	// 沉淀重要记忆到长期记忆
	a.LongTermMemory = append(a.LongTermMemory, result.ImportantMemories...)
	// 清空短期记忆（已总结）
	a.ShortTermMemory = a.ShortTermMemory[:0]
	// 更新计划和目标
	a.CurrentPlan = &result.Plan
	if result.LongTermGoal != "" {
		a.LongTermGoal = result.LongTermGoal
	}
	if result.Emotion != "" {
		a.State.Emotion = result.Emotion
	}

	// 长期记忆压缩：超过上限时把最早一批压缩成摘要
	if len(a.LongTermMemory) > longTermCap {
		if err := a.compressLongTermMemory(ctx); err != nil {
			log.Printf("[agent %s] 记忆压缩失败: %v", a.Config.ID, err)
		}
	}
	return nil
}

// ShouldReflect 判断是否应该触发反思。
func ShouldReflect(tick int) bool {
	return tick > 0 && tick%reflectEvery == 0
}

// Snapshot 生成当前 Agent 的完整状态快照。
func (a *Agent) Snapshot() schema.AgentSnapshot {
	rels := make(map[string]schema.Relation, len(a.Relations))
	for k, v := range a.Relations {
		rels[k] = v
	}
	stm := make([]string, len(a.ShortTermMemory))
	copy(stm, a.ShortTermMemory)
	ltm := make([]string, len(a.LongTermMemory))
	copy(ltm, a.LongTermMemory)

	snap := schema.AgentSnapshot{
		AgentID:         a.Config.ID,
		State:           a.State,
		ShortTermMemory: stm,
		LongTermMemory:  ltm,
		Relations:       rels,
		LongTermGoal:    a.LongTermGoal,
	}
	if a.CurrentPlan != nil {
		plan := *a.CurrentPlan
		plan.Steps = make([]schema.PlanStep, len(a.CurrentPlan.Steps))
		copy(plan.Steps, a.CurrentPlan.Steps)
		snap.CurrentPlan = &plan
	}
	return snap
}

// RestoreSnapshot 从快照恢复 Agent 状态。
func (a *Agent) RestoreSnapshot(snap schema.AgentSnapshot) {
	a.State = snap.State
	a.ShortTermMemory = snap.ShortTermMemory
	a.LongTermMemory = snap.LongTermMemory
	a.CurrentPlan = snap.CurrentPlan
	a.LongTermGoal = snap.LongTermGoal
	a.Relations = snap.Relations
}

// compressLongTermMemory 把最早的一批长期记忆压缩成摘要。
func (a *Agent) compressLongTermMemory(ctx context.Context) error {
	if len(a.LongTermMemory) <= longTermCap {
		return nil
	}

	// 取最早的 compressBatch 条
	batch := a.LongTermMemory[:compressBatch]
	var b strings.Builder
	for _, m := range batch {
		fmt.Fprintf(&b, "- %s\n", m)
	}

	messages := []llm.Message{
		{Role: "system", Content: fmt.Sprintf(
			"你是%s。请将以下记忆压缩成 2-3 句话的摘要，保留关键人物、事件和情感。只输出摘要文本，不要 JSON。",
			a.Config.Name,
		)},
		{Role: "user", Content: b.String()},
	}

	summary, err := a.llm.Chat(ctx, messages)
	if err != nil {
		return err
	}

	// 用摘要替换被压缩的记忆
	a.LongTermMemory = append([]string{"[摘要] " + strings.TrimSpace(summary)}, a.LongTermMemory[compressBatch:]...)
	log.Printf("[agent %s] 长期记忆压缩: %d 条 → 摘要 + %d 条", a.Config.ID, compressBatch, len(a.LongTermMemory)-1)
	return nil
}

// parseStringArray 容错解析 JSON 为字符串数组。
// 支持：["a","b"]、"单个字符串"、{"key":"val"} → 转为 JSON 字符串。
func parseStringArray(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	// 尝试 []string
	var arr []string
	if json.Unmarshal(raw, &arr) == nil {
		return arr
	}
	// 尝试单个字符串
	var s string
	if json.Unmarshal(raw, &s) == nil && s != "" {
		return []string{s}
	}
	// 兜底：把整个 JSON 当字符串存
	return []string{string(raw)}
}

// applyRelationChanges 应用关系变化。
func (a *Agent) applyRelationChanges(changes []schema.RelationChange) {
	for _, c := range changes {
		rel, ok := a.Relations[c.TargetID]
		if !ok {
			rel = schema.Relation{TargetID: c.TargetID, Affinity: 50}
		}
		rel.Affinity += c.AffinityDelta
		if rel.Affinity > 100 {
			rel.Affinity = 100
		}
		if rel.Affinity < -100 {
			rel.Affinity = -100
		}
		if c.NewLabel != "" {
			rel.Label = c.NewLabel
		}
		if c.Reason != "" {
			rel.Description = c.Reason
		}
		a.Relations[c.TargetID] = rel
	}
}

// buildSystemPrompt 构建系统提示。
func (a *Agent) buildSystemPrompt() string {
	var b strings.Builder
	fmt.Fprintf(&b, "你是%s，%s，性格：%s。你在一个古代小镇中生活。\n", a.Config.Name, a.Config.Occupation, a.Config.Personality)
	if a.LongTermGoal != "" {
		fmt.Fprintf(&b, "你的长期目标：%s\n", a.LongTermGoal)
	}
	if a.CurrentPlan != nil && len(a.CurrentPlan.Steps) > 0 {
		fmt.Fprintf(&b, "你今天的计划：\n")
		for _, s := range a.CurrentPlan.Steps {
			mark := "○"
			if s.Done {
				mark = "✓"
			}
			fmt.Fprintf(&b, "  %s %s 在%s %s\n", mark, s.Time, s.Location, s.Action)
		}
	}
	b.WriteString("请根据当前情境做出行动决策。")
	b.WriteString("以 JSON 格式回复，包含字段：location, action, target(可选), dialogue(可选), emotion, inner_thought, memory_update(数组), relation_changes(数组，每项含 target_id, affinity_delta, new_label可选, reason)。")
	b.WriteString("注意：所有字段值请简洁，dialogue 不超过两句话，action 不超过 20 字。只输出 JSON，不要任何其他文字。")
	return b.String()
}

// buildPrompt 构建发送给 LLM 的上下文提示。
func (a *Agent) buildPrompt(world schema.WorldState) string {
	var b strings.Builder
	fmt.Fprintf(&b, "当前时间：%s\n", world.GameTime)
	fmt.Fprintf(&b, "你现在在：%s\n", a.State.Location)

	// 长期记忆（重要经历）
	if len(a.LongTermMemory) > 0 {
		b.WriteString("\n重要经历：\n")
		for _, m := range a.LongTermMemory {
			fmt.Fprintf(&b, "- %s\n", m)
		}
	}

	// 短期记忆（最近事件）
	b.WriteString("\n最近记忆：\n")
	for _, m := range a.ShortTermMemory {
		fmt.Fprintf(&b, "- %s\n", m)
	}

	// 关系
	if len(a.Relations) > 0 {
		b.WriteString("\n你与他人的关系：\n")
		for _, rel := range a.Relations {
			fmt.Fprintf(&b, "- %s（%s，好感度 %d）\n", rel.TargetID, rel.Label, rel.Affinity)
		}
	}

	// 周围的人
	b.WriteString("\n周围的人：\n")
	if agents, ok := world.Locations[a.State.Location]; ok {
		for _, name := range agents {
			if name != a.Config.Name {
				fmt.Fprintf(&b, "- %s\n", name)
			}
		}
	}

	b.WriteString("\n最近发生的事：\n")
	for _, e := range world.Events {
		fmt.Fprintf(&b, "- %s\n", e)
	}
	return b.String()
}

// buildReflectPrompt 构建反思提示。
func (a *Agent) buildReflectPrompt(world schema.WorldState) string {
	var b strings.Builder
	fmt.Fprintf(&b, "当前时间：%s\n", world.GameTime)
	fmt.Fprintf(&b, "你现在在：%s\n", a.State.Location)

	b.WriteString("\n你最近的经历：\n")
	for _, m := range a.ShortTermMemory {
		fmt.Fprintf(&b, "- %s\n", m)
	}

	if len(a.LongTermMemory) > 0 {
		b.WriteString("\n你的重要记忆：\n")
		for _, m := range a.LongTermMemory {
			fmt.Fprintf(&b, "- %s\n", m)
		}
	}

	if a.LongTermGoal != "" {
		fmt.Fprintf(&b, "\n你当前的长期目标：%s\n", a.LongTermGoal)
	}

	if a.CurrentPlan != nil {
		b.WriteString("\n你今天的计划执行情况：\n")
		for _, s := range a.CurrentPlan.Steps {
			mark := "未完成"
			if s.Done {
				mark = "已完成"
			}
			fmt.Fprintf(&b, "- %s %s（%s）\n", s.Time, s.Action, mark)
		}
	}

	b.WriteString("\n请反思：哪些经历值得长期记住？明天有什么计划？长期目标是否需要调整？")
	return b.String()
}

// FilterWorldStateForAgent 过滤世界状态，NPC 只能看到同地点的人和相关事件。
func FilterWorldStateForAgent(state schema.WorldState, agentID string) schema.WorldState {
	filtered := schema.WorldState{
		Tick:     state.Tick,
		GameTime: state.GameTime,
	}

	agentLoc := ""
	agentName := ""
	for _, a := range state.Agents {
		if a.AgentID == agentID {
			agentLoc = a.Location
			agentName = a.Name
			break
		}
	}

	filtered.Locations = map[string][]string{}
	if agents, ok := state.Locations[agentLoc]; ok {
		filtered.Locations[agentLoc] = agents
	}

	for _, a := range state.Agents {
		if a.Location == agentLoc {
			visible := a
			if visible.AgentID != agentID {
				visible.InnerThought = ""
				visible.MemoryUpdate = nil
				visible.RelationChanges = nil
			}
			filtered.Agents = append(filtered.Agents, visible)
		}
	}

	for _, e := range state.Events {
		if strings.HasPrefix(e, "【") || strings.Contains(e, agentName) || strings.Contains(e, agentID) {
			filtered.Events = append(filtered.Events, e)
		}
	}

	return filtered
}
