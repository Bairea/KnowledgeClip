import { useEffect, useState } from 'react'
import type { Session } from '../types'

interface HistoryPanelProps {
  onSelectSession: (sessionId: string) => void
  currentSessionId: string | null
}

export default function HistoryPanel({ onSelectSession, currentSessionId }: HistoryPanelProps) {
  const [sessions, setSessions] = useState<Session[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    setLoading(true)
    fetch('/api/sessions')
      .then((res) => {
        if (!res.ok) {
          throw new Error(`HTTP ${res.status}`)
        }
        return res.json()
      })
      .then((data: Session[]) => {
        setSessions(data)
      })
      .catch((err) => {
        setError(err instanceof Error ? err.message : String(err))
      })
      .finally(() => {
        setLoading(false)
      })
  }, [])

  const truncate = (text: string, maxLen: number) => {
    if (text.length <= maxLen) return text
    return text.slice(0, maxLen) + '...'
  }

  return (
    <div className="flex w-64 flex-col border-r border-slate-700 bg-slate-900">
      <div className="border-b border-slate-700 px-4 py-3 text-sm font-semibold text-slate-200">
        历史记录
      </div>
      <div className="flex-1 overflow-auto p-2">
        {loading && (
          <div className="px-2 py-4 text-center text-sm text-slate-400">加载中...</div>
        )}
        {error && (
          <div className="px-2 py-4 text-center text-sm text-red-400">{error}</div>
        )}
        {!loading && !error && sessions.length === 0 && (
          <div className="px-2 py-4 text-center text-sm text-slate-400">暂无记录</div>
        )}
        {sessions.map((session) => (
          <button
            key={session.id}
            type="button"
            onClick={() => onSelectSession(session.id)}
            className={`w-full rounded-md px-2 py-2 text-left text-sm transition-colors ${
              currentSessionId === session.id
                ? 'bg-slate-700 text-white'
                : 'text-slate-300 hover:bg-slate-800 hover:text-white'
            }`}
          >
            <div className="font-medium">{truncate(session.prompt, 40)}</div>
            <div className="mt-1 text-xs text-slate-400">
              {new Date(session.created_at).toLocaleString()}
            </div>
          </button>
        ))}
      </div>
    </div>
  )
}
