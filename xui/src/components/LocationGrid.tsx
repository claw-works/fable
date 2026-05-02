import { useStore } from '../store'

export default function LocationGrid() {
  const { lastState, agentNames, playerName, focusAgent, setFocusAgent } = useStore()
  if (!lastState) return null
  const agentMap = Object.fromEntries((lastState.agents || []).map(a => [a.agent_id, a]))
  const locs = lastState.locations || {}

  return (
    <div id="location-grid">
      {Object.entries(locs).map(([loc, ids]) => (
        <div className="location-card" key={loc}>
          <h3>{loc}</h3>
          <div className="agents">
            {(ids || []).length === 0 && <em>无人</em>}
            {(ids || []).map(id => {
              const a = agentMap[id]
              const name = a?.name || agentNames[id] || (playerName && id === playerName ? playerName : id)
              const emotion = a?.emotion || ''
              const isFocused = focusAgent === id || (playerName && id === playerName && focusAgent === 'player')
              return (
                <span key={id} className={`agent-tag${isFocused ? ' focused' : ''}`} onClick={() => setFocusAgent(id)}>
                  {name}{emotion ? ` · ${emotion}` : ''}
                </span>
              )
            })}
          </div>
        </div>
      ))}
    </div>
  )
}
