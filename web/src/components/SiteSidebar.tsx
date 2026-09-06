import type { Site } from '../types'
import type { EngineHealth } from '../hooks/useEngineStatus'

interface SiteSidebarProps {
  sites: Site[]
  selectedSites: Set<string>
  toggleSite: (id: string) => void
  onEditSite: (site: Site) => void
  width: number
  engines?: Map<string, EngineHealth>
}

// Engine badge colors: healthy engines green, broken ones red, unknown gray.
function engineDotClass(h?: EngineHealth): string {
  if (!h) return 'bg-[var(--line-strong)]'
  return h.available ? 'bg-[var(--success)]' : 'bg-[var(--danger)]'
}

export default function SiteSidebar({ sites, selectedSites, toggleSite, onEditSite, width, engines }: SiteSidebarProps) {
  return (
    <aside className="flex flex-col border-r border-[var(--line)] bg-[var(--surface)]" style={{ width: `${width}px` }}>
      <div className="flex h-12 items-center gap-2 border-b border-[var(--line)] px-4">
        <span className="font-display text-[14px] font-semibold text-[var(--ink)]">站点</span>
        <span className="font-mono text-[9px] uppercase tracking-[0.12em] text-[var(--ink-faint)]">sources</span>
      </div>
      <div className="flex-1 overflow-auto p-2">
        {sites.map((site) => {
          const disabled = !site.enabled
          const isChecked = selectedSites.has(site.id)
          const health = engines?.get(site.engine_type)
          return (
            <div
              key={site.id}
              className={`flex items-center gap-2 px-2 py-1.5 transition-colors hover:bg-[var(--paper-soft)] ${
                isChecked ? 'border-l-2 border-[var(--accent)]' : 'border-l-2 border-transparent'
              }`}
            >
              <input
                type="checkbox"
                checked={isChecked}
                disabled={disabled}
                onChange={() => toggleSite(site.id)}
                className="h-4 w-4 border-[var(--line-strong)] bg-[var(--paper)] text-[var(--accent)] focus:ring-[var(--accent)] disabled:cursor-not-allowed disabled:opacity-40"
              />
              <button
                type="button"
                onClick={() => onEditSite(site)}
                className={`flex-1 cursor-pointer text-left font-ui text-[13px] hover:text-[var(--ink)] ${
                  disabled ? 'text-[var(--ink-muted)]' : 'text-[var(--ink-soft)]'
                }`}
              >
                {site.name}
              </button>
              <span
                title={`${site.engine_type}${health?.detail ? `：${health.detail}` : ''}`}
                className={`h-1.5 w-1.5 shrink-0 rounded-full ${engineDotClass(health)}`}
              />
              {disabled && (
                <span className="bg-[var(--paper-dark)] px-1.5 py-0.5 font-mono text-[9px] uppercase tracking-[0.08em] text-[var(--ink-muted)]">
                  未配置
                </span>
              )}
            </div>
          )
        })}
      </div>
    </aside>
  )
}
