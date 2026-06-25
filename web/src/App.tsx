import { useState, useCallback } from 'react'
import { useSites } from './hooks/useSites'
import SiteSidebar from './components/SiteSidebar'
import ChatGrid from './components/ChatGrid'
import InputArea from './components/InputArea'
import type { Message } from './types'

interface ChatResponse {
  session_id: string
  results: Record<string, string>
}

export default function App() {
  const { sites, selectedSites, toggleSite } = useSites()
  const [messages, setMessages] = useState<Message[]>([])

  const handleSend = useCallback(
    async (prompt: string) => {
      const siteIds = Array.from(selectedSites)
      if (siteIds.length === 0) return

      const res = await fetch('/api/chat', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ prompt, site_ids: siteIds }),
      })

      if (!res.ok) {
        console.error('Chat request failed:', res.status)
        return
      }

      const data: ChatResponse = await res.json()
      const newMessages: Message[] = []

      for (const siteId of siteIds) {
        const result = data.results[siteId] || ''
        const hasError = result.startsWith('ERROR: ')
        newMessages.push({
          id: `${data.session_id}-${siteId}`,
          session_id: data.session_id,
          site_id: siteId,
          content: hasError ? '' : result,
          kept: false,
          error: hasError ? result.slice(7) : '',
          elapsed_ms: 0,
          created_at: new Date().toISOString(),
        })
      }

      setMessages((prev) => [...prev, ...newMessages])
    },
    [selectedSites],
  )

  const handleToggleKeep = useCallback((id: string) => {
    setMessages((prev) =>
      prev.map((msg) =>
        msg.id === id ? { ...msg, kept: !msg.kept } : msg,
      ),
    )
  }, [])

  return (
    <div className="flex h-screen flex-col bg-slate-950 text-white">
      <header className="border-b border-slate-700 bg-slate-900 px-4 py-3 text-lg font-semibold">
        Chat Aggregator
      </header>
      <div className="flex flex-1 overflow-hidden">
        <SiteSidebar
          sites={sites}
          selectedSites={selectedSites}
          toggleSite={toggleSite}
        />
        <main className="flex flex-1 flex-col overflow-hidden">
          <div className="flex-1 overflow-auto">
            <ChatGrid
              messages={messages}
              sites={sites}
              onToggleKeep={handleToggleKeep}
            />
          </div>
          <InputArea onSend={handleSend} />
        </main>
      </div>
    </div>
  )
}
