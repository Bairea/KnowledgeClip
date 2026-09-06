import { useEffect, useState, useCallback } from 'react'

export interface EngineHealth {
  name: string
  available: boolean
  detail?: string
}

/**
 * Polls /api/engine/status so the UI can show engine availability before
 * the user hits send. Refreshes every 60s and on window focus.
 */
export function useEngineStatus() {
  const [engines, setEngines] = useState<Map<string, EngineHealth>>(new Map())

  const refresh = useCallback(() => {
    fetch('/api/engine/status')
      .then((res) => (res.ok ? res.json() : Promise.reject(new Error(`HTTP ${res.status}`))))
      .then((data: { engines?: EngineHealth[] }) => {
        const map = new Map<string, EngineHealth>()
        for (const e of data.engines || []) {
          map.set(e.name, e)
        }
        setEngines(map)
      })
      .catch(() => {
        // Keep the last known state; the poll will retry.
      })
  }, [])

  useEffect(() => {
    refresh()
    const timer = setInterval(refresh, 60_000)
    const onFocus = () => refresh()
    window.addEventListener('focus', onFocus)
    return () => {
      clearInterval(timer)
      window.removeEventListener('focus', onFocus)
    }
  }, [refresh])

  return { engines, refresh }
}
