import ReactMarkdown from "react-markdown"
import remarkGfm from "remark-gfm"
import { cn } from "../lib/utils"

export function WorkflowMarkdownPreview({ markdown }: { markdown: string }) {
  return (
    <div className="min-w-0 break-words text-[15px] leading-7 text-zinc-200">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
          p: ({ children }) => <p className="my-4 first:mt-0 last:mb-0">{children}</p>,
          h1: ({ children }) => <h1 className="mb-4 mt-8 border-b border-zinc-800 pb-3 text-2xl font-semibold leading-tight text-zinc-100 first:mt-0">{children}</h1>,
          h2: ({ children }) => <h2 className="mb-3 mt-8 text-xl font-semibold leading-tight text-zinc-100 first:mt-0">{children}</h2>,
          h3: ({ children }) => <h3 className="mb-2 mt-6 text-lg font-semibold leading-tight text-zinc-100 first:mt-0">{children}</h3>,
          h4: ({ children }) => <h4 className="mb-2 mt-5 font-semibold text-zinc-100 first:mt-0">{children}</h4>,
          ul: ({ children }) => <ul className="my-4 list-disc space-y-2 pl-6 marker:text-zinc-500">{children}</ul>,
          ol: ({ children }) => <ol className="my-4 list-decimal space-y-2 pl-6 marker:text-zinc-500">{children}</ol>,
          li: ({ children }) => <li className="pl-1">{children}</li>,
          blockquote: ({ children }) => <blockquote className="my-5 border-l-4 border-zinc-700 py-1 pl-5 text-zinc-400">{children}</blockquote>,
          hr: () => <hr className="my-8 border-zinc-800" />,
          a: ({ children, href }) => <a href={href} target="_blank" rel="noreferrer" className="text-indigo-300 underline decoration-indigo-500/60 underline-offset-4">{children}</a>,
          pre: ({ children }) => <pre className="my-5 max-w-full overflow-x-auto rounded-lg border border-zinc-800 bg-black/40 p-4 font-mono text-sm leading-6 text-zinc-200">{children}</pre>,
          code: ({ children, className, ...props }) => <code {...props} className={cn(className, !className && "rounded bg-zinc-800 px-1.5 py-0.5 font-mono text-[0.9em] text-zinc-100")}>{children}</code>,
          table: ({ children }) => <div className="my-5 overflow-x-auto rounded-lg border border-zinc-800"><table className="w-full border-collapse text-sm">{children}</table></div>,
          th: ({ children }) => <th className="border-b border-r border-zinc-700 bg-zinc-900 px-4 py-3 text-left font-semibold last:border-r-0">{children}</th>,
          td: ({ children }) => <td className="border-b border-r border-zinc-800 px-4 py-3 align-top last:border-r-0">{children}</td>,
        }}
      >
        {markdown}
      </ReactMarkdown>
    </div>
  )
}
