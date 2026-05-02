import { useEffect, useRef } from 'react'
import { useStore } from '../store'

function escapeHtml(s: string) {
  return s.replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c] || c))
}

export default function TickStream() {
  const { tickCards, focusAgent, setFocusAgent, resolveId, agentNames } = useStore()
  const ref = useRef<HTMLDivElement>(null)

  const cards = focusAgent ? tickCards.filter(c => c.agentId === focusAgent) : tickCards
  const list = cards.slice(-60)

  // Group by tick
  const groups: Record<number, typeof list> = {}
  list.forEach(c => { (groups[c.tick] ||= []).push(c) })
  const ticks = Object.keys(groups).map(Number).sort((a, b) => a - b)

  useEffect(() => { ref.current?.scrollTo(0, ref.current.scrollHeight) }, [tickCards.length])

  const focusName = focusAgent
    ? agentNames[focusAgent] || (focusAgent === 'player' ? useStore.getState().playerName : focusAgent)
    : null

  return (
    <div className="panel" id="timeline-panel">
      <div className="panel-header">
        <h2>{focusAgent ? '🎯 聚焦事件流' : '📜 事件流'}</h2>
        {focusAgent && (
          <div id="focus-bar">
            <span>聚焦：<strong>{focusName}</strong></span>
            <button onClick={() => setFocusAgent(null)}>✕ 清除</button>
          </div>
        )}
      </div>
      <div id="tick-stream" ref={ref}>
        {ticks.map(t => {
          const g = groups[t]
          return (
            <div className="tick-block" key={t}>
              <div className="tick-header">
                <span className="tick-no">Tick {t}</span>
                <span className="tick-time">{g[0].time}</span>
              </div>
              <div className="tick-cards">
                {g.map(c => (
                  <div key={c.key} className={`agent-card-item${c.isPlayer ? ' player' : ''}`} onClick={() => setFocusAgent(c.agentId)}>
                    <div className="aci-head">
                      <span className="aci-name">{c.isPlayer ? '🎭 ' : ''}{c.name}</span>
                      {c.emotion && <span className="aci-emotion">{c.emotion}</span>}
                      {c.location && <span className="aci-loc">📍{c.location}</span>}
                    </div>
                    <div className="aci-body">
                      {c.items.map((item, i) => (
                        <div key={i} className={`aci-line ${item.cls}`}>
                          <span className="aci-icon">{item.icon}</span>
                          <span className="aci-text" dangerouslySetInnerHTML={{ __html: escapeHtml(resolveId(item.text)) }} />
                        </div>
                      ))}
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )
        })}
      </div>
    </div>
  )
}
