# Fable 设计笔记

## 一、Tick 机制

### 基本定义
- 1 Tick = 游戏内 30 分钟（世界叙事单位）
- 现实时间：正常模式每 5 秒触发一次 Tick
- 一次行动可以消耗**多个 Tick**（如移动需要 2 Tick）

### SimulationMode
```go
type SimulationMode string

const (
    ModeAuto         SimulationMode = "auto"          // 正常自动
    ModeSlow         SimulationMode = "slow"           // 慢速（玩家交互中）
    ModePlayerDriven SimulationMode = "player_driven"  // 纯手动每步
)
```

---

## 二、对话子系统

### 核心原则
- **对话不消耗 Tick**，多轮对话在同一个 Tick 内完成
- 对话结束后，摘要作为"本 Tick 发生的事"注入世界事件
- 对话过程中可以触发行动事件，行动事件**消耗 Tick**

### 流程
```
Tick N 开始
  → 玩家选择和某 NPC 对话
  → 对话子系统启动，世界切换为 slow/暂停
  → 玩家和 NPC 来回多轮（不消耗 Tick）
  → 对话中触发行动（如"我要买一把刀"）→ 消耗 1 Tick
  → 玩家主动结束对话
  → 对话摘要注入本 Tick 世界事件
  → Tick N 正式结束，其他 NPC 行动
  → Tick N+1 开始
```

### ConversationSession 结构
```go
type ConversationTurn struct {
    Speaker string `json:"speaker"` // "player" 或 agent_id
    Content string `json:"content"`
    Tick    int    `json:"tick"`
}

type ConversationSession struct {
    PlayerID  string             `json:"player_id"`
    NPCid     string             `json:"npc_id"`
    History   []ConversationTurn `json:"history"`
    StartTick int                `json:"start_tick"`
}
```

---

## 三、玩家行动机制

### 核心原则
- 玩家行动**完全自主**，AI 不替玩家做决定
- AI 扮演"GM（游戏主持人）"角色：
  - 🎭 扮演 NPC 响应玩家行为
  - ⚖️ 裁判行动合理性（位置约束、世界规则、角色能力）
  - 📖 叙述行动结果
  - 🧠 让 NPC 记住发生的事

### 行动流程
```
玩家自然语言输入意图
  → AI 判断合理性（位置是否正确、NPC 是否在场等）
  → 合理：执行，消耗对应 Tick 数，NPC 响应
  → 不合理：AI 给出反馈（"你不在集市，买不了东西"）
```

---

## 四、地理系统

### 数据结构
地点之间图结构，带距离权重：

```go
type Connection struct {
    Target   string `yaml:"name"`
    Distance int    `yaml:"distance"` // 移动消耗的 Tick 数
}

type Location struct {
    Name        string       `yaml:"name"`
    Description string       `yaml:"description"`
    Connected   []Connection `yaml:"connected"`
    Capacity    int          `yaml:"capacity"` // 可选，最多容纳几人
}
```

### 三大原则
1. **距离** → 移动消耗 Tick（相邻 1 Tick，远处 2-3 Tick）
2. **可见性** → 只有同地点才能互动/听到对话
3. **信息局部性** → NPC 只知道自己亲历或被人告知的事

### 信息传播示例
```
张铁山在铁匠铺骂了人
  → 只有铁匠铺在场的人听到
  → 刘小满路过集市，听路人提起
  → 陈掌柜在茶馆，要等人来告诉他才知道
```

### 玩家移动
```
玩家："我想去渡口找王渡生"
  → AI 查路径：茶馆 → 集市 → 渡口，距离 2
  → 消耗 2 Tick，世界时间推进 1 小时
  → 到达渡口，王渡生可能不在（取决于他当时的行动）
```

---

## 五、地图渲染方案

### 方案：Tiled 数据结构 + Canvas 渲染
- **数据格式**：使用 Tiled 标准 JSON（.tmj），成熟开放格式
- **渲染**：自己写 HTML5 Canvas，轻量无依赖
- **好处**：数据和渲染解耦，后期可无缝换 Phaser 等引擎

### worlds 目录结构
```
worlds/example/
├── manifest.yaml   ← 世界元信息（id、版本、作者）
├── world.yaml      ← 逻辑层：NPC规则、连接关系、游戏规则
├── agents.yaml     ← 角色配置
└── map.tmj         ← Tiled 地图文件（瓦片、地点坐标、通道）
```

### world.yaml 与 map.tmj 关联
- 两者通过 `location_id` 关联，完全解耦
- `map.tmj` objects layer 存地点坐标和区域
- `world.yaml` 存逻辑规则和连接关系

### Tiled map.tmj 结构示例
```json
{
  "layers": [
    { "name": "ground",    "type": "tilelayer" },
    { "name": "buildings", "type": "tilelayer" },
    {
      "name": "objects",
      "type": "objectgroup",
      "objects": [
        {
          "name": "茶馆",
          "type": "location",
          "x": 160, "y": 96,
          "width": 64, "height": 64,
          "properties": [
            { "name": "location_id", "value": "teahouse" }
          ]
        }
      ]
    }
  ]
}
```

### Canvas 渲染层次
1. 读瓦片层 → 画地面和建筑
2. 读 objects 层 → 标记地点区域和名称
3. 叠加 NPC 实时位置 → 画小人/图标
4. 移动动画 → NPC 从 A 走向 B

---

## 六、世界实例存储（~/.fable）

### 目录结构
```
~/.fable/
├── config.yaml          ← 全局配置（LLM key、服务端口等）
├── worlds/
│   ├── qingshui-town/   ← 内置示例世界
│   │   ├── manifest.yaml
│   │   ├── world.yaml
│   │   ├── agents.yaml
│   │   └── map.tmj
│   └── my-world/        ← 用户自己创建的世界
└── saves/
    └── qingshui-town/   ← 存档（运行时状态、NPC记忆、历史）
        ├── save_20260418_1100.json
        └── latest.json
```

### 加载优先级
```
1. 命令行指定：fable --world ~/.fable/worlds/qingshui-town
2. 默认：~/.fable/worlds/ 里第一个世界
3. fallback：./worlds/example/（开发调试用）
```

### 首次启动行为
- 检测 `~/.fable/` 是否存在
- 不存在则自动初始化，复制内置示例世界（qingshui-town）
- 提示用户编辑 `~/.fable/config.yaml` 填入 LLM key

### config.yaml 路径优先级
```
1. 命令行 --config 指定
2. ~/.fable/config.yaml
3. ./config.yaml（开发调试用）
```

---

## 七、待实现清单

- [ ] 地点连接加 distance 字段
- [ ] world.yaml 地点格式更新（Connection 结构）
- [ ] 创建清水镇 map.tmj（Tiled 格式）
- [ ] Canvas 地图渲染器（frontend）
- [ ] 对话子系统（ConversationSession）
- [ ] SimulationMode 切换逻辑
- [ ] 玩家行动 GM 裁判逻辑
- [ ] NPC 信息局部性（只知道自己在场的事）
- [ ] 玩家移动路径计算（消耗多 Tick）
