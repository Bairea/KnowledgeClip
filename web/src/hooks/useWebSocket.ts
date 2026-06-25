import { useEffect, useRef } from 'react'

interface WSMessage {
  type: string
  session_id: string
  message_id?: string
  site_id?: string
  content?: string
  error?: string
  elapsed_ms?: number
  done: boolean
}

export function useWebSocket(onMessage: (msg: WSMessage) => void) {
  const wsRef = useRef<WebSocket | null>(null)
  const onMessageRef = useRef(onMessage)

  onMessageRef.current = onMessage

  useEffect(() => {
    let attempt = 0
    let timer: ReturnType<typeof setTimeout> | null = null
    let closedByUnmount = false

    const connect = () => {
      const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
      const ws = new WebSocket(`${protocol}//${window.location.host}/ws`)

      ws.onopen = () => {
        attempt = 0
      }

      ws.onmessage = (event) => {
        try {
          const data = JSON.parse(event.data) as WSMessage
          onMessageRef.current(data)
        } catch {
          // ignore invalid JSON
        }
      }

      ws.onclose = () => {
        wsRef.current = null
        if (closedByUnmount) return
        const delay = Math.min(30000, 1000 * Math.pow(2, attempt))
        attempt++
        timer = setTimeout(connect, delay)
      }

      ws.onerror = () => {
        ws.close()
      }

      wsRef.current = ws
    }

    connect()

    return () => {
      closedByUnmount = true
      if (timer) clearTimeout(timer)
      if (wsRef.current) {
        wsRef.current.close()
      }
    }
  }, [])
}
