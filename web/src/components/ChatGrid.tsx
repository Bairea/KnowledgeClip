import type { Message, Site } from '../types'
import MessageCard from './MessageCard'

interface ChatGridProps {
  messages: Message[]
  sites: Site[]
  onToggleKeep: (id: string) => void
}

export default function ChatGrid({ messages, sites, onToggleKeep }: ChatGridProps) {
  const siteMap = new Map(sites.map((s) => [s.id, s.name]))

  return (
    <div className="flex flex-wrap gap-4 overflow-auto p-4">
      {messages.map((msg) => (
        <div
          key={msg.id}
          className="flex-1 min-w-[280px] max-w-full"
        >
          <MessageCard
            message={msg}
            siteName={siteMap.get(msg.site_id) || msg.site_id}
            onToggleKeep={onToggleKeep}
          />
        </div>
      ))}
    </div>
  )
}
