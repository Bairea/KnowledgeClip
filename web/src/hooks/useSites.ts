import { useEffect, useState, useCallback } from 'react'
import type { Site } from '../types'

export function useSites() {
  const [sites, setSites] = useState<Site[]>([])
  const [selectedSites, setSelectedSites] = useState<Set<string>>(new Set())

  const fetchSites = useCallback(() => {
    fetch('/api/sites')
      .then((res) => res.json())
      .then((data: Site[]) => {
        setSites(data || [])
        const selected = (data || []).filter((s) => s.selected).map((s) => s.id)
        setSelectedSites(new Set(selected))
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
      const newSelected = !selectedSites.has(id)
      fetch(`/api/sites/${id}/selected`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ selected: newSelected }),
      })
        .then((res) => {
          if (!res.ok) throw new Error('Failed to update selection')
          setSelectedSites((prev) => {
            const next = new Set(prev)
            if (next.has(id)) {
              next.delete(id)
            } else {
              next.add(id)
            }
            return next
          })
        })
        .catch((err) => {
          console.error('Failed to toggle site selection:', err)
        })
    },
    [sites, selectedSites],
  )

  return { sites, selectedSites, toggleSite, fetchSites }
}
