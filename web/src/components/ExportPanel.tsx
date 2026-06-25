import { useState } from 'react'

interface ExportPanelProps {
  sessionId: string | null
}

export default function ExportPanel({ sessionId }: ExportPanelProps) {
  const [filterKept, setFilterKept] = useState(true)

  if (!sessionId) {
    return null
  }

  const handleExport = (format: string) => {
    const url = `/api/export?session_id=${encodeURIComponent(sessionId)}&format=${format}&filter_kept=${filterKept}`
    window.open(url, '_blank')
  }

  return (
    <div className="flex items-center gap-2">
      <label className="flex items-center gap-1 text-xs text-slate-300">
        <input
          type="checkbox"
          checked={filterKept}
          onChange={(e) => setFilterKept(e.target.checked)}
          className="h-3 w-3"
        />
        仅 keep
      </label>
      <button
        onClick={() => handleExport('json')}
        className="rounded bg-slate-700 px-3 py-1 text-sm text-white hover:bg-slate-600"
      >
        Export JSON
      </button>
      <button
        onClick={() => handleExport('markdown')}
        className="rounded bg-slate-700 px-3 py-1 text-sm text-white hover:bg-slate-600"
      >
        Export Markdown
      </button>
    </div>
  )
}
