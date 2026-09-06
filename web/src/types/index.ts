export interface Site {
  id: string
  name: string
  url: string
  engine_type: string
  enabled: boolean
  selectors: string
  format_prompt: string
  selected?: boolean
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
  /** 当前管线阶段（仅 loading 时有意义）：input | sending | generating | extracting */
  stage?: string
  /** 收到最后一次进度事件的本地时间戳（ms），用于实时计时 */
  stageAt?: number
}

export interface Turn {
  turn: number
  prompt: string
  messages: Message[]
}
