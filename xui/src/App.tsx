import { useEffect } from 'react'
import { useStore } from './store'
import Header from './components/Header'
import LocationGrid from './components/LocationGrid'
import TickStream from './components/TickStream'
import PlayerPanel from './components/PlayerPanel'
import PlayerLog from './components/PlayerLog'
import ConversationDrawer from './components/ConversationDrawer'

export default function App() {
  const { loadInitialData, connect } = useStore()

  useEffect(() => {
    loadInitialData()
    connect()
  }, [])

  return (
    <>
      <Header />
      <main id="layout">
        <aside id="col-left">
          <div className="panel">
            <h2>📍 各处动态</h2>
            <LocationGrid />
          </div>
        </aside>
        <section id="col-center">
          <TickStream />
        </section>
        <aside id="col-right">
          <PlayerPanel />
          <PlayerLog />
        </aside>
      </main>
      <ConversationDrawer />
    </>
  )
}
