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

const ENGINE_OPTIONS = ['cdp', 'playwright', 'ts-playwright']

function createEmptyForm(): SiteFormData {
  return {
    id: '',
    name: '',
    url: '',
    engine_type: 'cdp',
    selectors: '',
    format_prompt: '',
  }
}

export default function SiteConfigModal({ isOpen, editingSite, onClose, onSave }: SiteConfigModalProps) {
  const [formData, setFormData] = useState<SiteFormData>(createEmptyForm())
  const [detecting, setDetecting] = useState(false)

  useEffect(() => {
    if (isOpen) {
      setFormData(editingSite ? { ...editingSite } : createEmptyForm())
      setDetecting(false)
    }
  }, [isOpen, editingSite])

  if (!isOpen) return null

  const isEditing = Boolean(editingSite)

  const handleChange = (field: keyof SiteFormData, value: string) => {
    setFormData((prev) => ({ ...prev, [field]: value }))
  }

  const handleDetect = async () => {
    if (!formData.url) {
      alert('请先填写 URL')
      return
    }
    setDetecting(true)
    try {
      const res = await fetch(`/api/detect?url=${encodeURIComponent(formData.url)}`)
      if (!res.ok) {
        const err = await res.json()
        alert(`检测失败: ${err.error || res.statusText}`)
        return
      }
      const data = await res.json()
      handleChange('selectors', JSON.stringify(data, null, 2))
    } catch (e) {
      alert(`检测失败: ${e}`)
    } finally {
      setDetecting(false)
    }
  }

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    onSave(formData)
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60">
      <div className="w-full max-w-lg rounded-lg border border-slate-700 bg-slate-900 p-6 shadow-xl">
        <h2 className="mb-4 text-lg font-semibold text-white">
          {isEditing ? '编辑站点' : '新增站点'}
        </h2>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div>
            <label className="mb-1 block text-sm font-medium text-slate-300">ID</label>
            <input
              type="text"
              value={formData.id}
              disabled={isEditing}
              onChange={(e) => handleChange('id', e.target.value)}
              className="w-full rounded-md border border-slate-600 bg-slate-800 px-3 py-2 text-sm text-white placeholder-slate-500 focus:border-slate-500 focus:outline-none disabled:opacity-60"
              placeholder="站点唯一标识"
            />
          </div>
          <div>
            <label className="mb-1 block text-sm font-medium text-slate-300">名称</label>
            <input
              type="text"
              value={formData.name}
              onChange={(e) => handleChange('name', e.target.value)}
              className="w-full rounded-md border border-slate-600 bg-slate-800 px-3 py-2 text-sm text-white placeholder-slate-500 focus:border-slate-500 focus:outline-none"
              placeholder="站点显示名称"
            />
          </div>
          <div>
            <label className="mb-1 block text-sm font-medium text-slate-300">URL</label>
            <div className="flex gap-2">
              <input
                type="text"
                value={formData.url}
                onChange={(e) => handleChange('url', e.target.value)}
                className="flex-1 rounded-md border border-slate-600 bg-slate-800 px-3 py-2 text-sm text-white placeholder-slate-500 focus:border-slate-500 focus:outline-none"
                placeholder="https://example.com"
              />
              <button
                type="button"
                onClick={handleDetect}
                disabled={detecting}
                className="rounded-md bg-slate-700 px-3 py-2 text-sm font-medium text-white hover:bg-slate-600 disabled:opacity-60"
              >
                {detecting ? '检测中...' : 'Auto Detect'}
              </button>
            </div>
          </div>
          <div>
            <label className="mb-1 block text-sm font-medium text-slate-300">引擎类型</label>
            <select
              value={formData.engine_type}
              onChange={(e) => handleChange('engine_type', e.target.value)}
              className="w-full rounded-md border border-slate-600 bg-slate-800 px-3 py-2 text-sm text-white focus:border-slate-500 focus:outline-none"
            >
              {ENGINE_OPTIONS.map((opt) => (
                <option key={opt} value={opt}>
                  {opt}
                </option>
              ))}
            </select>
          </div>
          <div>
            <label className="mb-1 block text-sm font-medium text-slate-300">Selectors (JSON)</label>
            <textarea
              value={formData.selectors}
              onChange={(e) => handleChange('selectors', e.target.value)}
              rows={4}
              className="w-full rounded-md border border-slate-600 bg-slate-800 px-3 py-2 text-sm text-white placeholder-slate-500 focus:border-slate-500 focus:outline-none"
              placeholder='{"title": "h1", "content": "article"}'
            />
          </div>
          <div>
            <label className="mb-1 block text-sm font-medium text-slate-300">Format Prompt</label>
            <textarea
              value={formData.format_prompt}
              onChange={(e) => handleChange('format_prompt', e.target.value)}
              rows={4}
              className="w-full rounded-md border border-slate-600 bg-slate-800 px-3 py-2 text-sm text-white placeholder-slate-500 focus:border-slate-500 focus:outline-none"
              placeholder="格式化提示词"
            />
          </div>
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
              className="rounded-md bg-slate-600 px-4 py-2 text-sm font-medium text-white hover:bg-slate-500"
            >
              保存
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}
