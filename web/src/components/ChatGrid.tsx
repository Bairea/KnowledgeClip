import type { Message, Site } from '../types'
import MessageCard from './MessageCard'

interface ChatGridProps {
  messages: Message[]
  sites: Site[]
  onToggleKeep: (id: string) => void
  onRetry: (message: Message) => void
  columns?: number
}

export default function ChatGrid({ messages, sites, onToggleKeep, onRetry, columns = 3 }: ChatGridProps) {
  const siteMap = new Map(sites.map((s) => [s.id, s.name]))

  const gridStyle = {
    display: 'grid',
    gridTemplateColumns: `repeat(${columns}, minmax(0, 1fr))`,
    gap: '1.5rem',
    padding: '1.5rem',
  }

  return (
    <div style={gridStyle}>
      {messages.map((msg) => (
        <MessageCard
          key={msg.id}
          message={msg}
          siteName={siteMap.get(msg.site_id) || msg.site_id}
          onToggleKeep={onToggleKeep}
          onRetry={onRetry}
        />
      ))}
    </div>
  )
}
