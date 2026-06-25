import type { Site } from '../types'

interface SiteSidebarProps {
  sites: Site[]
  selectedSites: Set<string>
  toggleSite: (id: string) => void
  onEditSite: (site: Site) => void
}

export default function SiteSidebar({ sites, selectedSites, toggleSite, onEditSite }: SiteSidebarProps) {
  return (
    <aside className="flex w-56 flex-col border-r border-slate-700 bg-slate-900">
      <div className="border-b border-slate-700 px-4 py-3 text-sm font-semibold text-slate-200">
        站点
      </div>
      <div className="flex-1 overflow-auto p-2">
        {sites.map((site) => (
          <div
            key={site.id}
            className="flex items-center gap-2 rounded-md px-2 py-1.5 hover:bg-slate-800"
          >
            <input
              type="checkbox"
              checked={selectedSites.has(site.id)}
              onChange={() => toggleSite(site.id)}
              className="h-4 w-4 rounded border-slate-600 bg-slate-800 text-slate-500"
            />
            <button
              type="button"
              onClick={() => onEditSite(site)}
              className="text-sm text-slate-300 hover:text-white"
            >
              {site.name}
            </button>
          </div>
        ))}
      </div>
    </aside>
  )
}
