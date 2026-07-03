interface KeepSwitchProps {
  checked: boolean
  onChange: () => void
}

export default function KeepSwitch({ checked, onChange }: KeepSwitchProps) {
  return (
    <button
      type="button"
      onClick={onChange}
      aria-pressed={checked}
      title={checked ? '已保留' : '保留'}
      className={`relative inline-flex h-5 w-9 items-center border transition-colors ${
        checked
          ? 'border-[var(--accent)] bg-[var(--accent)]'
          : 'border-[var(--line-strong)] bg-[var(--paper)]'
      }`}
    >
      <span
        className={`inline-block h-3.5 w-3.5 transform transition-transform ${
          checked
            ? 'translate-x-[18px] bg-[var(--accent-ink)]'
            : 'translate-x-[2px] bg-[var(--ink-faint)]'
        }`}
      />
    </button>
  )
}
