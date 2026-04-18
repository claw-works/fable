// Package schema 定义 Fable 系统的所有核心数据结构。
package schema

// AgentState 表示每个 Tick 中 Agent 的输出状态，结构化便于前端渲染。
type AgentState struct {
	AgentID      string   `json:"agent_id"      yaml:"agent_id"`
	Tick         int      `json:"tick"           yaml:"tick"`
	GameTime     string   `json:"game_time"      yaml:"game_time"`      // "Day1 08:30"
	Location     string   `json:"location"       yaml:"location"`
	Action       string   `json:"action"         yaml:"action"`
	Target       *string  `json:"target,omitempty"       yaml:"target,omitempty"`
	Dialogue     *string  `json:"dialogue,omitempty"     yaml:"dialogue,omitempty"`
	Emotion      string   `json:"emotion"        yaml:"emotion"`
	InnerThought string   `json:"inner_thought"  yaml:"inner_thought"`
	MemoryUpdate []string `json:"memory_update"  yaml:"memory_update"`
}

// WorldState 表示某一 Tick 的世界快照。
type WorldState struct {
	Tick      int                 `json:"tick"       yaml:"tick"`
	GameTime  string              `json:"game_time"  yaml:"game_time"`
	Locations map[string][]string `json:"locations"  yaml:"locations"` // location -> []agent_id
	Events    []string            `json:"events"     yaml:"events"`
	Agents    []AgentState        `json:"agents"     yaml:"agents"`
}

// Location 表示世界中的一个地点。
type Location struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Connected   []string `yaml:"connected"` // 相邻地点
}

// AgentConfig 表示角色配置，从 YAML 读取。
type AgentConfig struct {
	ID            string            `yaml:"id"`
	Name          string            `yaml:"name"`
	Age           int               `yaml:"age"`
	Occupation    string            `yaml:"occupation"`
	Personality   string            `yaml:"personality"`
	Backstory     string            `yaml:"backstory"`
	Relationships map[string]string `yaml:"relationships"`
	InitLocation  string            `yaml:"init_location"`
}

// WorldConfig 表示世界配置，从 YAML 读取。
type WorldConfig struct {
	Name        string     `yaml:"name"`
	Description string     `yaml:"description"`
	Locations   []Location `yaml:"locations"`
	Rules       []string   `yaml:"rules"`
	TimeStep    int        `yaml:"time_step"` // 每 Tick 多少分钟
}

// AgentsFile 表示 agents.yaml 的顶层结构。
type AgentsFile struct {
	Agents []AgentConfig `yaml:"agents"`
}

// Config 表示全局配置文件 config.yaml。
type Config struct {
	LLM        LLMConfig        `yaml:"llm"`
	Server     ServerConfig     `yaml:"server"`
	Simulation SimulationConfig `yaml:"simulation"`
}

// LLMConfig 表示 LLM 调用配置。
type LLMConfig struct {
	BaseURL string `yaml:"base_url"`
	APIKey  string `yaml:"api_key"`
	Model   string `yaml:"model"`
	Timeout int    `yaml:"timeout"` // 秒
}

// ServerConfig 表示 HTTP 服务配置。
type ServerConfig struct {
	Port int `yaml:"port"`
}

// SimulationConfig 表示模拟运行配置。
type SimulationConfig struct {
	TickInterval int  `yaml:"tick_interval"` // 秒
	AutoRun      bool `yaml:"auto_run"`
}

// WorldManifest 表示世界包的元信息（manifest.yaml）。
type WorldManifest struct {
	ID          string   `yaml:"id"`
	Name        string   `yaml:"name"`
	Version     string   `yaml:"version"`
	Author      string   `yaml:"author"`
	Description string   `yaml:"description"`
	Tags        []string `yaml:"tags"`
	Repo        string   `yaml:"repo"`
}

// TickResult 表示一次 Tick 的完整结果。
type TickResult struct {
	WorldState WorldState `json:"world_state"`
	Error      error      `json:"-"`
}

// PlayerConfig 玩家角色配置。
type PlayerConfig struct {
	ID           string `json:"id" yaml:"id"`
	Name         string `json:"name" yaml:"name"`
	Age          int    `json:"age" yaml:"age"`
	Occupation   string `json:"occupation" yaml:"occupation"`
	Personality  string `json:"personality" yaml:"personality"`
	Backstory    string `json:"backstory" yaml:"backstory"`
	InitLocation string `json:"init_location" yaml:"init_location"`
}

// PlayerAction 玩家在某个 Tick 的输入。
type PlayerAction struct {
	Type     string  `json:"type"`               // "move" | "talk" | "act" | "skip"
	Location *string `json:"location,omitempty"`  // move 时目标地点
	Target   *string `json:"target,omitempty"`    // talk 时目标 agent_id
	Content  string  `json:"content,omitempty"`   // talk/act 的内容
}

// PlayerState 玩家当前状态（和 AgentState 对齐）。
type PlayerState struct {
	PlayerID string  `json:"player_id"`
	Tick     int     `json:"tick"`
	GameTime string  `json:"game_time"`
	Location string  `json:"location"`
	Action   string  `json:"action"`
	Target   *string `json:"target,omitempty"`
	Dialogue *string `json:"dialogue,omitempty"`
}
