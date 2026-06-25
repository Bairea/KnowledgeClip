import type { Message } from '../types'
import KeepSwitch from './KeepSwitch'

interface MessageCardProps {
  message: Message
  siteName: string
  onToggleKeep: (id: string) => void
}

export default function MessageCard({ message, siteName, onToggleKeep }: MessageCardProps) {
  return (
    <div className="flex flex-col rounded-lg border border-slate-700 bg-slate-800 p-4">
      <div className="mb-2 border-b border-slate-700 pb-2 text-sm font-semibold text-slate-200">
        {siteName}
      </div>
      <div className="min-h-[120px] flex-1 overflow-auto whitespace-pre-wrap text-sm text-slate-300">
        {message.error ? (
          <span className="text-red-400">{message.error}</span>
        ) : (
          message.content
        )}
      </div>
      <div className="mt-2 flex items-center justify-between border-t border-slate-700 pt-2">
        <span className="text-xs text-slate-500">
          {message.elapsed_ms > 0 ? `${message.elapsed_ms}ms` : ''}
        </span>
        <KeepSwitch
          checked={message.kept}
          onChange={() => onToggleKeep(message.id)}
        />
      </div>
    </div>
  )
}
