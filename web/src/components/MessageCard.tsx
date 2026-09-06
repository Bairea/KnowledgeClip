import { ReactNode, useEffect, useReducer, useState } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { Prism as SyntaxHighlighter } from 'react-syntax-highlighter'
import { oneDark } from 'react-syntax-highlighter/dist/esm/styles/prism'
import type { Message } from '../types'
import KeepSwitch from './KeepSwitch'

interface MessageCardProps {
  message: Message
  siteName: string
  onToggleKeep: (id: string) => void
  onRetry?: (message: Message) => void
}

const STAGE_ORDER = ['input', 'sending', 'generating', 'extracting'] as const

const STAGE_LABELS: Record<string, string> = {
  input: '连接站点',
  sending: '发送提问',
  generating: '生成回答中',
  extracting: '提取回答',
}

/** 四段管线步进条：已完成阶段实心，当前阶段呼吸，未到阶段空心。 */
function StageDots({ stage }: { stage?: string }) {
  const current = STAGE_ORDER.indexOf((stage || 'input') as (typeof STAGE_ORDER)[number])
  return (
    <span className="flex items-center gap-1" title={STAGE_LABELS[stage || 'input']}>
      {STAGE_ORDER.map((s, i) => (
        <span
          key={s}
          className={`h-1 w-4 transition-colors duration-300 ${
            i < current
              ? 'bg-[var(--accent-soft)]'
              : i === current
                ? 'animate-pulse bg-[var(--accent)]'
                : 'bg-[var(--line)]'
          }`}
        />
      ))}
    </span>
  )
}

/** 实时秒表：加载期间每秒刷新。 */
function ElapsedTicker({ since }: { since: number }) {
  const [, tick] = useReducer((x: number) => x + 1, 0)
  useEffect(() => {
    const t = setInterval(tick, 1000)
    return () => clearInterval(t)
  }, [])
  const secs = Math.max(0, Math.round((Date.now() - since) / 1000))
  return (
    <span className="font-mono text-[10px] tabular-nums tracking-[0.1em] text-[var(--ink-muted)]">
      {secs}s
    </span>
  )
}

function extractCodeInfo(children: ReactNode): { language: string; code: string } {
  const child = Array.isArray(children) ? children[0] : children
  if (child && typeof child === 'object' && 'props' in child) {
    const childEl = child as { props: { className?: string; children?: ReactNode } }
    const className = childEl.props.className || ''
    const match = /language-(\w+)/.exec(className)
    const language = match ? match[1] : 'text'
    const raw = childEl.props.children
    const code = typeof raw === 'string' ? raw.replace(/\n$/, '') : String(raw ?? '')
    return { language, code }
  }
  return { language: 'text', code: String(children ?? '') }
}

