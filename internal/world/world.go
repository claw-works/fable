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
	mu       sync.RWMutex
	Config   schema.WorldConfig
	Agents   map[string]*agent.Agent
	State    schema.WorldState
	History  []schema.WorldState
	OnTick   func(schema.WorldState) // Tick 完成后的回调（用于推送 WebSocket）
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

	return &World{
		Config: wCfg,
		Agents: agents,
		State: schema.WorldState{
			Tick:      0,
			GameTime:  "Day1 08:00",
			Locations: locations,
		},
	}, nil
}

// Tick 执行一次世界更新。
func (w *World) Tick(ctx context.Context) schema.WorldState {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.State.Tick++
	w.State.GameTime = w.advanceTime(w.State.GameTime)

	var agentStates []schema.AgentState
	var events []string
	locations := make(map[string][]string)

	for id, a := range w.Agents {
		state, err := a.Think(ctx, w.State)
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
	return w.State
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
