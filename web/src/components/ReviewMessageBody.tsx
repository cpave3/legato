import ReactMarkdown from "react-markdown"
import remarkGfm from "remark-gfm"
import { cn } from "../lib/utils"

interface ReviewMessageBodyProps {
  body: string
}

export function ReviewMessageBody({ body }: ReviewMessageBodyProps) {
  return (
    <div className="min-w-0 break-words text-sm leading-5">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
          p: ({ children }) => <p className="my-2 first:mt-0 last:mb-0">{children}</p>,
          h1: ({ children }) => <h1 className="mb-1 mt-3 text-base font-semibold text-zinc-100 first:mt-0">{children}</h1>,
          h2: ({ children }) => <h2 className="mb-1 mt-3 text-sm font-semibold text-zinc-100 first:mt-0">{children}</h2>,
          h3: ({ children }) => <h3 className="mb-1 mt-3 text-sm font-semibold text-zinc-100 first:mt-0">{children}</h3>,
          ul: ({ children }) => <ul className="my-2 list-disc space-y-1 pl-5">{children}</ul>,
          ol: ({ children }) => <ol className="my-2 list-decimal space-y-1 pl-5">{children}</ol>,
          blockquote: ({ children }) => <blockquote className="my-2 border-l-2 border-zinc-600 pl-3 text-zinc-400">{children}</blockquote>,
          a: ({ children, href }) => <a href={href} target="_blank" rel="noreferrer" className="text-indigo-300 underline decoration-indigo-500/60 underline-offset-2">{children}</a>,
          pre: ({ children }) => <pre className="my-2 max-w-full overflow-x-auto rounded border border-zinc-700 bg-black/40 p-2 font-mono text-xs leading-5 text-zinc-200">{children}</pre>,
          code: ({ children, className, ...props }) => (
            <code {...props} className={cn(className, !className && "rounded bg-black/30 px-1 py-0.5 font-mono text-[0.9em] text-zinc-100")}>{children}</code>
          ),
          table: ({ children }) => <div className="my-2 overflow-x-auto"><table className="w-full border-collapse text-xs">{children}</table></div>,
          th: ({ children }) => <th className="border border-zinc-700 bg-zinc-900 px-2 py-1 text-left">{children}</th>,
          td: ({ children }) => <td className="border border-zinc-700 px-2 py-1 align-top">{children}</td>,
        }}
      >
        {body}
      </ReactMarkdown>
    </div>
  )
}