export default function MessageCard({ message, siteName, onToggleKeep, onRetry }: MessageCardProps) {
  const stage = message.loading ? message.stage || 'input' : ''
  const tickerSince = message.stageAt ?? (Date.parse(message.created_at) || Date.now())
  const [copied, setCopied] = useState(false)

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(message.content)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch {
      // Clipboard API unavailable (non-secure context); best effort only.
    }
  }
  return (
    <article className="relative flex flex-col overflow-hidden border border-[var(--line)] bg-[var(--surface-raised)] transition-colors hover:border-[var(--line-strong)]">
      {message.loading && (
        <div className="absolute inset-x-0 top-0 h-0.5 overflow-hidden">
          <div className="h-full w-1/3 animate-[slide_1.6s_ease-in-out_infinite] bg-[var(--accent)]" />
        </div>
      )}
      <header className="flex items-center justify-between border-b border-[var(--line-soft)] bg-[var(--paper-soft)] px-4 py-2">
        <div className="flex items-baseline gap-2">
          <span className="font-mono text-[10px] uppercase tracking-[0.12em] text-[var(--ink-faint)]">№</span>
          <span className="font-ui text-[13px] font-semibold text-[var(--ink)]">{siteName}</span>
        </div>
        {message.loading ? (
          <div className="flex items-center gap-2.5">
            <StageDots stage={stage} />
            <span className="font-ui text-[11px] text-[var(--ink-muted)]">
              {STAGE_LABELS[stage]}
            </span>
            <ElapsedTicker since={tickerSince} />
          </div>
        ) : null}
      </header>
      <div className="max-h-[440px] min-h-[120px] flex-1 overflow-y-auto px-6 py-5 scroll-smooth scrollbar-thin">
        {message.error ? (
          <p className="font-ui text-sm leading-relaxed text-[var(--danger)]">{message.error}</p>
        ) : message.loading && !message.content ? (
          <div className="flex h-32 flex-col items-center justify-center gap-3">
            <StageDots stage={stage} />
            <span className="font-mono text-[11px] uppercase tracking-[0.15em] text-[var(--ink-faint)] animate-pulse">
              {STAGE_LABELS[stage]}
            </span>
          </div>
        ) : (
          <div className="font-reading prose prose-stone prose-sm max-w-none
            prose-headings:font-display
            prose-headings:scroll-mt-20
            prose-h1:text-[18px] prose-h1:font-semibold prose-h1:text-[var(--ink)] prose-h1:tracking-[-0.01em]
            prose-h2:text-[16px] prose-h2:font-semibold prose-h2:text-[var(--ink)] prose-h2:tracking-[-0.005em]
            prose-h3:text-[15px] prose-h3:font-semibold prose-h3:text-[var(--ink)]
            prose-h4:text-[14px] prose-h4:font-semibold prose-h4:text-[var(--ink-soft)]
            prose-p:text-[15.5px] prose-p:leading-[1.75] prose-p:text-[var(--ink-soft)]
            prose-p:font-reading
            prose-li:text-[15px] prose-li:leading-[1.7] prose-li:text-[var(--ink-soft)]
            prose-strong:text-[var(--ink)] prose-strong:font-semibold
            prose-em:text-[var(--ink-soft)]
            prose-code:text-[var(--accent)] prose-code:font-mono prose-code:font-medium prose-code:before:content-none prose-code:after:content-none
            prose-pre:bg-transparent prose-pre:border-0 prose-pre:p-0 prose-pre:my-0
            prose-blockquote:border-l-[var(--accent)] prose-blockquote:bg-[var(--paper-soft)] prose-blockquote:py-1 prose-blockquote:pr-4 prose-blockquote:pl-4 prose-blockquote:text-[var(--ink-muted)] prose-blockquote:font-reading prose-blockquote:italic
            prose-a:text-[var(--accent)] prose-a:no-underline prose-a:font-medium hover:prose-a:underline
            prose-table:text-sm
            prose-th:bg-[var(--paper-dark)] prose-th:text-[var(--ink)] prose-th:font-semibold prose-th:font-ui
            prose-td:text-[var(--ink-soft)] prose-td:font-reading
            prose-hr:border-[var(--line)]
          ">
            <ReactMarkdown
              remarkPlugins={[remarkGfm]}
              components={{
                h1: ({ node: _n, ...props }) => <h1 className="mt-5 mb-2.5 border-b border-[var(--line)] pb-1.5 text-[18px] font-semibold text-[var(--ink)] tracking-[-0.01em]" {...props} />,
                h2: ({ node: _n, ...props }) => <h2 className="mt-5 mb-2 text-[16px] font-semibold text-[var(--ink)] tracking-[-0.005em]" {...props} />,
                h3: ({ node: _n, ...props }) => <h3 className="mt-4 mb-1.5 text-[15px] font-semibold text-[var(--ink)]" {...props} />,
                h4: ({ node: _n, ...props }) => <h4 className="mt-3 mb-1 text-[14px] font-semibold text-[var(--ink-soft)]" {...props} />,
                h5: ({ node: _n, ...props }) => <h5 className="mt-2.5 mb-1 font-ui text-[12px] font-semibold uppercase tracking-[0.05em] text-[var(--ink-soft)]" {...props} />,
                h6: ({ node: _n, ...props }) => <h6 className="mt-2 mb-1 font-ui text-[11px] font-medium uppercase tracking-[0.08em] text-[var(--ink-muted)]" {...props} />,
                p: ({ node: _n, ...props }) => <p className="my-2.5 font-reading text-[15.5px] leading-[1.75] text-[var(--ink-soft)]" {...props} />,
                ul: ({ node: _n, ...props }) => <ul className="my-2.5 space-y-1 pl-5 list-disc marker:text-[var(--ink-faint)]" {...props} />,
                ol: ({ node: _n, ...props }) => <ol className="my-2.5 space-y-1 pl-5 list-decimal marker:text-[var(--ink-faint)] marker:font-mono marker:text-[12px]" {...props} />,
                li: ({ node: _n, ...props }) => <li className="font-reading text-[15px] leading-[1.7] text-[var(--ink-soft)]" {...props} />,
                code: ({ node: _n, className, children, ...props }) => {
                  const match = /language-(\w+)/.exec(className || '')
                  if (match) {
                    return <code className={className} {...props}>{children}</code>
                  }
                  return <code className="font-mono text-[12.5px] font-medium text-[var(--accent)] bg-[var(--paper-dark)] px-1.5 py-0.5" {...props}>{children}</code>
                },
                pre: ({ node: _n, children }) => {
                  const { language, code } = extractCodeInfo(children)
                  return (
                    <SyntaxHighlighter
                      language={language}
                      style={oneDark as any}
                      showLineNumbers
                      startingLineNumber={1}
                      lineNumberStyle={{
                        color: 'var(--code-line)',
                        paddingRight: '1.5em',
                        userSelect: 'none',
                        minWidth: '2.5em',
                        textAlign: 'right',
                      } as any}
                      customStyle={{
                        margin: '0.875rem 0',
                        borderRadius: 'var(--radius)',
                        border: '1px solid var(--code-border)',
                        backgroundColor: 'var(--code-bg)',
                        fontSize: '0.8rem',
                        padding: '0.875rem 1rem',
                      }}
                      codeTagProps={{
                        style: {
                          fontFamily: 'var(--font-mono, "IBM Plex Mono", "Cascadia Code", "JetBrains Mono", Consolas, monospace)',
                          fontSize: '0.8rem',
                          color: 'var(--code-ink)',
                        }
                      }}
                    >
                      {code}
                    </SyntaxHighlighter>
                  )
                },
                a: ({ node: _n, ...props }) => (
                  <a className="text-[var(--accent)] font-medium hover:underline underline-offset-2" target="_blank" rel="noopener noreferrer" {...props} />
                ),
                blockquote: ({ node: _n, ...props }) => (
                  <blockquote className="my-3 border-l-2 border-[var(--accent)] bg-[var(--paper-soft)] py-1.5 pl-4 pr-3 font-reading italic text-[var(--ink-muted)]" {...props} />
                ),
                table: ({ node: _n, ...props }) => (
                  <div className="my-3 overflow-auto border border-[var(--line)]">
                    <table className="w-full border-collapse text-sm" {...props} />
                  </div>
                ),
                thead: ({ node: _n, ...props }) => <thead className="bg-[var(--paper-dark)]" {...props} />,
                th: ({ node: _n, ...props }) => (
                  <th className="border-b border-[var(--line)] px-3 py-1.5 text-left font-ui text-[12px] font-semibold uppercase tracking-[0.04em] text-[var(--ink)]" {...props} />
                ),
                td: ({ node: _n, ...props }) => (
                  <td className="border-b border-[var(--line-soft)] px-3 py-1.5 font-reading text-[14px] text-[var(--ink-soft)]" {...props} />
                ),
                hr: ({ node: _n, ...props }) => <hr className="my-4 border-0 border-t border-[var(--line)]" {...props} />,
                strong: ({ node: _n, ...props }) => <strong className="font-semibold text-[var(--ink)]" {...props} />,
                em: ({ node: _n, ...props }) => <em className="text-[var(--ink-soft)]" {...props} />,
                del: ({ node: _n, ...props }) => <del className="text-[var(--ink-muted)]" {...props} />,
                img: ({ node: _n, ...props }) => (
                  <img className="my-3 max-w-full border border-[var(--line)]" {...props} />
                ),
              }}
            >
              {message.content}
            </ReactMarkdown>
          </div>
        )}
      </div>
      <footer className="flex items-center justify-between border-t border-[var(--line-soft)] bg-[var(--paper-soft)] px-4 py-2">
        <div className="flex items-center gap-3">
          <span className="font-mono tabular text-[10px] uppercase tracking-[0.1em] text-[var(--ink-faint)]">
            {message.elapsed_ms > 0 ? `${(message.elapsed_ms / 1000).toFixed(1)}s` : ''}
          </span>
          {!message.loading && message.error && onRetry && (
            <button
              type="button"
              onClick={() => onRetry(message)}
              className="border border-[var(--danger)] px-2 py-0.5 font-ui text-[11px] font-medium text-[var(--danger)] hover:bg-[var(--danger)] hover:text-[var(--surface-raised)]"
            >
              重试
            </button>
          )}
          {!message.loading && !message.error && message.content && (
            <button
              type="button"
              onClick={handleCopy}
              className="font-ui text-[11px] font-medium text-[var(--ink-muted)] hover:text-[var(--ink)]"
            >
              {copied ? '已复制 ✓' : '复制'}
            </button>
          )}
        </div>
        <KeepSwitch
          checked={message.kept}
          onChange={() => onToggleKeep(message.id)}
        />
      </footer>
    </article>
  )
}
