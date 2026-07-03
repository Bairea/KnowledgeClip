import { useState } from 'react'

interface ExportPanelProps {
  sessionId: string | null
}

export default function ExportPanel({ sessionId }: ExportPanelProps) {
  const [filterKept, setFilterKept] = useState(false)

  if (!sessionId) {
    return null
  }

  const handleExport = (format: string) => {
    const url = `/api/export?session_id=${encodeURIComponent(sessionId)}&format=${format}&filter_kept=${filterKept}`
    window.open(url, '_blank')
  }

  return (
    <div className="flex items-center gap-2">
      <label className="flex items-center gap-1.5 font-ui text-[11px] text-[var(--ink-soft)]">
        <input
          type="checkbox"
          checked={filterKept}
          onChange={(e) => setFilterKept(e.target.checked)}
          className="h-3 w-3 border-[var(--line-strong)] text-[var(--accent)] focus:ring-[var(--accent)]"
        />
        仅导出已保留
      </label>
      <button
        onClick={() => handleExport('json')}
        className="border border-[var(--line)] bg-[var(--surface)] px-3 py-1 font-mono text-[11px] uppercase tracking-[0.06em] text-[var(--ink-soft)] hover:border-[var(--ink-muted)] hover:text-[var(--ink)]"
      >
        JSON
      </button>
      <button
        onClick={() => handleExport('markdown')}
        className="border border-[var(--line)] bg-[var(--surface)] px-3 py-1 font-mono text-[11px] uppercase tracking-[0.06em] text-[var(--ink-soft)] hover:border-[var(--ink-muted)] hover:text-[var(--ink)]"
      >
        Markdown
      </button>
    </div>
  )
}
