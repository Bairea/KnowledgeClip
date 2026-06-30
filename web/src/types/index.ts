export interface Site {
  id: string
  name: string
  url: string
  engine_type: string
  enabled: boolean
  selectors: string
  format_prompt: string
}

export interface Session {
  id: string
  prompt: string
  created_at: string
}

export interface Message {
  id: string
  message_id?: string
  session_id: string
  site_id: string
  content: string
  kept: boolean
  error: string
  elapsed_ms: number
  created_at: string
  loading?: boolean
  turn?: number
}

export interface Turn {
  turn: number
  prompt: string
  messages: Message[]
}
