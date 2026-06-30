import { useState, FormEvent } from 'react'

interface InputAreaProps {
  onSend: (prompt: string) => void
  disabled?: boolean
  height: number
}

export default function InputArea({ onSend, disabled, height }: InputAreaProps) {
  const [prompt, setPrompt] = useState('')

  const handleSubmit = (e: FormEvent) => {
    e.preventDefault()
    if (disabled) return
    const trimmed = prompt.trim()
    if (!trimmed) return
    onSend(trimmed)
    setPrompt('')
  }

  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      handleSubmit(e as unknown as FormEvent)
    }
  }

  return (
    <form
      onSubmit={handleSubmit}
      className="flex items-end gap-2 border-t border-slate-700 bg-slate-900 p-4"
      style={{ height: `${height}px` }}
    >
      <textarea
        value={prompt}
        onChange={(e) => setPrompt(e.target.value)}
        onKeyDown={handleKeyDown}
        placeholder="输入消息... (Enter 发送, Shift+Enter 换行)"
        disabled={disabled}
        className="flex-1 resize-none rounded-md border border-slate-600 bg-slate-800 px-4 py-2.5 text-sm text-white placeholder-slate-500 outline-none focus:border-slate-400 disabled:opacity-50"
        style={{ height: '100%' }}
      />
      <button
        type="submit"
        disabled={disabled}
        className="h-11 shrink-0 rounded-md bg-slate-700 px-5 text-sm font-medium text-white hover:bg-slate-600 disabled:opacity-50"
      >
        发送
      </button>
    </form>
  )
}
