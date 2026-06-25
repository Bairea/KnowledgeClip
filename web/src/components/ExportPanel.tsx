interface ExportPanelProps {
  sessionId: string | null
}

export default function ExportPanel({ sessionId }: ExportPanelProps) {
  if (!sessionId) {
    return null
  }

  const handleExport = (format: string) => {
    const url = `/api/export?session_id=${encodeURIComponent(sessionId)}&format=${format}`
    window.open(url, '_blank')
  }

  return (
    <div className="flex items-center gap-2">
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
