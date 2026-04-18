# Fable - AI Agent 小镇模拟

Fable 是一个 AI Agent 小镇模拟系统。每个 NPC 由 LLM 驱动，拥有独立的记忆、规划和行动能力。支持自定义世界观配置，通过 WebSocket 实时观察小镇中发生的一切。

## 快速开始

### 1. 配置 LLM

编辑 `config.yaml`，填入你的 API Key：

```yaml
llm:
  base_url: "https://api.openai.com/v1"
  api_key: "sk-your-key"
  model: "gpt-4o-mini"
```

### 2. 启动

```bash
make run
```

服务启动后：
- 观察端：http://localhost:8080/frontend/
- 管理端：http://localhost:8080/admin/

### 3. 控制模拟

通过管理端或 API 控制：

```bash
# 单步执行
curl -X POST http://localhost:8080/api/tick

# 开始自动运行
curl -X POST http://localhost:8080/api/start

# 暂停
curl -X POST http://localhost:8080/api/stop
```

## 目录结构

```
fable/
├── cmd/fable/main.go          # 启动入口
├── internal/
│   ├── schema/schema.go       # 核心数据结构
│   ├── agent/agent.go         # Agent 逻辑（记忆、规划、行动）
│   ├── world/world.go         # 世界状态管理，Tick 驱动
│   └── llm/llm.go             # LLM API 封装
├── server/server.go           # HTTP API + WebSocket
├── frontend/                  # 观察端（古朴中国风）
├── admin/                     # 管理端（暗色科技风）
├── worlds/example/            # 示例世界观：清水镇
│   ├── world.yaml             # 地点、规则、背景
│   └── agents.yaml            # 角色设定
├── config.yaml                # 全局配置
└── Makefile
```

## API

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/state` | 当前世界状态 |
| POST | `/api/tick` | 执行一次 Tick |
| POST | `/api/start` | 开始自动运行 |
| POST | `/api/stop` | 暂停运行 |
| GET | `/api/config/world` | 世界配置 |
| GET | `/api/config/agents` | 角色配置 |
| GET | `/api/history` | 历史记录 |
| WS | `/ws` | WebSocket 实时推送 |

## 自定义世界观

在 `worlds/` 下创建新目录，包含 `world.yaml` 和 `agents.yaml`，启动时指定路径：

```bash
go run cmd/fable/main.go worlds/your-world
```

## License

MIT
