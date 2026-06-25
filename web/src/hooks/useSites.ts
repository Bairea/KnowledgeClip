import { useEffect, useState, useCallback } from 'react'
import type { Site } from '../types'

export function useSites() {
  const [sites, setSites] = useState<Site[]>([])
  const [selectedSites, setSelectedSites] = useState<Set<string>>(new Set())

  const fetchSites = useCallback(() => {
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

  useEffect(() => {
    fetchSites()
  }, [fetchSites])

  const toggleSite = useCallback(
    (id: string) => {
      const site = sites.find((s) => s.id === id)
      if (!site || !site.enabled) return
      setSelectedSites((prev) => {
        const next = new Set(prev)
        if (next.has(id)) {
          next.delete(id)
        } else {
          next.add(id)
        }
        return next
      })
    },
    [sites],
  )

  return { sites, selectedSites, toggleSite, fetchSites }
}
