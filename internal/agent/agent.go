// Package agent 实现 NPC Agent 的核心逻辑，包括记忆、规划和行动。
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/claw-works/fable/internal/llm"
	"github.com/claw-works/fable/internal/schema"
)

// Agent 表示一个由 LLM 驱动的 NPC。
type Agent struct {
	Config  schema.AgentConfig
	Memory  []string // 记忆列表
	State   schema.AgentState
	llm     *llm.Client
}

// New 创建一个新的 Agent。
func New(cfg schema.AgentConfig, llmClient *llm.Client) *Agent {
	return &Agent{
		Config: cfg,
		Memory: []string{cfg.Backstory},
		State: schema.AgentState{
			AgentID:  cfg.ID,
			Name:     cfg.Name,
			Location: cfg.InitLocation,
			Emotion:  "平静",
		},
		llm: llmClient,
	}
}

// Think 让 Agent 根据当前世界状态进行思考并产生行动。
func (a *Agent) Think(ctx context.Context, world schema.WorldState) (schema.AgentState, error) {
	filtered := FilterWorldStateForAgent(world, a.Config.ID)
	prompt := a.buildPrompt(filtered)

	messages := []llm.Message{
		{Role: "system", Content: fmt.Sprintf(
			"你是%s，%s，性格：%s。你在一个古代小镇中生活。请根据当前情境做出行动决策。"+
				"以 JSON 格式回复，包含字段：location, action, target(可选), dialogue(可选), emotion, inner_thought, memory_update(数组)。",
			a.Config.Name, a.Config.Occupation, a.Config.Personality,
		)},
		{Role: "user", Content: prompt},
	}

	resp, err := a.llm.ChatJSON(ctx, messages)
	if err != nil {
		return a.State, fmt.Errorf("agent %s think: %w", a.Config.ID, err)
	}

	var result schema.AgentState
	if err := json.Unmarshal([]byte(resp), &result); err != nil {
		return a.State, fmt.Errorf("agent %s parse response: %w", a.Config.ID, err)
	}

	result.AgentID = a.Config.ID
	result.Name = a.Config.Name
	result.Tick = world.Tick + 1
	result.GameTime = world.GameTime

	// 更新记忆
	a.Memory = append(a.Memory, result.MemoryUpdate...)
	a.State = result
	return result, nil
}

// buildPrompt 构建发送给 LLM 的上下文提示。
func (a *Agent) buildPrompt(world schema.WorldState) string {
	var b strings.Builder
	fmt.Fprintf(&b, "当前时间：%s\n", world.GameTime)
	fmt.Fprintf(&b, "你现在在：%s\n", a.State.Location)
	fmt.Fprintf(&b, "你的记忆：\n")
	for _, m := range a.Memory {
		fmt.Fprintf(&b, "- %s\n", m)
	}
	fmt.Fprintf(&b, "\n周围的人：\n")
	if agents, ok := world.Locations[a.State.Location]; ok {
		for _, id := range agents {
			if id != a.Config.ID {
				fmt.Fprintf(&b, "- %s\n", id)
			}
		}
	}
	fmt.Fprintf(&b, "\n最近发生的事：\n")
	for _, e := range world.Events {
		fmt.Fprintf(&b, "- %s\n", e)
	}
	return b.String()
}

// FilterWorldStateForAgent 过滤世界状态，NPC 只能看到：
// - 当前地点有哪些人（同地点可见）
// - 自己亲历的事件（包含自己名字或 agent_id 的事件）
// - 全局事件（以"【"开头的广播事件）
func FilterWorldStateForAgent(state schema.WorldState, agentID string) schema.WorldState {
	filtered := schema.WorldState{
		Tick:     state.Tick,
		GameTime: state.GameTime,
	}

	// 找到 agent 当前所在地点
	agentLoc := ""
	for _, a := range state.Agents {
		if a.AgentID == agentID {
			agentLoc = a.Location
			break
		}
	}

	// 只保留同地点的人
	filtered.Locations = map[string][]string{}
	if agents, ok := state.Locations[agentLoc]; ok {
		filtered.Locations[agentLoc] = agents
	}

	// 只保留同地点的 agent 状态（隐藏 InnerThought）
	for _, a := range state.Agents {
		if a.Location == agentLoc {
			visible := a
			if visible.AgentID != agentID {
				visible.InnerThought = ""
				visible.MemoryUpdate = nil
			}
			filtered.Agents = append(filtered.Agents, visible)
		}
	}

	// 过滤事件：只保留全局广播和与自己相关的
	for _, e := range state.Events {
		if strings.HasPrefix(e, "【") || strings.Contains(e, agentID) {
			filtered.Events = append(filtered.Events, e)
		}
	}

	return filtered
}
