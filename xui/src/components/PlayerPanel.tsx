import { useState } from 'react'
import { useStore } from '../store'

export default function PlayerPanel() {
  const s = useStore()
  const { playerJoined, playerName, playerState, tickRunning } = s

  if (!playerJoined) return <NotJoined />
  return (
    <div className="panel" id="player-section">
      <h2>🎭 {playerName || '玩家'}</h2>
      <div id="player-info">📍 {playerState?.location} · {playerState?.action || ''}</div>
      <ActionButtons />
      <hr style={{ borderColor: '#333', margin: '0.8rem 0' }} />
      <SimControls />
      {tickRunning && <div id="tick-overlay"><span>🌀 世界运转中…</span></div>}
    </div>
  )
}

function NotJoined() {
  const [showForm, setShowForm] = useState(false)
  const { doJoin, worldConfig } = useStore()
  const [form, setForm] = useState({ name: '李逍遥', age: 22, occupation: '游侠', personality: '豪爽仗义', backstory: '江湖漂泊的年轻侠客', init_location: '' })

  if (!showForm) {
    return (
      <div className="panel" id="player-section">
        <h2>🎭 玩家模式</h2>
        <button onClick={() => setShowForm(true)}>加入小镇</button>
      </div>
    )
  }

  const locs = worldConfig?.locations || []
  const loc = form.init_location || locs[0]?.name || ''

  return (
    <div className="panel" id="player-section">
      <h2>🎭 玩家模式</h2>
      <div id="join-form">
        <div className="form-row"><label>姓名</label><input value={form.name} onChange={e => setForm({ ...form, name: e.target.value })} /></div>
        <div className="form-row"><label>年龄</label><input type="number" value={form.age} onChange={e => setForm({ ...form, age: +e.target.value })} /></div>
        <div className="form-row"><label>职业</label><input value={form.occupation} onChange={e => setForm({ ...form, occupation: e.target.value })} /></div>
        <div className="form-row"><label>性格</label><input value={form.personality} onChange={e => setForm({ ...form, personality: e.target.value })} /></div>
        <div className="form-row"><label>背景</label><textarea rows={2} value={form.backstory} onChange={e => setForm({ ...form, backstory: e.target.value })} /></div>
        <div className="form-row"><label>起始地点</label>
          <select value={loc} onChange={e => setForm({ ...form, init_location: e.target.value })}>
            {locs.map(l => <option key={l.name} value={l.name}>{l.name}</option>)}
          </select>
        </div>
        <div className="form-actions">
          <button onClick={() => doJoin({ ...form, init_location: loc })}>确认加入</button>
          <button onClick={() => setShowForm(false)}>取消</button>
        </div>
      </div>
    </div>
  )
}

function ActionButtons() {
  const { submitAction, lastState, playerState, agentNames, tickRunning } = useStore()
  const [panel, setPanel] = useState<'none' | 'move' | 'act' | 'conv'>('none')
  const [moveTarget, setMoveTarget] = useState('')
  const [actContent, setActContent] = useState('')
  const [convTarget, setConvTarget] = useState('')
  const { worldConfig, startConversation } = useStore()

  // NPCs at same location as player
  const sameLocNpcs = (() => {
    if (!lastState || !playerState) return []
    const loc = playerState.location
    const ids = lastState.locations[loc] || []
    return ids.filter(id => id !== 'player' && id !== useStore.getState().playerName)
  })()

  return (
    <div style={{ opacity: tickRunning ? 0.4 : 1, pointerEvents: tickRunning ? 'none' : 'auto' }}>
      <div className="player-actions">
        <button onClick={() => setPanel(panel === 'move' ? 'none' : 'move')}>🚶 移动</button>
        <button onClick={() => setPanel(panel === 'act' ? 'none' : 'act')}>⚡ 行动</button>
        <button onClick={() => setPanel(panel === 'conv' ? 'none' : 'conv')}>💬 对话</button>
        <button onClick={() => useStore.getState().doLeave()}>🚪 离开</button>
      </div>
      {panel === 'move' && (
        <div className="action-sub">
          <select value={moveTarget} onChange={e => setMoveTarget(e.target.value)}>
            <option value="">选择地点</option>
            {(worldConfig?.locations || []).map(l => <option key={l.name} value={l.name}>{l.name}</option>)}
          </select>
          <button onClick={() => { if (moveTarget) { submitAction({ type: 'move', location: moveTarget }); setPanel('none') } }}>前往</button>
        </div>
      )}
      {panel === 'act' && (
        <div className="action-sub">
          <input placeholder="描述你的行动…" value={actContent} onChange={e => setActContent(e.target.value)} />
          <button onClick={() => { if (actContent) { submitAction({ type: 'act', content: actContent }); setActContent(''); setPanel('none') } }}>执行</button>
        </div>
      )}
      {panel === 'conv' && (
        <div className="action-sub">
          <select value={convTarget} onChange={e => setConvTarget(e.target.value)}>
            <option value="">选择对象</option>
            {sameLocNpcs.map(id => <option key={id} value={id}>{agentNames[id] || id}</option>)}
          </select>
          <button onClick={() => { if (convTarget) { startConversation(convTarget); setPanel('none') } }}>开始对话</button>
        </div>
      )}
    </div>
  )
}

function SimControls() {
  const { doTick, doRunN, doAutoRun, doInterrupt, doAutoRun: doResume, toggleAutopilot, autopilotOn, tickRunning } = useStore()
  const [runN, setRunN] = useState(6)

  return (
    <div className="sim-controls" style={{ opacity: tickRunning ? 0.4 : 1, pointerEvents: tickRunning ? 'none' : 'auto' }}>
      <span style={{ fontSize: '0.85rem', color: '#aaa' }}>推进控制</span>
      <div className="sim-btn-row">
        <button onClick={() => doTick()}>⏭ 1步</button>
        <button onClick={() => doRunN(runN)}>▶ <input type="number" value={runN} min={1} max={200} style={{ width: '3rem', textAlign: 'center' }} onClick={e => e.stopPropagation()} onChange={e => setRunN(+e.target.value)} />步</button>
        <button onClick={() => doAutoRun()}>⏩ 自动</button>
        <button onClick={() => doInterrupt()}>⏸ 暂停</button>
        <button onClick={() => doResume()}>▶ 继续</button>
      </div>
      <div className="sim-btn-row" style={{ marginTop: '0.3rem' }}>
        <button onClick={() => toggleAutopilot()} style={{ background: autopilotOn ? '#2d5a27' : undefined }}>
          🤖 {autopilotOn ? '托管中' : '托管'}
        </button>
      </div>
    </div>
  )
}
