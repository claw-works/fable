// Fable - AI Agent 小镇模拟系统
// 启动入口：加载配置 → 初始化世界 → 启动 HTTP 服务
package main

import (
	"fmt"
	"log"
	"os"

	"github.com/claw-works/fable/internal/llm"
	"github.com/claw-works/fable/internal/schema"
	"github.com/claw-works/fable/internal/world"
	"github.com/claw-works/fable/server"
	"gopkg.in/yaml.v3"
)

func main() {
	cfg, err := loadConfig("config.yaml")
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	worldDir := "worlds/example"
	if len(os.Args) > 1 {
		worldDir = os.Args[1]
	}

	llmClient := llm.New(cfg.LLM)

	w, err := world.Load(worldDir, llmClient)
	if err != nil {
		log.Fatalf("load world: %v", err)
	}

	fmt.Printf("🏘  Fable - %s\n", w.Config.Name)
	fmt.Printf("📍 地点: %d 个 | 👥 角色: %d 个\n", len(w.Config.Locations), len(w.Agents))
	fmt.Printf("🌐 http://localhost:%d\n", cfg.Server.Port)

	srv := server.New(w, *cfg)
	if err := srv.Run(); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func loadConfig(path string) (*schema.Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var cfg schema.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &cfg, nil
}
