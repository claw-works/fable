import { create } from 'zustand'
import type { AgentState, WorldState, WorldConfig, AgentConfig, PlayerState, PlayerAction, TickCard, TickCardItem, PlayerLog, ConversationTurn, StreamEvent } from './types'

interface Store {
  // State
  worldConfig: WorldConfig | null
  agentConfigs: AgentConfig[]
  agentNames: Record<string, string>
  lastState: WorldState | null
  tickCards: TickCard[]
  tickCardKeys: Record<string, true>
  focusAgent: string | null
  playerJoined: boolean
  playerName: string
  playerState: PlayerState | null
  playerLogs: PlayerLog[]
  playerLogKeys: Record<string, true>
  autopilotOn: boolean
  tickRunning: boolean
  wsConnected: boolean
  conversation: { active: boolean; npcId: string; npcName: string; history: ConversationTurn[] } | null
  currentSave: string

  // Actions
  loadInitialData: () => Promise<void>
  connect: () => void
  setFocusAgent: (id: string | null) => void
  resolveId: (text: string) => string
  doTick: () => Promise<void>
  doRunN: (n: number) => Promise<void>
  doAutoRun: () => Promise<void>
  doStop: () => Promise<void>
  doInterrupt: () => Promise<void>
  doJoin: (cfg: { name: string; age: number; occupation: string; personality: string; backstory: string; init_location: string }) => Promise<void>
  doLeave: () => Promise<void>
  submitAction: (action: PlayerAction) => Promise<void>
  toggleAutopilot: () => Promise<void>
  startConversation: (npcId: string) => Promise<void>
  sendMessage: (content: string) => Promise<string>
  endConversation: () => Promise<void>
  listSaves: () => Promise<string[]>
  newGame: (worldId: string, saveName: string) => Promise<void>
}

const api = async (path: string, opts?: RequestInit) => {
  const res = await fetch(path, opts)
  return res.json()
}
const post = (path: string, body?: unknown) =>
  api(path, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: body ? JSON.stringify(body) : undefined })

