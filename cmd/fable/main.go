// Fable - AI Agent 小镇模拟系统
// 启动入口：加载配置 → 初始化世界 → 启动 HTTP 服务
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/claw-works/fable/internal/llm"
	"github.com/claw-works/fable/internal/schema"
	"github.com/claw-works/fable/internal/storage"
	"github.com/claw-works/fable/internal/world"
	"github.com/claw-works/fable/server"
	"gopkg.in/yaml.v3"
)

func main() {
	configFlag := flag.String("config", "", "配置文件路径")
	worldFlag := flag.String("world", "", "世界目录路径")
	flag.Parse()

	// 首次启动初始化 ~/.fable
	if _, err := os.Stat(storage.FableDir()); os.IsNotExist(err) {
		if err := storage.Init("worlds"); err != nil {
			log.Printf("warn: init ~/.fable: %v", err)
		} else {
			fmt.Println("🏘️  首次启动！已初始化 ~/.fable/")
			fmt.Println("📝  请编辑 ~/.fable/config.yaml 填入您的 LLM API Key")
			fmt.Println("🌏  已安装示例世界：清水镇（qingshui-town）")
		}
	}

	cfg, err := resolveConfig(*configFlag)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	worldDir := resolveWorldDir(*worldFlag)

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

// resolveConfig 按优先级加载配置：--config > ~/.fable/config.yaml > ./config.yaml
func resolveConfig(flagPath string) (*schema.Config, error) {
	paths := []string{flagPath, storage.ConfigPath(), "config.yaml"}
	for _, p := range paths {
		if p == "" {
			continue
		}
		if _, err := os.Stat(p); err == nil {
			return loadConfig(p)
		}
	}
	return nil, fmt.Errorf("no config file found")
}

// resolveWorldDir 按优先级确定世界目录：--world > ~/.fable/worlds/第一个 > ./worlds/example
func resolveWorldDir(flagPath string) string {
	if flagPath != "" {
		return flagPath
	}
	// 尝试 ~/.fable/worlds/ 第一个世界
	entries, err := os.ReadDir(storage.WorldsDir())
	if err == nil {
		for _, e := range entries {
			if e.IsDir() {
				return filepath.Join(storage.WorldsDir(), e.Name())
			}
		}
	}
	return "worlds/example"
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
