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
      <div className="flex w-10 flex-col items-center border-r border-[var(--line)] bg-[var(--surface)] py-3">
        <button
          type="button"
          onClick={onToggleCollapse}
          aria-label="展开历史记录"
          title="展开历史记录"
          className="flex h-8 w-8 items-center justify-center text-[var(--ink-muted)] hover:bg-[var(--paper-dark)] hover:text-[var(--ink-soft)]"
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
    <div className="flex flex-col border-r border-[var(--line)] bg-[var(--surface)]" style={{ width: `${width}px` }}>
      <div className="flex h-12 items-center justify-between border-b border-[var(--line)] px-4">
        <div className="flex items-baseline gap-2">
          <span className="font-display text-[14px] font-semibold text-[var(--ink)]">历史记录</span>
          <span className="font-mono text-[9px] uppercase tracking-[0.12em] text-[var(--ink-faint)]">archive</span>
        </div>
        <div className="flex items-center gap-1">
          {!manageMode ? (
            <button
            type="button"
            onClick={() => setManageMode(true)}
            className="px-2 py-0.5 font-ui text-[11px] text-[var(--ink-muted)] hover:bg-[var(--paper-soft)] hover:text-[var(--ink-soft)]"
          >
            管理
          </button>
        ) : null}
        <button
          type="button"
          onClick={onToggleCollapse}
          aria-label="收起历史记录"
          title="收起历史记录"
          className="flex h-6 w-6 items-center justify-center text-[var(--ink-muted)] hover:bg-[var(--paper-soft)] hover:text-[var(--ink-soft)]"
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
        <div className="flex items-center justify-between border-b border-[var(--line)] px-3 py-2">
          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={selected.size === sessions.length ? selectNone : selectAll}
              className="font-ui text-[11px] text-[var(--ink-muted)] hover:text-[var(--ink)]"
            >
              {selected.size === sessions.length ? '取消全选' : '全选'}
            </button>
            <span className="font-mono tabular text-[10px] uppercase tracking-[0.08em] text-[var(--ink-faint)]">已选 {selected.size}</span>
          </div>
          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={handleDelete}
              disabled={selected.size === 0}
              className="bg-[var(--danger)] px-2 py-1 font-ui text-[11px] font-medium text-[var(--accent-ink)] hover:opacity-80 disabled:cursor-not-allowed disabled:opacity-40"
            >
              删除
            </button>
            <button
              type="button"
              onClick={exitManage}
              className="px-2 py-1 font-ui text-[11px] text-[var(--ink-muted)] hover:text-[var(--ink)]"
            >
              取消
            </button>
          </div>
        </div>
      )}

      <div className="flex-1 overflow-auto p-2">
        {loading && (
          <div className="px-2 py-4 text-center font-ui text-[12px] text-[var(--ink-muted)]">加载中...</div>
        )}
        {error && (
          <div className="px-2 py-4 text-center font-ui text-[12px] text-[var(--danger)]">{error}</div>
        )}
        {!loading && !error && sessions.length === 0 && (
          <div className="px-2 py-4 text-center font-ui text-[12px] text-[var(--ink-muted)]">暂无记录</div>
        )}
        {sessions.map((session) => {
          const isSelected = selected.has(session.id)
          const isCurrent = currentSessionId === session.id
          return (
            <div
              key={session.id}
              className={`flex items-center gap-2 px-2 py-2 transition-colors ${
                manageMode
                  ? isSelected
                    ? 'bg-[var(--accent-soft)]'
                    : 'hover:bg-[var(--paper-soft)]'
                  : isCurrent
                    ? 'border-l-2 border-[var(--accent)] bg-[var(--paper-soft)]'
                    : 'border-l-2 border-transparent hover:bg-[var(--paper-soft)]'
              }`}
            >
              {manageMode && (
                <input
                  type="checkbox"
                  checked={isSelected}
                  onChange={() => toggleSelect(session.id)}
                  className="h-4 w-4 shrink-0 border-[var(--line-strong)] bg-[var(--paper)] text-[var(--accent)] focus:ring-[var(--accent)]"
                />
              )}
              <button
                type="button"
                onClick={() => {
                  if (!manageMode) onSelectSession(session.id)
                }}
                disabled={manageMode}
                className="flex-1 text-left disabled:cursor-default"
              >
                <div
                  className={`font-reading text-[13px] leading-[1.4] ${
                    manageMode
                      ? 'text-[var(--ink-soft)]'
                      : isCurrent
                        ? 'text-[var(--ink)] font-medium'
                        : 'text-[var(--ink-soft)]'
                  }`}
                >
                  {truncate(session.prompt, 40)}
                </div>
                <div className="mt-1 font-mono tabular text-[10px] uppercase tracking-[0.06em] text-[var(--ink-faint)]">
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
