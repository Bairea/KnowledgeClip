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
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60">
      <div className="w-full max-w-lg rounded-lg border border-slate-700 bg-slate-900 p-6 shadow-xl">
        <h2 className="mb-4 text-lg font-semibold text-white">
          {isEditing ? '编辑站点' : '新增站点'}
        </h2>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div>
            <label className="mb-1 block text-sm font-medium text-slate-300">名称</label>
            <input
              type="text"
              value={formData.name}
              onChange={(e) => handleChange('name', e.target.value)}
              className="w-full rounded-md border border-slate-600 bg-slate-800 px-3 py-2 text-sm text-white placeholder-slate-500 focus:border-slate-500 focus:outline-none"
              placeholder="如：豆包"
              autoFocus
            />
          </div>
          <div>
            <label className="mb-1 block text-sm font-medium text-slate-300">链接</label>
            <input
              type="text"
              value={formData.url}
              onChange={(e) => handleChange('url', e.target.value)}
              className="w-full rounded-md border border-slate-600 bg-slate-800 px-3 py-2 text-sm text-white placeholder-slate-500 focus:border-slate-500 focus:outline-none"
              placeholder="https://www.doubao.com/chat/"
            />
            {detecting && (
              <p className="mt-1 text-xs text-blue-400">正在自动检测选择器...</p>
            )}
            {autoDetected && !detecting && (
              <p className="mt-1 text-xs text-green-400">选择器已自动检测</p>
            )}
          </div>

          <div>
            <button
              type="button"
              onClick={() => setShowAdvanced((v) => !v)}
              className="text-xs text-slate-400 hover:text-slate-200"
            >
              {showAdvanced ? '▼ 高级设置' : '▶ 高级设置'}
            </button>
          </div>

          {showAdvanced && (
            <>
              <div>
                <label className="mb-1 block text-sm font-medium text-slate-300">ID</label>
                <input
                  type="text"
                  value={formData.id}
                  disabled={isEditing}
                  onChange={(e) => handleChange('id', e.target.value)}
                  className="w-full rounded-md border border-slate-600 bg-slate-800 px-3 py-2 text-sm text-white placeholder-slate-500 focus:border-slate-500 focus:outline-none disabled:opacity-60"
                  placeholder="站点唯一标识（自动生成）"
                />
              </div>
              <div>
                <label className="mb-1 block text-sm font-medium text-slate-300">引擎类型</label>
                <select
                  value={formData.engine_type}
                  onChange={(e) => handleChange('engine_type', e.target.value)}
                  className="w-full rounded-md border border-slate-600 bg-slate-800 px-3 py-2 text-sm text-white focus:border-slate-500 focus:outline-none"
                >
                  <option value="cdp">cdp</option>
                  <option value="playwright">playwright</option>
                </select>
              </div>
              <div>
                <label className="mb-1 block text-sm font-medium text-slate-300">Selectors (JSON)</label>
                <textarea
                  value={formData.selectors}
                  onChange={(e) => handleChange('selectors', e.target.value)}
                  rows={6}
                  className="w-full rounded-md border border-slate-600 bg-slate-800 px-3 py-2 text-sm text-white placeholder-slate-500 focus:border-slate-500 focus:outline-none"
                />
                <button
                  type="button"
                  onClick={handleDetect}
                  disabled={detecting || !formData.url}
                  className="mt-1 rounded-md bg-slate-700 px-3 py-1.5 text-xs font-medium text-white hover:bg-slate-600 disabled:opacity-60"
                >
                  {detecting ? '检测中...' : '重新检测'}
                </button>
              </div>
              <div>
                <label className="mb-1 block text-sm font-medium text-slate-300">Format Prompt</label>
                <textarea
                  value={formData.format_prompt}
                  onChange={(e) => handleChange('format_prompt', e.target.value)}
                  rows={3}
                  className="w-full rounded-md border border-slate-600 bg-slate-800 px-3 py-2 text-sm text-white placeholder-slate-500 focus:border-slate-500 focus:outline-none"
                />
              </div>
            </>
          )}

          <div className="flex justify-end gap-3 pt-2">
            <button
              type="button"
              onClick={onClose}
              className="rounded-md border border-slate-600 bg-slate-800 px-4 py-2 text-sm font-medium text-slate-200 hover:bg-slate-700"
            >
              取消
            </button>
            <button
              type="submit"
              disabled={detecting || !formData.name || !formData.url}
              className="rounded-md bg-slate-600 px-4 py-2 text-sm font-medium text-white hover:bg-slate-500 disabled:opacity-60"
            >
              {detecting ? '保存中...' : '保存'}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}
