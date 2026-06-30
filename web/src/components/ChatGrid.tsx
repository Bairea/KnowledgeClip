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

  const gridStyle = {
    display: 'grid',
    gridTemplateColumns: `repeat(${count}, minmax(0, 1fr))`,
    gap: '0.75rem',
    padding: '0.75rem',
  }

  return (
    <div style={gridStyle}>
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