export const useStore = create<Store>((set, get) => ({
  worldConfig: null,
  agentConfigs: [],
  agentNames: {},
  lastState: null,
  tickCards: [],
  tickCardKeys: {},
  focusAgent: null,
  playerJoined: false,
  playerName: '',
  playerState: null,
  playerLogs: [],
  playerLogKeys: {},
  autopilotOn: false,
  tickRunning: false,
  wsConnected: false,
  conversation: null,
  currentSave: '',

  loadInitialData: async () => {
    try {
      const [wc, agents, history, state, ps, session] = await Promise.all([
        api('/api/config/world'),
        api('/api/config/agents'),
        api('/api/history?limit=100'),
        api('/api/state'),
        api('/api/player/state'),
        api('/api/session'),
      ])
      const names: Record<string, string> = {}
      ;(agents || []).forEach((a: AgentConfig) => { if (a.id && a.name) names[a.id] = a.name })
      set({ worldConfig: wc, agentConfigs: agents || [], agentNames: names, currentSave: session?.save_name || '' })

      // Ingest history
      ;(history || []).forEach((s: WorldState) => ingestFullState(s, get, set))
      // Ingest current
      set({ lastState: state })
      ingestFullState(state, get, set)

      // Player state
      if (ps?.player_id) {
        set({ playerJoined: true, playerName: ps.name || ps.player_id, playerState: ps })
      }
    } catch (e) { console.error('loadInitialData:', e) }
  },

  connect: () => {
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
    const ws = new WebSocket(`${proto}//${location.host}/ws`)
    ws.onopen = () => set({ wsConnected: true })
    ws.onclose = () => { set({ wsConnected: false }); setTimeout(() => get().connect(), 3000) }
    ws.onmessage = (e) => {
      try {
        const msg = JSON.parse(e.data) as StreamEvent & WorldState
        if (msg.type === 'tick_start') {
          set({ tickRunning: true })
        } else if (msg.type === 'tick_end') {
          set({ tickRunning: false })
        } else if (msg.type === 'agent_update' && msg.agent_state) {
          handleAgentUpdate(msg, get, set)
        } else if (msg.type === 'event') {
          if (msg.text) {
            addPlayerEventCard(msg.text, msg.tick, msg.game_time, get, set)
            addPlayerLog(msg.text, msg.tick, msg.game_time, get, set)
          }
        } else if (msg.tick !== undefined && !msg.type) {
          set({ lastState: msg as WorldState })
          ingestFullState(msg as WorldState, get, set)
        }
      } catch (err) { console.error('ws parse:', err) }
    }
  },

  setFocusAgent: (id) => {
    const { playerName } = get()
    const filterId = (playerName && id === playerName) ? 'player' : id
    set({ focusAgent: filterId })
  },

  resolveId: (text: string) => text,

  doTick: () => post('/api/tick'),
  doRunN: (n) => post('/api/run', { ticks: n }),
  doAutoRun: () => post('/api/start'),
  doStop: () => post('/api/stop'),
  doInterrupt: () => post('/api/player/interrupt'),

  doJoin: async (cfg) => {
    await post('/api/player/join', { id: 'player', ...cfg })
    set({ playerJoined: true, playerName: cfg.name, playerState: { player_id: 'player', tick: 0, game_time: '', location: cfg.init_location, action: '刚刚到达' } })
    addPlayerLog(`🎮 ${cfg.name} 加入了清水镇（${cfg.init_location}）`, 0, '', get, set)
  },

  doLeave: async () => {
    await fetch('/api/player/leave', { method: 'DELETE' })
    addPlayerLog('👋 离开了清水镇', 0, '', get, set)
    set({ playerJoined: false, playerName: '', playerState: null })
  },

  submitAction: async (action) => {
    await post('/api/player/action', action)
  },

  toggleAutopilot: async () => {
    const next = !get().autopilotOn
    await post('/api/player/autopilot', { enabled: next })
    set({ autopilotOn: next })
    addPlayerLog(next ? '🤖 已开启 AI 托管' : '🤖 已关闭 AI 托管', 0, '', get, set)
  },

  startConversation: async (npcId) => {
    const { agentNames } = get()
    const npcName = agentNames[npcId] || npcId
    await post('/api/conversation/start', { npc_id: npcId })
    set({ conversation: { active: true, npcId, npcName, history: [] } })
    addPlayerLog(`💬 开始与 ${npcName} 对话`, 0, '', get, set)
  },

  sendMessage: async (content) => {
    const conv = get().conversation
    if (!conv) return ''
    set({ conversation: { ...conv, history: [...conv.history, { speaker: 'player', content, tick: 0 }] } })
    const data = await post('/api/conversation/say', { content })
    const reply = data.reply || ''
    if (reply) {
      set(s => ({ conversation: s.conversation ? { ...s.conversation, history: [...s.conversation.history, { speaker: 'npc', content: reply, tick: 0 }] } : null }))
    }
    return reply
  },

  endConversation: async () => {
    await fetch('/api/conversation/end', { method: 'DELETE' })
    set({ conversation: null })
    addPlayerLog('💬 对话结束', 0, '', get, set)
  },

  listSaves: async () => {
    return api('/api/saves')
  },

  newGame: async (worldId, saveName) => {
    await post('/api/new-game', { world_id: worldId, save_name: saveName })
    // 重置前端状态
    set({ tickCards: [], tickCardKeys: {}, playerJoined: false, playerName: '', playerState: null, playerLogs: [], playerLogKeys: {}, conversation: null, lastState: null, focusAgent: null })
    await get().loadInitialData()
  },
}))

// ── Helpers ──

type Get = () => Store
type Set = (partial: Partial<Store> | ((s: Store) => Partial<Store>)) => void

function handleAgentUpdate(msg: StreamEvent, get: Get, set: Set) {
  const a = msg.agent_state!
  const s = get()
  const lastState = s.lastState ? { ...s.lastState } : null
  if (lastState) {
    const agents = [...lastState.agents]
    const idx = agents.findIndex(x => x.agent_id === a.agent_id)
    if (idx >= 0) agents[idx] = a; else agents.push(a)
    const locs = { ...lastState.locations }
    for (const [loc, ids] of Object.entries(locs)) {
      locs[loc] = ids.filter(id => id !== a.agent_id)
    }
    if (a.location) {
      locs[a.location] = [...(locs[a.location] || []), a.agent_id]
    }
    set({ lastState: { ...lastState, agents, locations: locs, game_time: msg.game_time || lastState.game_time, tick: msg.tick ?? lastState.tick } })
  }
  addAgentTickCard(a, msg.tick, msg.game_time, get, set)

  // NPC 对玩家的行动/对话 → 写入玩家日志
  const pn = s.playerName
  if (pn && a.agent_id !== 'player' && a.target && (a.target === pn || a.target === 'player')) {
    const name = a.name || a.agent_id
    if (a.dialogue) {
      addPlayerLog(`💬 ${name} 对你说：「${a.dialogue}」`, msg.tick, msg.game_time, get, set)
    } else if (a.action) {
      addPlayerLog(`⚡ ${name} 对你${a.action}`, msg.tick, msg.game_time, get, set)
    }
  }
}

