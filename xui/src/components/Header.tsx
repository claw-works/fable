import { useEffect, useRef, useState } from 'react'
import { useStore } from '../store'

export default function Header() {
  const { lastState, wsConnected, worldConfig } = useStore()
  const name = worldConfig?.name || '清水镇'
  const [showMenu, setShowMenu] = useState(false)
  const menuRef = useRef<HTMLSpanElement>(null)

  useEffect(() => {
    if (!showMenu) return
    const handler = (e: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) setShowMenu(false)
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [showMenu])

  return (
    <header>
      <h1>🏘 {name}</h1>
      <div id="status">
        <span id="game-time">{lastState?.game_time || 'Day1 08:00'}</span>
        <span id="tick-count">Tick: {lastState?.tick || 0}</span>
        <span className={wsConnected ? 'connected' : 'disconnected'}>● {wsConnected ? '已连接' : '未连接'}</span>
        <span style={{ position: 'relative' }} ref={menuRef}>
          <button className="menu-btn" onClick={() => setShowMenu(!showMenu)}>⚙</button>
          {showMenu && <GameMenu onClose={() => setShowMenu(false)} />}
        </span>
      </div>
    </header>
  )
}

function GameMenu({ onClose }: { onClose: () => void }) {
  const { listSaves, newGame, currentSave } = useStore()
  const [saves, setSaves] = useState<string[]>([])
  const [newName, setNewName] = useState('')
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    listSaves().then(s => setSaves(Array.isArray(s) ? s : []))
  }, [])

  const handleNew = async () => {
    const name = newName.trim() || `save-${Date.now()}`
    setLoading(true)
    try {
      await newGame('qingshui-town', name)
      onClose()
    } finally { setLoading(false) }
  }

  const handleLoad = async (saveName: string) => {
    setLoading(true)
    try {
      await newGame('qingshui-town', saveName)
      onClose()
    } finally { setLoading(false) }
  }

  return (
    <div className="game-menu" onClick={e => e.stopPropagation()}>
      <div className="gm-section">
        <strong>新建游戏</strong>
        <div style={{ display: 'flex', gap: '0.4rem', marginTop: '0.3rem' }}>
          <input placeholder="存档名（可选）" value={newName} onChange={e => setNewName(e.target.value)} style={{ flex: 1 }} />
          <button onClick={handleNew} disabled={loading}>{loading ? '...' : '创建'}</button>
        </div>
      </div>
      <div className="gm-section">
        <strong>存档列表</strong>
        {saves.length === 0
          ? <div style={{ color: '#888', fontSize: '0.8rem', marginTop: '0.3rem' }}>暂无存档</div>
          : <div className="gm-saves">{saves.map(s => (
              <div key={s} className={`gm-save-item${s === currentSave ? ' active' : ''}`} onClick={() => handleLoad(s)}>
                <span>{s}</span>
                {s === currentSave && <span style={{ color: '#6c6', fontSize: '0.75rem' }}>● 当前</span>}
              </div>
            ))}</div>
        }
      </div>
      <button className="gm-close" onClick={onClose}>关闭</button>
    </div>
  )
}
