import { useState, useEffect } from 'react'

interface SiteFormData {
  id: string
  name: string
  url: string
  engine_type: string
  selectors: string
  format_prompt: string
}

interface SiteConfigModalProps {
  isOpen: boolean
  editingSite: SiteFormData | null
  onClose: () => void
  onSave: (data: SiteFormData) => void
}

function slugify(name: string): string {
  return name
    .toLowerCase()
    .trim()
    .replace(/[^\w\u4e00-\u9fa5]+/g, '-')
    .replace(/^-+|-+$/g, '')
}

const DEFAULT_FORMAT_PROMPT =
  '请使用标准Markdown格式回答，标题从第三层级（###）开始，适当使用表格、代码块、列表等结构化元素。'

const DEFAULT_SELECTORS = JSON.stringify(
  {
    input: '',
    submit: '',
    answer: '',
    wait_for: '',
    copy_button: '',
    content_strategy: 'clipboard',
  },
  null,
  2,
)

function createEmptyForm(): SiteFormData {
  return {
    id: '',
    name: '',
    url: '',
    engine_type: 'cdp',
    selectors: DEFAULT_SELECTORS,
    format_prompt: DEFAULT_FORMAT_PROMPT,
  }
}

export default function SiteConfigModal({ isOpen, editingSite, onClose, onSave }: SiteConfigModalProps) {
  const [formData, setFormData] = useState<SiteFormData>(createEmptyForm())
  const [detecting, setDetecting] = useState(false)
  const [showAdvanced, setShowAdvanced] = useState(false)
  const [autoDetected, setAutoDetected] = useState(false)

  useEffect(() => {
    if (isOpen) {
      setFormData(editingSite ? { ...editingSite } : createEmptyForm())
      setDetecting(false)
      setShowAdvanced(false)
      setAutoDetected(false)
    }
  }, [isOpen, editingSite])

  if (!isOpen) return null

  const isEditing = Boolean(editingSite)

  const handleChange = (field: keyof SiteFormData, value: string) => {
    setFormData((prev) => {
      const next = { ...prev, [field]: value }
      if (field === 'name' && !isEditing) {
        next.id = slugify(value)
      }
      return next
    })
  }

  const handleDetect = async (): Promise<string | null> => {
    if (!formData.url) return null
    setDetecting(true)
    try {
      const res = await fetch(`/api/detect?url=${encodeURIComponent(formData.url)}`)
      if (!res.ok) {
        const err = await res.json()
        console.error('Detect failed:', err.error)
        return null
      }
      const data = await res.json()
      const merged = {
        input: '',
        submit: '',
        answer: '',
        wait_for: '',
        copy_button: '',
        content_strategy: 'clipboard',
        ...data,
      }
      if (merged.answer && !merged.wait_for) {
        merged.wait_for = merged.answer + ':last-child'
      }
      const selectorsStr = JSON.stringify(merged, null, 2)
      setFormData((prev) => ({ ...prev, selectors: selectorsStr }))
      setAutoDetected(true)
      return selectorsStr
    } catch (e) {
      console.error('Detect failed:', e)
      return null
    } finally {
      setDetecting(false)
    }
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()

    let finalSelectors = formData.selectors

    if (!isEditing && !autoDetected) {
      const detected = await handleDetect()
      if (!detected) {
        setShowAdvanced(true)
        alert('自动检测未能完成，请在高级设置中手动配置选择器')
        return
      }
      finalSelectors = detected
    }

    onSave({ ...formData, selectors: finalSelectors })
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-[var(--ink)]/50 backdrop-blur-sm">
      <div className="w-full max-w-lg border border-[var(--line)] bg-[var(--surface-raised)] p-6 shadow-2xl">
        <div className="mb-5 flex items-baseline gap-2 border-b border-[var(--line)] pb-3">
          <h2 className="font-display text-[18px] font-semibold text-[var(--ink)]">
            {isEditing ? '编辑站点' : '新增站点'}
          </h2>
          <span className="font-mono text-[10px] uppercase tracking-[0.12em] text-[var(--ink-faint)]">config</span>
        </div>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div>
            <label className="mb-1 block font-ui text-[12px] font-medium text-[var(--ink-soft)]">名称</label>
            <input
              type="text"
              value={formData.name}
              onChange={(e) => handleChange('name', e.target.value)}
              className="w-full border border-[var(--line)] bg-[var(--paper)] px-3 py-2 font-ui text-[13px] text-[var(--ink)] placeholder-[var(--ink-faint)] focus:border-[var(--accent)] focus:outline-none focus:ring-1 focus:ring-[var(--accent-soft)]"
              placeholder="如：豆包"
              autoFocus
            />
          </div>
          <div>
            <label className="mb-1 block font-ui text-[12px] font-medium text-[var(--ink-soft)]">链接</label>
            <input
              type="text"
              value={formData.url}
              onChange={(e) => handleChange('url', e.target.value)}
              className="w-full border border-[var(--line)] bg-[var(--paper)] px-3 py-2 font-ui text-[13px] text-[var(--ink)] placeholder-[var(--ink-faint)] focus:border-[var(--accent)] focus:outline-none focus:ring-1 focus:ring-[var(--accent-soft)]"
              placeholder="https://www.doubao.com/chat/"
            />
            {detecting && (
              <p className="mt-1.5 font-mono text-[10px] uppercase tracking-[0.08em] text-[var(--accent)]">正在自动检测选择器...</p>
            )}
            {autoDetected && !detecting && (
              <p className="mt-1.5 font-mono text-[10px] uppercase tracking-[0.08em] text-[var(--success)]">选择器已自动检测</p>
            )}
          </div>

          <div>
            <button
              type="button"
              onClick={() => setShowAdvanced((v) => !v)}
              className="font-mono text-[10px] uppercase tracking-[0.1em] text-[var(--ink-muted)] hover:text-[var(--ink)]"
            >
              {showAdvanced ? '▼ 高级设置' : '▶ 高级设置'}
            </button>
          </div>

          {showAdvanced && (
            <>
              <div>
                <label className="mb-1 block font-ui text-[12px] font-medium text-[var(--ink-soft)]">ID</label>
                <input
                  type="text"
                  value={formData.id}
                  disabled={isEditing}
                  onChange={(e) => handleChange('id', e.target.value)}
                  className="w-full border border-[var(--line)] bg-[var(--paper)] px-3 py-2 font-mono text-[12px] text-[var(--ink)] placeholder-[var(--ink-faint)] focus:border-[var(--accent)] focus:outline-none focus:ring-1 focus:ring-[var(--accent-soft)] disabled:opacity-60"
                  placeholder="站点唯一标识（自动生成）"
                />
              </div>
              <div>
                <label className="mb-1 block font-ui text-[12px] font-medium text-[var(--ink-soft)]">引擎类型</label>
                <select
                  value={formData.engine_type}
                  onChange={(e) => handleChange('engine_type', e.target.value)}
                  className="w-full border border-[var(--line)] bg-[var(--paper)] px-3 py-2 font-mono text-[12px] text-[var(--ink)] focus:border-[var(--accent)] focus:outline-none focus:ring-1 focus:ring-[var(--accent-soft)]"
                >
                  <option value="cdp">cdp</option>
                  <option value="browser-act">browser-act</option>
                  <option value="bsk">bsk</option>
                  <option value="playwright">playwright</option>
                </select>
              </div>
              <div>
                <label className="mb-1 block font-ui text-[12px] font-medium text-[var(--ink-soft)]">Selectors (JSON)</label>
                <textarea
                  value={formData.selectors}
                  onChange={(e) => handleChange('selectors', e.target.value)}
                  rows={6}
                  className="w-full border border-[var(--line)] bg-[var(--paper)] px-3 py-2 font-mono text-[12px] leading-[1.5] text-[var(--ink)] placeholder-[var(--ink-faint)] focus:border-[var(--accent)] focus:outline-none focus:ring-1 focus:ring-[var(--accent-soft)]"
                />
                <button
                  type="button"
                  onClick={handleDetect}
                  disabled={detecting || !formData.url}
                  className="mt-1.5 border border-[var(--line)] bg-[var(--paper-dark)] px-3 py-1.5 font-ui text-[11px] font-medium text-[var(--ink-soft)] hover:border-[var(--ink-muted)] hover:text-[var(--ink)] disabled:opacity-60"
                >
                  {detecting ? '检测中...' : '重新检测'}
                </button>
              </div>
              <div>
                <label className="mb-1 block font-ui text-[12px] font-medium text-[var(--ink-soft)]">Format Prompt</label>
                <textarea
                  value={formData.format_prompt}
                  onChange={(e) => handleChange('format_prompt', e.target.value)}
                  rows={3}
                  className="w-full border border-[var(--line)] bg-[var(--paper)] px-3 py-2 font-reading text-[13px] leading-[1.5] text-[var(--ink)] placeholder-[var(--ink-faint)] focus:border-[var(--accent)] focus:outline-none focus:ring-1 focus:ring-[var(--accent-soft)]"
                />
              </div>
            </>
          )}

          <div className="flex justify-end gap-3 border-t border-[var(--line)] pt-4">
            <button
              type="button"
              onClick={onClose}
              className="border border-[var(--line)] bg-[var(--paper)] px-4 py-2 font-ui text-[13px] font-medium text-[var(--ink-soft)] hover:bg-[var(--paper-dark)] hover:text-[var(--ink)]"
            >
              取消
            </button>
            <button
              type="submit"
              disabled={detecting || !formData.name || !formData.url}
              className="bg-[var(--accent)] px-4 py-2 font-ui text-[13px] font-medium text-[var(--accent-ink)] hover:bg-[var(--accent-hover)] disabled:opacity-60"
            >
              {detecting ? '保存中...' : '保存'}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}
