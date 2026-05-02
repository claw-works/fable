export interface AgentState {
  agent_id: string
  name: string
  tick: number
  game_time: string
  location: string
  action: string
  target?: string
  dialogue?: string
  emotion: string
  inner_thought: string
  memory_update: string[]
  relation_changes?: RelationChange[]
}

export interface RelationChange {
  target_id: string
  affinity_delta: number
  new_label?: string
  reason: string
}

export interface WorldState {
  tick: number
  game_time: string
  locations: Record<string, string[]>
  events: string[]
  agents: AgentState[]
}

export interface Location {
  name: string
  description: string
  connected: { name: string; distance: number }[]
  capacity?: number
}

export interface WorldConfig {
  name: string
  description: string
  locations: Location[]
  rules: string[]
  time_step: number
}

export interface AgentConfig {
  id: string
  name: string
  age: number
  occupation: string
  personality: string
  backstory: string
  relationships: Record<string, string>
  init_location: string
}

export interface PlayerConfig {
  id: string
  name: string
  age: number
  occupation: string
  personality: string
  backstory: string
  init_location: string
}

export interface PlayerState {
  player_id: string
  tick: number
  game_time: string
  location: string
  action: string
  target?: string
  dialogue?: string
}

export interface PlayerAction {
  type: 'move' | 'talk' | 'act' | 'skip'
  location?: string
  target?: string
  content?: string
}

export interface StreamEvent {
  type: 'agent_update' | 'event' | 'tick_start' | 'tick_end'
  agent_state?: AgentState
  text?: string
  game_time: string
  tick: number
}

export interface TickCardItem {
  icon: string
  label: string
  text: string
  cls: string
}

export interface TickCard {
  key: string
  tick: number
  time: string
  agentId: string
  name: string
  emotion: string
  location: string
  items: TickCardItem[]
  isPlayer?: boolean
}

export interface ConversationTurn {
  speaker: string
  content: string
  tick: number
}

export interface PlayerLog {
  time: string
  tick: number
  text: string
}