function addAgentTickCard(a: AgentState, tick: number, time: string, get: Get, set: Set) {
  const items: TickCardItem[] = []
  if (a.action) items.push({ icon: '⚡', label: '行动', text: a.action, cls: 'action' })
  if (a.dialogue) items.push({ icon: '💬', label: '对话', text: `「${a.dialogue}」`, cls: 'dialogue' })
  if (a.inner_thought) items.push({ icon: '💭', label: '内心', text: a.inner_thought, cls: 'thought' })
  if (a.memory_update?.length) items.push({ icon: '📝', label: '记忆', text: a.memory_update.join('；'), cls: 'memory' })
  if (!items.length) return

  const key = `${tick}::${a.agent_id}`
  const s = get()
  const cards = [...s.tickCards]
  const keys = { ...s.tickCardKeys }
  const existing = cards.findIndex(c => c.key === key)
  const card: TickCard = { key, tick, time, agentId: a.agent_id, name: a.name || a.agent_id, emotion: a.emotion || '', location: a.location || '', items }
  if (existing >= 0) cards[existing] = card
  else { cards.push(card); keys[key] = true }
  if (cards.length > 300) { const rm = cards.splice(0, cards.length - 300); rm.forEach(c => delete keys[c.key]) }
  set({ tickCards: cards, tickCardKeys: keys })
}

function addPlayerEventCard(text: string, tick: number, time: string, get: Get, set: Set) {
  if (!text) return
  const itemKey = `${tick}::player::${text}`
  const s = get()
  if (s.tickCardKeys[itemKey]) return
  const cardKey = `${tick}::player-event`
  const cards = [...s.tickCards]
  const keys = { ...s.tickCardKeys }
  keys[itemKey] = true
  const item: TickCardItem = { icon: '🎭', label: '玩家', text: text.replace(/^【玩家】/, ''), cls: 'player-event' }
  const existing = cards.findIndex(c => c.key === cardKey)
  if (existing >= 0) {
    const c = { ...cards[existing], items: [...cards[existing].items] }
    if (!c.items.some(i => i.text === item.text)) c.items.push(item)
    cards[existing] = c
  } else {
    cards.push({ key: cardKey, tick, time, agentId: 'player', name: s.playerName || '玩家', emotion: '', location: '', items: [item], isPlayer: true })
    keys[cardKey] = true
  }
  if (cards.length > 300) { const rm = cards.splice(0, cards.length - 300); rm.forEach(c => delete keys[c.key]) }
  set({ tickCards: cards, tickCardKeys: keys })
}

function addPlayerLog(text: string, tick: number, time: string, get: Get, set: Set) {
  const s = get()
  const t = time || s.lastState?.game_time || ''
  const k = tick || s.lastState?.tick || 0
  const logKey = `${k}::${text}`
  if (s.playerLogKeys[logKey]) return
  const logs = [{ time: t, tick: k, text }, ...s.playerLogs]
  if (logs.length > 100) logs.length = 100
  const logKeys = { ...s.playerLogKeys }
  logKeys[logKey] = true
  set({ playerLogs: logs, playerLogKeys: logKeys })
}

function ingestFullState(state: WorldState, get: Get, set: Set) {
  ;(state.agents || []).forEach(a => addAgentTickCard(a, state.tick, state.game_time, get, set))
  ;(state.events || []).forEach(e => {
    if (e.startsWith('【')) {
      addPlayerEventCard(e, state.tick, state.game_time, get, set)
      addPlayerLog(e, state.tick, state.game_time, get, set)
    }
  })
}
