import { useState, useCallback, useRef } from 'react'
import { useSites } from './hooks/useSites'
import { useWebSocket } from './hooks/useWebSocket'
import SiteSidebar from './components/SiteSidebar'
import ChatGrid from './components/ChatGrid'
import InputArea from './components/InputArea'
import ExportPanel from './components/ExportPanel'
import SiteConfigModal from './components/SiteConfigModal'
import HistoryPanel from './components/HistoryPanel'
import ResizeHandle from './components/ResizeHandle'
import type { Message, Site } from './types'

interface SiteFormData {
  id: string
  name: string
  url: string
  engine_type: string
  selectors: string
  format_prompt: string
}

interface ChatResponse {
  session_id: string
}

interface WSMessage {
  type: string
  session_id: string
  message_id?: string
  site_id?: string
  content?: string
  error?: string
  elapsed_ms?: number
  done: boolean
}

interface UpdateKeptPayload {
  message_id: string
  kept: boolean
}

interface TurnInfo {
  turn: number
  prompt: string
  siteIds: string[]
}

export default function App() {
  const { sites, selectedSites, toggleSite, fetchSites } = useSites()
  const [messages, setMessages] = useState<Message[]>([])
  const [isLoading, setIsLoading] = useState(false)
  const [currentSessionId, setCurrentSessionId] = useState<string | null>(null)
  const [showConfig, setShowConfig] = useState(false)
  const [editingSite, setEditingSite] = useState<SiteFormData | null>(null)
  const [turns, setTurns] = useState<TurnInfo[]>([])
  const [historyRefresh, setHistoryRefresh] = useState(0)
  const [historyCollapsed, setHistoryCollapsed] = useState(false)
  const [historyWidth, setHistoryWidth] = useState(256)
  const [sidebarWidth, setSidebarWidth] = useState(224)
  const [inputHeight, setInputHeight] = useState(80)

  const currentTurnRef = useRef(0)

  useWebSocket((msg: WSMessage) => {
    if (msg.type === 'message' && msg.site_id) {
      const siteId = msg.site_id
      const content = msg.content || ''
      const error = msg.error || ''
      const elapsedMs = msg.elapsed_ms || 0
      const messageId = msg.message_id || ''
      const turn = currentTurnRef.current

      setMessages((prev) => {
        const id = `${msg.session_id}-${siteId}-${turn}`
        const exists = prev.find((m) => m.id === id)
        if (exists) {
          return prev.map((m) =>
            m.id === id
              ? {
                  ...m,
                  content,
                  error,
                  elapsed_ms: elapsedMs,
                  loading: false,
                  message_id: messageId || m.message_id,
                }
              : m,
          )
        }
        return [
          ...prev,
          {
            id,
            message_id: messageId,
            session_id: msg.session_id,
            site_id: siteId,
            content,
            kept: false,
            error,
            elapsed_ms: elapsedMs,
            created_at: new Date().toISOString(),
            loading: false,
            turn,
          },
        ]
      })
    } else if (msg.type === 'complete') {
      setIsLoading(false)
    }
  })

  const handleSend = useCallback(
    async (prompt: string) => {
      const siteIds = Array.from(selectedSites)
      if (siteIds.length === 0) return

      const nextTurn = currentTurnRef.current + 1
      currentTurnRef.current = nextTurn

      const res = await fetch('/api/chat', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          prompt,
          site_ids: siteIds,
          session_id: currentSessionId || undefined,
          turn: nextTurn,
        }),
      })

      if (!res.ok) {
        console.error('Chat request failed:', res.status)
        return
      }

      const data: ChatResponse = await res.json()
      if (!currentSessionId) {
        setCurrentSessionId(data.session_id)
        setHistoryRefresh((v) => v + 1)
      }
      setIsLoading(true)

      setTurns((prev) => [
        ...prev,
        { turn: nextTurn, prompt, siteIds },
      ])

      const newMessages: Message[] = []
      for (const siteId of siteIds) {
        newMessages.push({
          id: `${data.session_id}-${siteId}-${nextTurn}`,
          session_id: data.session_id,
          site_id: siteId,
          content: '',
          kept: false,
          error: '',
          elapsed_ms: 0,
          created_at: new Date().toISOString(),
          loading: true,
          turn: nextTurn,
        })
      }

      setMessages((prev) => [...prev, ...newMessages])
    },
    [selectedSites, currentSessionId],
  )

  const handleToggleKeep = useCallback(
    async (id: string) => {
      const msg = messages.find((m) => m.id === id)
      if (!msg?.message_id) {
        return
      }

      const newKept = !msg.kept
      const payload: UpdateKeptPayload = {
        message_id: msg.message_id,
        kept: newKept,
      }

      setMessages((prev) =>
        prev.map((m) =>
          m.id === id ? { ...m, kept: newKept } : m,
        ),
      )

      try {
        const res = await fetch('/api/messages/kept', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload),
        })
        if (!res.ok) {
          throw new Error(`HTTP ${res.status}`)
        }
      } catch (err) {
        console.error('Failed to update keep state:', err)
        setMessages((prev) =>
          prev.map((m) =>
            m.id === id ? { ...m, kept: msg.kept } : m,
          ),
        )
      }
    },
    [messages],
  )

  const openNewSite = useCallback(() => {
    setEditingSite(null)
    setShowConfig(true)
  }, [])

  const openEditSite = useCallback((site: Site) => {
    setEditingSite({
      id: site.id,
      name: site.name,
      url: site.url,
      engine_type: site.engine_type,
      selectors: site.selectors || '',
      format_prompt: site.format_prompt || '',
    })
    setShowConfig(true)
  }, [])

  const closeConfig = useCallback(() => {
    setShowConfig(false)
    setEditingSite(null)
  }, [])

  const handleSaveSite = useCallback(
    async (formData: SiteFormData) => {
      const isNew = !editingSite
      const url = isNew ? '/api/sites' : `/api/sites/${formData.id}`
      const method = isNew ? 'POST' : 'PUT'

      let selectorsObj: Record<string, string> = {}
      const trimmed = formData.selectors.trim()
      if (trimmed) {
        try {
          const parsed = JSON.parse(trimmed)
          if (typeof parsed === 'object' && parsed !== null && !Array.isArray(parsed)) {
            selectorsObj = parsed as Record<string, string>
          } else {
            alert('Selectors 必须是 JSON 对象')
            return
          }
        } catch {
          alert('Selectors JSON 格式错误，请检查')
          return
        }
      }

      try {
        const res = await fetch(url, {
          method,
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            id: formData.id,
            name: formData.name,
            url: formData.url,
            engine_type: formData.engine_type,
            selectors: selectorsObj,
            format_prompt: formData.format_prompt,
          }),
        })
        if (!res.ok) {
          const errBody = await res.json().catch(() => ({ error: `HTTP ${res.status}` }))
          alert(`保存失败: ${errBody.error || res.statusText}`)
          return
        }
        closeConfig()
        fetchSites()
      } catch (err) {
        alert(`保存失败: ${err}`)
      }
    },
    [editingSite, closeConfig, fetchSites],
  )

  const handleSelectSession = useCallback(
    async (sessionId: string) => {
      try {
        const res = await fetch(`/api/sessions/${sessionId}/messages`)
        if (!res.ok) {
          throw new Error(`HTTP ${res.status}`)
        }
        const data = await res.json()
        const loadedMessages: Message[] = data.map((msg: Record<string, unknown>) => ({
          id: `${msg.session_id}-${msg.site_id}-${msg.turn || 0}`,
          message_id: String(msg.id || ''),
          session_id: String(msg.session_id || ''),
          site_id: String(msg.site_id || ''),
          content: String(msg.content || ''),
          kept: Boolean(msg.kept),
          error: String(msg.error || ''),
          elapsed_ms: Number(msg.elapsed_ms || 0),
          created_at: String(msg.created_at || ''),
          loading: false,
          turn: Number(msg.turn || 0),
        }))

        const turnMap = new Map<number, { prompt: string; siteIds: string[] }>()
        for (const msg of loadedMessages) {
          const turn = msg.turn || 0
          if (!turnMap.has(turn)) {
            const prompt = typeof data.find((m: Record<string, unknown>) => Number(m.turn || 0) === turn)?.prompt === 'string'
              ? String(data.find((m: Record<string, unknown>) => Number(m.turn || 0) === turn)?.prompt || '')
              : ''
            turnMap.set(turn, { prompt, siteIds: [] })
          }
          turnMap.get(turn)!.siteIds.push(msg.site_id)
        }

        const loadedTurns: TurnInfo[] = Array.from(turnMap.entries())
          .sort((a, b) => a[0] - b[0])
          .map(([turn, info]) => ({
            turn,
            prompt: info.prompt,
            siteIds: info.siteIds,
          }))

        setCurrentSessionId(sessionId)
        setMessages(loadedMessages)
        setIsLoading(false)
        currentTurnRef.current = loadedTurns.length > 0 ? loadedTurns[loadedTurns.length - 1].turn : 0
        setTurns(loadedTurns)
      } catch (err) {
        console.error('Failed to load session messages:', err)
      }
    },
    [],
  )

  const handleNewChat = useCallback(() => {
    setCurrentSessionId(null)
    setMessages([])
    setIsLoading(false)
    currentTurnRef.current = 0
    setTurns([])
  }, [])

  const handleDeleteSessions = useCallback(
    async (sessionIds: string[]) => {
      if (sessionIds.length === 0) return
      try {
        for (const id of sessionIds) {
          await fetch(`/api/sessions/${id}`, { method: 'DELETE' })
        }
        if (currentSessionId && sessionIds.includes(currentSessionId)) {
          handleNewChat()
        }
        setHistoryRefresh((v) => v + 1)
      } catch (err) {
        console.error('Failed to delete sessions:', err)
        alert(`删除失败: ${err}`)
      }
    },
    [currentSessionId, handleNewChat],
  )

  return (
    <div className="flex h-screen flex-col bg-slate-950 text-white">
      <header className="flex h-12 items-center justify-between border-b border-slate-700 bg-slate-900 px-4">
        <span className="text-lg font-semibold">Chat Aggregator</span>
        <div className="flex items-center gap-3">
          <button
            type="button"
            onClick={handleNewChat}
            className="rounded-md bg-slate-700 px-3 py-1.5 text-sm font-medium text-white hover:bg-slate-600"
          >
            New Chat
          </button>
          <button
            type="button"
            onClick={openNewSite}
            className="rounded-md bg-slate-700 px-3 py-1.5 text-sm font-medium text-white hover:bg-slate-600"
          >
            + New Site
          </button>
          <ExportPanel sessionId={currentSessionId} />
        </div>
      </header>
      <div className="flex flex-1 overflow-hidden">
        <HistoryPanel
          onSelectSession={handleSelectSession}
          currentSessionId={currentSessionId}
          refreshTrigger={historyRefresh}
          collapsed={historyCollapsed}
          onToggleCollapse={() => setHistoryCollapsed((v) => !v)}
          onDeleteSessions={handleDeleteSessions}
          width={historyWidth}
        />
        {!historyCollapsed && (
          <ResizeHandle
            direction="horizontal"
            current={historyWidth}
            min={160}
            max={500}
            onResize={(delta) => setHistoryWidth((w) => Math.max(160, Math.min(500, w + delta)))}
          />
        )}
        <SiteSidebar
          sites={sites}
          selectedSites={selectedSites}
          toggleSite={toggleSite}
          onEditSite={openEditSite}
          width={sidebarWidth}
        />
        <ResizeHandle
          direction="horizontal"
          current={sidebarWidth}
          min={150}
          max={400}
          onResize={(delta) => setSidebarWidth((w) => Math.max(150, Math.min(400, w + delta)))}
        />
        <main className="flex flex-1 flex-col overflow-hidden">
          <div className="flex-1 overflow-auto">
            {turns.length === 0 && messages.length === 0 && (
              <div className="flex h-full items-center justify-center text-slate-500">
                <div className="text-center">
                  <p className="text-lg font-medium">开始新对话</p>
                  <p className="mt-2 text-sm">选择左侧站点，输入问题开始多轮对话</p>
                </div>
              </div>
            )}
            {turns.map((turn) => (
              <div key={turn.turn} className="border-b border-slate-800">
                <div className="bg-slate-900/50 px-4 py-2">
                  <span className="text-xs font-medium text-slate-400">你</span>
                  <p className="mt-0.5 text-sm text-slate-200">{turn.prompt}</p>
                </div>
                <ChatGrid
                  messages={messages.filter((m) => m.turn === turn.turn)}
                  sites={sites}
                  onToggleKeep={handleToggleKeep}
                />
              </div>
            ))}
          </div>
          <ResizeHandle
            direction="vertical"
            current={inputHeight}
            min={60}
            max={400}
            onResize={(delta) => setInputHeight((h) => Math.max(60, Math.min(400, h - delta)))}
          />
          <InputArea onSend={handleSend} disabled={isLoading} height={inputHeight} />
        </main>
      </div>
      <SiteConfigModal
        isOpen={showConfig}
        editingSite={editingSite}
        onClose={closeConfig}
        onSave={handleSaveSite}
      />
    </div>
  )
}
