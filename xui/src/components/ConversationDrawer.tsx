import { useState } from 'react'
import { useStore } from '../store'

export default function ConversationDrawer() {
  const { conversation, sendMessage, endConversation } = useStore()
  const [input, setInput] = useState('')
  const [loading, setLoading] = useState(false)

  if (!conversation?.active) return null

  const handleSend = async () => {
    const text = input.trim()
    if (!text || loading) return
    setInput('')
    setLoading(true)
    await sendMessage(text)
    setLoading(false)
  }

  return (
    <div id="conv-drawer" className="drawer open">
      <div className="drawer-header">
        <h3>💬 与 {conversation.npcName} 对话</h3>
        <button onClick={endConversation}>✕ 结束</button>
      </div>
      <div id="conv-messages">
        <div className="conv-system">你走向{conversation.npcName}，开始了一段对话…</div>
        {conversation.history.map((t, i) => (
          <div key={i} className={`conv-msg ${t.speaker === 'player' ? 'player' : 'npc'}`}>
            {t.speaker === 'player' ? `你：${t.content}` : t.content}
          </div>
        ))}
        {loading && <div className="conv-msg npc loading">思考中…</div>}
      </div>
      <div className="drawer-input">
        <input
          value={input}
          onChange={e => setInput(e.target.value)}
          onKeyDown={e => e.key === 'Enter' && handleSend()}
          placeholder="说点什么…"
        />
        <button onClick={handleSend}>发送</button>
      </div>
    </div>
  )
}
