import { useEffect, useState } from 'react'
import type { Session } from '../types'

interface HistoryPanelProps {
  onSelectSession: (sessionId: string) => void
  currentSessionId: string | null
  refreshTrigger: number
  collapsed: boolean
  onToggleCollapse: () => void
  onDeleteSessions: (sessionIds: string[]) => void
  width: number
}

export default function HistoryPanel({
  onSelectSession,
  currentSessionId,
  refreshTrigger,
  collapsed,
  onToggleCollapse,
  onDeleteSessions,
  width,
}: HistoryPanelProps) {
  const [sessions, setSessions] = useState<Session[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [manageMode, setManageMode] = useState(false)
  const [selected, setSelected] = useState<Set<string>>(new Set())

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
        setSessions(data || [])
      })
      .catch((err) => {
        setError(err instanceof Error ? err.message : String(err))
      })
      .finally(() => {
        setLoading(false)
      })
  }, [refreshTrigger])

  const truncate = (text: string, maxLen: number) => {
    if (text.length <= maxLen) return text
    return text.slice(0, maxLen) + '...'
  }

  const toggleSelect = (id: string) => {
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(id)) {
        next.delete(id)
      } else {
        next.add(id)
      }
      return next
    })
  }

  const selectAll = () => {
    setSelected(new Set(sessions.map((s) => s.id)))
  }

  const selectNone = () => {
    setSelected(new Set())
  }

  const handleDelete = () => {
    if (selected.size === 0) return
    if (window.confirm(`确定删除选中的 ${selected.size} 条历史记录吗？`)) {
      onDeleteSessions(Array.from(selected))
      setSelected(new Set())
      setManageMode(false)
    }
  }

  const exitManage = () => {
    setManageMode(false)
    setSelected(new Set())
  }

  if (collapsed) {
    return (
      <div className="flex w-10 flex-col items-center border-r border-slate-700 bg-slate-900 py-3">
        <button
          type="button"
          onClick={onToggleCollapse}
          aria-label="展开历史记录"
          title="展开历史记录"
          className="flex h-8 w-8 items-center justify-center rounded-md text-slate-400 hover:bg-slate-800 hover:text-white"
        >
          <svg className="h-4 w-4" viewBox="0 0 20 20" fill="currentColor">
            <path
              fillRule="evenodd"
              d="M7.21 14.77a.75.75 0 0 1 .02-1.06L11.168 10 7.23 6.29a.75.75 0 1 1 1.04-1.08l4.5 4.25a.75.75 0 0 1 0 1.08l-4.5 4.25a.75.75 0 0 1-1.06-.02Z"
              clipRule="evenodd"
            />
          </svg>
        </button>
      </div>
    )
  }

  return (
    <div className="flex flex-col border-r border-slate-700 bg-slate-900" style={{ width: `${width}px` }}>
      <div className="flex h-12 items-center justify-between border-b border-slate-700 px-4">
        <span className="text-sm font-semibold text-slate-200">历史记录</span>
        <div className="flex items-center gap-1">
          {!manageMode ? (
            <button
              type="button"
              onClick={() => setManageMode(true)}
              className="rounded px-2 py-0.5 text-xs text-slate-400 hover:bg-slate-800 hover:text-white"
            >
              管理
            </button>
          ) : null}
          <button
            type="button"
            onClick={onToggleCollapse}
            aria-label="收起历史记录"
            title="收起历史记录"
            className="flex h-6 w-6 items-center justify-center rounded-md text-slate-400 hover:bg-slate-800 hover:text-white"
          >
            <svg className="h-4 w-4" viewBox="0 0 20 20" fill="currentColor">
              <path
                fillRule="evenodd"
                d="M12.79 5.23a.75.75 0 0 1-.02 1.06L8.832 10l3.938 3.71a.75.75 0 1 1-1.04 1.08l-4.5-4.25a.75.75 0 0 1 0-1.08l4.5-4.25a.75.75 0 0 1 1.06.02Z"
                clipRule="evenodd"
              />
            </svg>
          </button>
        </div>
      </div>

      {manageMode && (
        <div className="flex items-center justify-between border-b border-slate-700 px-3 py-2">
          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={selected.size === sessions.length ? selectNone : selectAll}
              className="text-xs text-slate-400 hover:text-white"
            >
              {selected.size === sessions.length ? '取消全选' : '全选'}
            </button>
            <span className="text-xs text-slate-500">已选 {selected.size}</span>
          </div>
          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={handleDelete}
              disabled={selected.size === 0}
              className="rounded bg-red-700 px-2 py-1 text-xs font-medium text-white hover:bg-red-600 disabled:cursor-not-allowed disabled:opacity-40"
            >
              删除
            </button>
            <button
              type="button"
              onClick={exitManage}
              className="rounded px-2 py-1 text-xs text-slate-400 hover:text-white"
            >
              取消
            </button>
          </div>
        </div>
      )}

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
        {sessions.map((session) => {
          const isSelected = selected.has(session.id)
          return (
            <div
              key={session.id}
              className={`flex items-center gap-2 rounded-md px-2 py-2 transition-colors ${
                manageMode
                  ? isSelected
                    ? 'bg-slate-700'
                    : 'hover:bg-slate-800'
                  : currentSessionId === session.id
                    ? 'bg-slate-700'
                    : 'hover:bg-slate-800'
              }`}
            >
              {manageMode && (
                <input
                  type="checkbox"
                  checked={isSelected}
                  onChange={() => toggleSelect(session.id)}
                  className="h-4 w-4 shrink-0 rounded border-slate-600 bg-slate-800 text-blue-500"
                />
              )}
              <button
                type="button"
                onClick={() => {
                  if (!manageMode) onSelectSession(session.id)
                }}
                disabled={manageMode}
                className="flex-1 text-left text-sm disabled:cursor-default"
              >
                <div
                  className={`font-medium ${
                    manageMode
                      ? 'text-slate-300'
                      : currentSessionId === session.id
                        ? 'text-white'
                        : 'text-slate-300'
                  }`}
                >
                  {truncate(session.prompt, 40)}
                </div>
                <div className="mt-1 text-xs text-slate-400">
                  {new Date(session.created_at).toLocaleString()}
                </div>
              </button>
            </div>
          )
        })}
      </div>
    </div>
  )
}
