// Package storage 管理 ~/.fable 目录结构、世界安装和 SQLite 存档。
package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/claw-works/fable/internal/schema"
	_ "modernc.org/sqlite"
	"gopkg.in/yaml.v3"
)

// FableDir 返回 ~/.fable 目录路径。
func FableDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".fable")
}

// WorldsDir 返回 ~/.fable/worlds 目录。
func WorldsDir() string { return filepath.Join(FableDir(), "worlds") }

// ConfigPath 返回 ~/.fable/config.yaml 路径。
func ConfigPath() string { return filepath.Join(FableDir(), "config.yaml") }

// DB 是当前存档的数据库连接。
var DB *sql.DB

// InitDB 打开或创建 ~/.fable/saves/{worldID}/{saveName}.db 并建表。
func InitDB(worldID, saveName string) error {
	if DB != nil {
		DB.Close()
	}
	dir := filepath.Join(FableDir(), "saves", worldID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	dbPath := filepath.Join(dir, saveName+".db")
	var err error
	DB, err = sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	if _, err := DB.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return err
	}
	_, err = DB.Exec(createSQL)
	return err
}

const createSQL = `
CREATE TABLE IF NOT EXISTS ticks (
	tick       INTEGER PRIMARY KEY,
	game_time  TEXT NOT NULL,
	state_json TEXT NOT NULL,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS agent_states (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	tick          INTEGER NOT NULL,
	agent_id      TEXT NOT NULL,
	name          TEXT NOT NULL,
	location      TEXT NOT NULL,
	action        TEXT NOT NULL DEFAULT '',
	dialogue      TEXT,
	emotion       TEXT NOT NULL DEFAULT '',
	inner_thought TEXT NOT NULL DEFAULT '',
	memory_json   TEXT,
	created_at    DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_agent_states_agent ON agent_states(agent_id, tick);
CREATE INDEX IF NOT EXISTS idx_agent_states_location ON agent_states(location, tick);

CREATE TABLE IF NOT EXISTS player_data (
	id          INTEGER PRIMARY KEY CHECK (id = 1),
	config_json TEXT,
	state_json  TEXT
);
`

