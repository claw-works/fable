import { useStore } from '../store'

export default function PlayerLog() {
  const { playerLogs, playerJoined } = useStore()
  if (!playerJoined) return null
  return (
    <div className="panel" id="player-log-section">
      <h2>📋 玩家日志</h2>
      <div id="player-log">
        {playerLogs.map((l, i) => (
          <div key={i} className="plog-item">
            <span className="tick">T{l.tick}</span>
            <span className="time">{l.time}</span> {l.text}
          </div>
        ))}
      </div>
    </div>
  )
}
