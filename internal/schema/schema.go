// Package schema 定义 Fable 系统的所有核心数据结构。
package schema

// AgentState 表示每个 Tick 中 Agent 的输出状态，结构化便于前端渲染。
type AgentState struct {
	AgentID      string   `json:"agent_id"      yaml:"agent_id"`
	Name         string   `json:"name"          yaml:"name"`
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

// Connection 表示地点之间的连接，带距离权重。
type Connection struct {
	Target   string `json:"name" yaml:"name"`
	Distance int    `json:"distance" yaml:"distance"`
}

// Location 表示世界中的一个地点。
type Location struct {
	Name        string       `json:"name" yaml:"name"`
	Description string       `json:"description" yaml:"description"`
	Connected   []Connection `json:"connected" yaml:"connected"`
	Capacity    int          `json:"capacity,omitempty" yaml:"capacity,omitempty"`
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
	Name        string     `json:"name" yaml:"name"`
	Description string     `json:"description" yaml:"description"`
	Locations   []Location `json:"locations" yaml:"locations"`
	Rules       []string   `json:"rules" yaml:"rules"`
	TimeStep    int        `json:"time_step" yaml:"time_step"`
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
	DevMode    bool             `yaml:"dev_mode"`
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

// ConversationTurn 表示对话中的一轮发言。
type ConversationTurn struct {
	Speaker string `json:"speaker"` // "player" 或 agent_id
	Content string `json:"content"`
	Tick    int    `json:"tick"`
}

// ConversationSession 表示一次玩家与 NPC 的对话会话。
type ConversationSession struct {
	PlayerID  string             `json:"player_id"`
	NPCid     string             `json:"npc_id"`
	History   []ConversationTurn `json:"history"`
	StartTick int                `json:"start_tick"`
	Active    bool               `json:"active"`
}

// SimulationMode 表示模拟运行模式（状态机）。
type SimulationMode string

const (
	ModeIdle    SimulationMode = "idle"    // 未运行
	ModeRunning SimulationMode = "running" // 自动运行中
	ModePaused  SimulationMode = "paused"  // 暂停等待玩家响应
)

// SaveData 存档数据，包含世界状态和玩家数据。
type SaveData struct {
	State       WorldState   `json:"state"`
	Player      *PlayerConfig `json:"player,omitempty"`
	PlayerState *PlayerState  `json:"player_state,omitempty"`
}

// LastSession 记录上次使用的世界和存档名。
type LastSession struct {
	WorldID  string `yaml:"world_id"`
	SaveName string `yaml:"save_name"`
}
