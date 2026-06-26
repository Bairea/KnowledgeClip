import type { Message, Site } from '../types'
import MessageCard from './MessageCard'

interface ChatGridProps {
  messages: Message[]
  sites: Site[]
  onToggleKeep: (id: string) => void
}

export default function ChatGrid({ messages, sites, onToggleKeep }: ChatGridProps) {
  const siteMap = new Map(sites.map((s) => [s.id, s.name]))
  const count = messages.length

  const cols = count <= 1 ? 'grid-cols-1' : count === 2 ? 'grid-cols-1 lg:grid-cols-2' : 'grid-cols-1 lg:grid-cols-3'

  return (
    <div className={`grid ${cols} gap-3 p-3`}>
      {messages.map((msg) => (
        <MessageCard
          key={msg.id}
          message={msg}
          siteName={siteMap.get(msg.site_id) || msg.site_id}
          onToggleKeep={onToggleKeep}
        />
      ))}
    </div>
  )
}
