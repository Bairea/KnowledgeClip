import { ReactNode } from 'react'
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

export default function MessageCard({ message, siteName, onToggleKeep }: MessageCardProps) {
  return (
    <div className="flex flex-col rounded-lg border border-slate-700 bg-slate-800">
      <div className="flex items-center justify-between border-b border-slate-700 px-4 py-2">
        <span className="text-sm font-semibold text-slate-200">{siteName}</span>
        {message.loading && (
          <span className="animate-pulse text-xs text-slate-400">generating...</span>
        )}
      </div>
      <div className="max-h-[70vh] min-h-[100px] flex-1 overflow-auto px-4 py-3">
        {message.error ? (
          <span className="text-sm text-red-400">{message.error}</span>
        ) : message.loading && !message.content ? (
          <div className="flex h-32 items-center justify-center">
            <span className="animate-pulse text-sm text-slate-500">waiting for response...</span>
          </div>
        ) : (
          <div className="prose prose-invert prose-sm max-w-none
            prose-headings:scroll-mt-20
            prose-h1:text-lg prose-h1:font-bold prose-h1:text-slate-100
            prose-h2:text-base prose-h2:font-bold prose-h2:text-slate-100
            prose-h3:text-sm prose-h3:font-semibold prose-h3:text-slate-100
            prose-h4:text-sm prose-h4:font-semibold prose-h4:text-slate-200
            prose-p:text-sm prose-p:leading-relaxed prose-p:text-slate-300
            prose-li:text-sm prose-li:text-slate-300
            prose-strong:text-slate-100 prose-strong:font-semibold
            prose-em:text-slate-200
            prose-code:text-pink-300 prose-code:before:content-none prose-code:after:content-none
            prose-pre:bg-transparent prose-pre:border-0 prose-pre:p-0
            prose-blockquote:border-l-slate-500 prose-blockquote:text-slate-400
            prose-a:text-blue-400 prose-a:no-underline hover:prose-a:underline
            prose-table:text-xs
            prose-th:bg-slate-700 prose-th:text-slate-200 prose-th:font-semibold
            prose-td:text-slate-300
            prose-hr:border-slate-700
          ">
            <ReactMarkdown
              remarkPlugins={[remarkGfm]}
              components={{
                h1: ({ node: _n, ...props }) => <h1 className="mt-3 mb-2 border-b border-slate-700 pb-1 text-base font-bold text-slate-100" {...props} />,
                h2: ({ node: _n, ...props }) => <h2 className="mt-3 mb-2 text-sm font-bold text-slate-100" {...props} />,
                h3: ({ node: _n, ...props }) => <h3 className="mt-2 mb-1 text-sm font-semibold text-slate-100" {...props} />,
                h4: ({ node: _n, ...props }) => <h4 className="mt-2 mb-1 text-sm font-semibold text-slate-200" {...props} />,
                h5: ({ node: _n, ...props }) => <h5 className="mt-1 mb-0.5 text-xs font-semibold text-slate-200" {...props} />,
                h6: ({ node: _n, ...props }) => <h6 className="mt-1 mb-0.5 text-xs font-medium text-slate-300" {...props} />,
                p: ({ node: _n, ...props }) => <p className="my-1.5 text-sm leading-relaxed text-slate-300" {...props} />,
                ul: ({ node: _n, ...props }) => <ul className="my-1.5 space-y-0.5 pl-5 list-disc marker:text-slate-500" {...props} />,
                ol: ({ node: _n, ...props }) => <ol className="my-1.5 space-y-0.5 pl-5 list-decimal marker:text-slate-500" {...props} />,
                li: ({ node: _n, ...props }) => <li className="text-sm leading-relaxed text-slate-300" {...props} />,
                code: ({ node: _n, className, children, ...props }) => {
                  const match = /language-(\w+)/.exec(className || '')
                  if (match) {
                    return <code className={className} {...props}>{children}</code>
                  }
                  return <code className="rounded bg-slate-900 px-1.5 py-0.5 text-xs text-pink-300" {...props}>{children}</code>
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
                        color: '#475569',
                        paddingRight: '1.5em',
                        userSelect: 'none',
                        minWidth: '2.5em',
                        textAlign: 'right',
                      } as any}
                      customStyle={{
                        margin: '0.5rem 0',
                        borderRadius: '0.375rem',
                        border: '1px solid #334155',
                        backgroundColor: '#0f172a',
                        fontSize: '0.75rem',
                        padding: '0.75rem',
                      }}
                      codeTagProps={{
                        style: {
                          fontFamily: '"Cascadia Code", "Fira Code", "JetBrains Mono", "Consolas", monospace',
                          fontSize: '0.75rem',
                        }
                      }}
                    >
                      {code}
                    </SyntaxHighlighter>
                  )
                },
                a: ({ node: _n, ...props }) => (
                  <a className="text-blue-400 hover:underline" target="_blank" rel="noopener noreferrer" {...props} />
                ),
                blockquote: ({ node: _n, ...props }) => (
                  <blockquote className="my-2 border-l-2 border-slate-500 pl-3 text-slate-400" {...props} />
                ),
                table: ({ node: _n, ...props }) => (
                  <div className="my-2 overflow-auto rounded-md border border-slate-700">
                    <table className="w-full border-collapse text-xs" {...props} />
                  </div>
                ),
                thead: ({ node: _n, ...props }) => <thead className="bg-slate-700" {...props} />,
                th: ({ node: _n, ...props }) => (
                  <th className="border-b border-slate-600 px-3 py-1.5 text-left font-semibold text-slate-200" {...props} />
                ),
                td: ({ node: _n, ...props }) => (
                  <td className="border-b border-slate-700 px-3 py-1.5 text-slate-300" {...props} />
                ),
                hr: ({ node: _n, ...props }) => <hr className="my-3 border-slate-700" {...props} />,
                strong: ({ node: _n, ...props }) => <strong className="font-semibold text-slate-100" {...props} />,
                em: ({ node: _n, ...props }) => <em className="text-slate-200" {...props} />,
                del: ({ node: _n, ...props }) => <del className="text-slate-500" {...props} />,
                img: ({ node: _n, ...props }) => (
                  <img className="my-2 max-w-full rounded-md" {...props} />
                ),
              }}
            >
              {message.content}
            </ReactMarkdown>
          </div>
        )}
      </div>
      <div className="flex items-center justify-between border-t border-slate-700 px-4 py-2">
        <span className="text-xs text-slate-500">
          {message.elapsed_ms > 0 ? `${(message.elapsed_ms / 1000).toFixed(1)}s` : ''}
        </span>
        <KeepSwitch
          checked={message.kept}
          onChange={() => onToggleKeep(message.id)}
        />
      </div>
    </div>
  )
}
