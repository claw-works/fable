// Package storage 管理 ~/.fable 目录结构、世界安装和存档。
package storage

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/claw-works/fable/internal/schema"
	"gopkg.in/yaml.v3"
)

// FableDir 返回 ~/.fable 目录路径。
func FableDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".fable")
}

// WorldsDir 返回 ~/.fable/worlds 目录。
func WorldsDir() string { return filepath.Join(FableDir(), "worlds") }

// SavesDir 返回 ~/.fable/saves 目录。
func SavesDir() string { return filepath.Join(FableDir(), "saves") }

// ConfigPath 返回 ~/.fable/config.yaml 路径。
func ConfigPath() string { return filepath.Join(FableDir(), "config.yaml") }

// Init 初始化 ~/.fable 目录结构，首次运行时复制内置示例世界。
func Init(builtinWorldsDir string) error {
	dirs := []string{FableDir(), WorldsDir(), SavesDir()}
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

// SaveWorld 保存当前世界状态到 ~/.fable/saves/{worldID}/latest.json。
func SaveWorld(worldID string, state schema.WorldState) error {
	dir := filepath.Join(SavesDir(), worldID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "latest.json"), data, 0644)
}

// LoadLatestSave 加载最近存档。
func LoadLatestSave(worldID string) (*schema.WorldState, error) {
	path := filepath.Join(SavesDir(), worldID, "latest.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var state schema.WorldState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return &state, nil
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
