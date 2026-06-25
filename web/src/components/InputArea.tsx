import { useState, FormEvent } from 'react'

interface InputAreaProps {
  onSend: (prompt: string) => void
}

export default function InputArea({ onSend }: InputAreaProps) {
  const [prompt, setPrompt] = useState('')

  const handleSubmit = (e: FormEvent) => {
    e.preventDefault()
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
        className="flex-1 rounded-md border border-slate-600 bg-slate-800 px-4 py-2 text-sm text-white placeholder-slate-500 outline-none focus:border-slate-400"
      />
      <button
        type="submit"
        className="rounded-md bg-slate-700 px-4 py-2 text-sm font-medium text-white hover:bg-slate-600"
      >
        发送
      </button>
    </form>
  )
}
