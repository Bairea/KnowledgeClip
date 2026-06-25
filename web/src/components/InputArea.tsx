import { useState, FormEvent } from 'react'

interface InputAreaProps {
  onSend: (prompt: string) => void
  disabled?: boolean
}

export default function InputArea({ onSend, disabled }: InputAreaProps) {
  const [prompt, setPrompt] = useState('')

  const handleSubmit = (e: FormEvent) => {
    e.preventDefault()
    if (disabled) return
    const trimmed = prompt.trim()
    if (!trimmed) return
    onSend(trimmed)
    setPrompt('')
  }

  return (
    <form
      onSubmit={handleSubmit}
      className="flex items-center gap-2 border-t border-slate-700 bg-slate-900 p-4"
    >
      <input
        type="text"
        value={prompt}
        onChange={(e) => setPrompt(e.target.value)}
        placeholder="输入消息..."
        disabled={disabled}
        className="flex-1 rounded-md border border-slate-600 bg-slate-800 px-4 py-2 text-sm text-white placeholder-slate-500 outline-none focus:border-slate-400 disabled:opacity-50"
      />
      <button
        type="submit"
        disabled={disabled}
        className="rounded-md bg-slate-700 px-4 py-2 text-sm font-medium text-white hover:bg-slate-600 disabled:opacity-50"
      >
        发送
      </button>
    </form>
  )
}
