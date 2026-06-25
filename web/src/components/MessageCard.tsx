import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import type { Message } from '../types'
import KeepSwitch from './KeepSwitch'

interface MessageCardProps {
  message: Message
  siteName: string
  onToggleKeep: (id: string) => void
}

export default function MessageCard({ message, siteName, onToggleKeep }: MessageCardProps) {
  return (
    <div className="flex flex-col rounded-lg border border-slate-700 bg-slate-800 p-4">
      <div className="mb-2 border-b border-slate-700 pb-2 text-sm font-semibold text-slate-200">
        {siteName}
      </div>
      <div className="min-h-[120px] flex-1 overflow-auto text-sm text-slate-300">
        {message.error ? (
          <span className="text-red-400">{message.error}</span>
        ) : (
          <ReactMarkdown
            remarkPlugins={[remarkGfm]}
            components={{
              h1: ({ node: _n, ...props }) => <h1 className="mt-2 mb-1 text-base font-semibold" {...props} />,
              h2: ({ node: _n, ...props }) => <h2 className="mt-2 mb-1 text-base font-semibold" {...props} />,
              h3: ({ node: _n, ...props }) => <h3 className="mt-2 mb-1 text-sm font-semibold" {...props} />,
              h4: ({ node: _n, ...props }) => <h4 className="mt-2 mb-1 text-sm font-semibold" {...props} />,
              p: ({ node: _n, ...props }) => <p className="my-1 leading-relaxed" {...props} />,
              ul: ({ node: _n, ...props }) => <ul className="my-1 list-disc pl-5" {...props} />,
              ol: ({ node: _n, ...props }) => <ol className="my-1 list-decimal pl-5" {...props} />,
              li: ({ node: _n, ...props }) => <li className="my-0.5" {...props} />,
              code: ({ node: _n, className, children, ...props }) => {
                const match = /language-(\w+)/.exec(className || '')
                return match ? (
                  <pre className="my-1 overflow-auto rounded bg-slate-900 p-2 text-xs">
                    <code className={className} {...props}>{children}</code>
                  </pre>
                ) : (
                  <code className="rounded bg-slate-900 px-1 py-0.5 text-xs" {...props}>{children}</code>
                )
              },
              a: ({ node: _n, ...props }) => (
                <a className="text-blue-400 hover:underline" target="_blank" rel="noopener noreferrer" {...props} />
              ),
              blockquote: ({ node: _n, ...props }) => (
                <blockquote className="my-1 border-l-2 border-slate-600 pl-2 text-slate-400" {...props} />
              ),
              table: ({ node: _n, ...props }) => (
                <table className="my-1 border-collapse text-xs" {...props} />
              ),
              th: ({ node: _n, ...props }) => (
                <th className="border border-slate-600 px-2 py-1" {...props} />
              ),
              td: ({ node: _n, ...props }) => (
                <td className="border border-slate-600 px-2 py-1" {...props} />
              ),
            }}
          >
            {message.content}
          </ReactMarkdown>
        )}
      </div>
      <div className="mt-2 flex items-center justify-between border-t border-slate-700 pt-2">
        <span className="text-xs text-slate-500">
          {message.elapsed_ms > 0 ? `${message.elapsed_ms}ms` : ''}
        </span>
        <KeepSwitch
          checked={message.kept}
          onChange={() => onToggleKeep(message.id)}
        />
      </div>
    </div>
  )
}
