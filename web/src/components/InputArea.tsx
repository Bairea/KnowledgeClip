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
      className="flex items-end gap-2 border-t border-[var(--line)] bg-[var(--surface)] p-4"
      style={{ height: `${height}px` }}
    >
      <textarea
        value={prompt}
        onChange={(e) => setPrompt(e.target.value)}
        onKeyDown={handleKeyDown}
        placeholder="输入消息... (Enter 发送, Shift+Enter 换行)"
        disabled={disabled}
        className="flex-1 resize-none border border-[var(--line)] bg-[var(--paper)] px-4 py-2.5 font-reading text-[15.5px] leading-[1.6] text-[var(--ink)] placeholder-[var(--ink-faint)] outline-none focus:border-[var(--accent)] focus:ring-1 focus:ring-[var(--accent-soft)] disabled:opacity-50"
        style={{ height: '100%' }}
      />
      <button
        type="submit"
        disabled={disabled}
        className="h-11 shrink-0 bg-[var(--accent)] px-5 font-ui text-[13px] font-medium text-[var(--accent-ink)] hover:bg-[var(--accent-hover)] disabled:opacity-50"
      >
        发送
      </button>
    </form>
  )
}
