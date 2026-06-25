import { useEffect, useState, useCallback } from 'react'
import type { Site } from '../types'

export function useSites() {
  const [sites, setSites] = useState<Site[]>([])
  const [selectedSites, setSelectedSites] = useState<Set<string>>(new Set())

  useEffect(() => {
    fetch('/api/sites')
      .then((res) => res.json())
      .then((data: Site[]) => {
        setSites(data)
        const enabled = data.filter((s) => s.enabled).map((s) => s.id)
        setSelectedSites(new Set(enabled))
      })
      .catch((err) => {
        console.error('Failed to fetch sites:', err)
      })
  }, [])

  const toggleSite = useCallback((id: string) => {
    setSelectedSites((prev) => {
      const next = new Set(prev)
      if (next.has(id)) {
        next.delete(id)
      } else {
        next.add(id)
      }
      return next
    })
  }, [])

  return { sites, selectedSites, toggleSite }
}
