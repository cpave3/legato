import { useEffect, useRef, useState, type ReactNode } from "react"
import { Check, Copy } from "lucide-react"
import { cn } from "../lib/utils"

// CopyablePre is a code block with a button in its top right corner that
// copies the block's text, as it is shown, to the clipboard.
export function CopyablePre({ children, className }: { children: ReactNode; className?: string }) {
  const pre = useRef<HTMLPreElement>(null)
  const [copied, setCopied] = useState(false)

  useEffect(() => {
    if (!copied) return
    const timer = window.setTimeout(() => setCopied(false), 1500)
    return () => window.clearTimeout(timer)
  }, [copied])

  const copy = async () => {
    // A fenced block ends in a newline that is not part of the code.
    const text = (pre.current?.textContent ?? "").replace(/\n$/, "")
    try {
      await navigator.clipboard.writeText(text)
      setCopied(true)
    } catch {
      // No clipboard (a page over plain HTTP): the text can still be selected.
    }
  }

  return (
    <div className="group relative">
      <pre ref={pre} className={cn(className, "pr-10")}>{children}</pre>
      <button
        type="button"
        onClick={() => void copy()}
        aria-label={copied ? "Copied" : "Copy code"}
        title={copied ? "Copied" : "Copy code"}
        className={cn(
          "absolute right-1.5 top-1.5 flex h-7 w-7 items-center justify-center rounded-md border border-zinc-700 bg-zinc-900/90 text-zinc-400 transition-opacity hover:text-zinc-100 focus-visible:opacity-100",
          copied ? "text-emerald-400 opacity-100" : "sm:opacity-60 sm:group-hover:opacity-100",
        )}
      >
        {copied ? <Check size={14} aria-hidden="true" /> : <Copy size={14} aria-hidden="true" />}
      </button>
    </div>
  )
}