// SaveTick 保存一个 Tick 的完整数据。
func SaveTick(data schema.SaveData) error {
	stateJSON, err := json.Marshal(data.State)
	if err != nil {
		return err
	}

	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(
		`INSERT OR REPLACE INTO ticks (tick, game_time, state_json) VALUES (?, ?, ?)`,
		data.State.Tick, data.State.GameTime, string(stateJSON),
	)
	if err != nil {
		return err
	}

	stmt, err := tx.Prepare(
		`INSERT INTO agent_states (tick, agent_id, name, location, action, dialogue, emotion, inner_thought, memory_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, a := range data.State.Agents {
		memJSON, _ := json.Marshal(a.MemoryUpdate)
		_, err = stmt.Exec(data.State.Tick, a.AgentID, a.Name, a.Location,
			a.Action, a.Dialogue, a.Emotion, a.InnerThought, string(memJSON))
		if err != nil {
			return err
		}
	}

	if data.Player != nil || data.PlayerState != nil {
		cfgJSON, _ := json.Marshal(data.Player)
		psJSON, _ := json.Marshal(data.PlayerState)
		_, err = tx.Exec(
			`INSERT OR REPLACE INTO player_data (id, config_json, state_json) VALUES (1, ?, ?)`,
			string(cfgJSON), string(psJSON),
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// LoadLatestSave 加载最新的存档快照。
func LoadLatestSave() (*schema.SaveData, error) {
	var stateJSON string
	err := DB.QueryRow(
		`SELECT state_json FROM ticks ORDER BY tick DESC LIMIT 1`,
	).Scan(&stateJSON)
	if err != nil {
		return nil, err
	}

	var data schema.SaveData
	if err := json.Unmarshal([]byte(stateJSON), &data.State); err != nil {
		return nil, err
	}

	var cfgJSON, psJSON sql.NullString
	err = DB.QueryRow(`SELECT config_json, state_json FROM player_data WHERE id=1`).Scan(&cfgJSON, &psJSON)
	if err == nil {
		if cfgJSON.Valid {
			var pc schema.PlayerConfig
			if json.Unmarshal([]byte(cfgJSON.String), &pc) == nil {
				data.Player = &pc
			}
		}
		if psJSON.Valid {
			var ps schema.PlayerState
			if json.Unmarshal([]byte(psJSON.String), &ps) == nil {
				data.PlayerState = &ps
			}
		}
	}

	return &data, nil
}

// QueryRecentTicks 查询最近 N 个 tick 的完整世界状态。
func QueryRecentTicks(limit int) ([]schema.WorldState, error) {
	rows, err := DB.Query(
		`SELECT state_json FROM ticks ORDER BY tick DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []schema.WorldState
	for rows.Next() {
		var stateJSON string
		if err := rows.Scan(&stateJSON); err != nil {
			return nil, err
		}
		var state schema.WorldState
		if json.Unmarshal([]byte(stateJSON), &state) == nil {
			results = append(results, state)
		}
	}
	// 反转为时间正序
	for i, j := 0, len(results)-1; i < j; i, j = i+1, j-1 {
		results[i], results[j] = results[j], results[i]
	}
	return results, nil
}

// QueryAgentHistory 查询某个 NPC 的历史状态。
func QueryAgentHistory(agentID string, limit int) ([]schema.AgentState, error) {
	rows, err := DB.Query(
		`SELECT tick, name, location, action, dialogue, emotion, inner_thought, memory_json
		 FROM agent_states WHERE agent_id=? ORDER BY tick DESC LIMIT ?`,
		agentID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []schema.AgentState
	for rows.Next() {
		var a schema.AgentState
		var dialogue sql.NullString
		var memJSON string
		if err := rows.Scan(&a.Tick, &a.Name, &a.Location, &a.Action, &dialogue, &a.Emotion, &a.InnerThought, &memJSON); err != nil {
			return nil, err
		}
		a.AgentID = agentID
		if dialogue.Valid {
			a.Dialogue = &dialogue.String
		}
		json.Unmarshal([]byte(memJSON), &a.MemoryUpdate)
		results = append(results, a)
	}
	return results, nil
}

// QueryTickState 查询某个 Tick 的完整快照。
func QueryTickState(tick int) (*schema.WorldState, error) {
	var stateJSON string
	err := DB.QueryRow(`SELECT state_json FROM ticks WHERE tick=?`, tick).Scan(&stateJSON)
	if err != nil {
		return nil, err
	}
	var state schema.WorldState
	if err := json.Unmarshal([]byte(stateJSON), &state); err != nil {
		return nil, err
	}
	return &state, nil
}

// QueryAgentsByLocation 查询某个地点在某个 Tick 的所有 NPC。
func QueryAgentsByLocation(location string, tick int) ([]schema.AgentState, error) {
	rows, err := DB.Query(
		`SELECT agent_id, name, action, dialogue, emotion, inner_thought
		 FROM agent_states WHERE location=? AND tick=?`,
		location, tick,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []schema.AgentState
	for rows.Next() {
		var a schema.AgentState
		var dialogue sql.NullString
		if err := rows.Scan(&a.AgentID, &a.Name, &a.Action, &dialogue, &a.Emotion, &a.InnerThought); err != nil {
			return nil, err
		}
		a.Location = location
		a.Tick = tick
		if dialogue.Valid {
			a.Dialogue = &dialogue.String
		}
		results = append(results, a)
	}
	return results, nil
}

// SaveLastSession 记录上次使用的世界和存档（写文件，跨存档）。
func SaveLastSession(s schema.LastSession) error {
	if err := os.MkdirAll(FableDir(), 0755); err != nil {
		return err
	}
	data, err := yaml.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(FableDir(), "last_session.yaml"), data, 0644)
}

// LoadLastSession 读取上次会话信息。
func LoadLastSession() (*schema.LastSession, error) {
	data, err := os.ReadFile(filepath.Join(FableDir(), "last_session.yaml"))
	if err != nil {
		return nil, err
	}
	var s schema.LastSession
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// ── 以下为文件系统工具函数（Init、ListWorlds 等不变）──

// ListWorldDirs 列出 ~/.fable/worlds/ 下的目录名。
func ListWorldDirs() ([]string, error) {
	entries, err := os.ReadDir(WorldsDir())
	if err != nil {
		return nil, err
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	return dirs, nil
}

// ListSaves 列出某个世界的所有存档名（.db 文件）。
func ListSaves(worldID string) ([]string, error) {
	dir := filepath.Join(FableDir(), "saves", worldID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var saves []string
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() && filepath.Ext(name) == ".db" {
			saves = append(saves, name[:len(name)-3]) // 去掉 .db
		}
	}
	return saves, nil
}

// Init 初始化 ~/.fable 目录结构，首次运行时复制内置示例世界。
func Init(builtinWorldsDir string) error {
	dirs := []string{FableDir(), WorldsDir()}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("create dir %s: %w", d, err)
		}
	}
	// 复制默认配置
	if _, err := os.Stat(ConfigPath()); os.IsNotExist(err) {
		defaultCfg := `llm:
  base_url: "https://api.openai.com/v1"
  api_key: "your-api-key"
  model: "gpt-4o-mini"
  timeout: 30

server:
  port: 8080

simulation:
  tick_interval: 5
  auto_run: false
`
		if err := os.WriteFile(ConfigPath(), []byte(defaultCfg), 0644); err != nil {
			return fmt.Errorf("write config: %w", err)
		}
	}
	// 复制内置世界
	exampleSrc := filepath.Join(builtinWorldsDir, "example")
	exampleDst := filepath.Join(WorldsDir(), "qingshui-town")
	if _, err := os.Stat(exampleDst); os.IsNotExist(err) {
		if err := copyDir(exampleSrc, exampleDst); err != nil {
			return fmt.Errorf("copy builtin world: %w", err)
		}
	}
	return nil
}

// ListWorlds 列出所有已安装的世界。
func ListWorlds() ([]schema.WorldManifest, error) {
	entries, err := os.ReadDir(WorldsDir())
	if err != nil {
		return nil, err
	}
	var worlds []schema.WorldManifest
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		mPath := filepath.Join(WorldsDir(), e.Name(), "manifest.yaml")
		data, err := os.ReadFile(mPath)
		if err != nil {
			continue
		}
		var m schema.WorldManifest
		if yaml.Unmarshal(data, &m) == nil {
			worlds = append(worlds, m)
		}
	}
	return worlds, nil
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
