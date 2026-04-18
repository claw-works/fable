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
			Location: cfg.InitLocation,
			Emotion:  "平静",
		},
		llm: llmClient,
	}
}

// Think 让 Agent 根据当前世界状态进行思考并产生行动。
func (a *Agent) Think(ctx context.Context, world schema.WorldState) (schema.AgentState, error) {
	prompt := a.buildPrompt(world)

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
