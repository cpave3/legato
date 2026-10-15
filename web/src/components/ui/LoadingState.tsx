import { Loader2 } from "lucide-react"

import { cn } from "../../lib/utils"

interface LoadingStateProps {
  // What is loading, said as it is shown: "Loading the session".
  label: string
  // A quieter line under it: which agent, which item.
  detail?: string
  // "fill" takes the whole of the space it is in and centres in it; "inline"
  // is a line among other content (a menu, a list, a row).
  size?: "fill" | "inline"
  className?: string
}

// LoadingState is what a part of the page shows while its content is on its
// way: a spinner and a few words, centred. It fades in after a moment, so a
// load that is quick never flashes it.
export function LoadingState({ label, detail, size = "fill", className }: LoadingStateProps) {
  if (size === "inline") {
    return (
      <div role="status" aria-label={label} className={cn("lw-loading inline-flex items-center gap-2 font-lw text-[13px] text-lw-text-2", className)}>
        <Loader2 aria-hidden="true" size={14} className="shrink-0 animate-spin text-lw-faint motion-reduce:animate-none" />
        <span>{label}</span>
      </div>
    )
  }
  return (
    <div
      role="status"
      aria-label={label}
      // flex-1 takes the space of a flex parent, in a row or a column; w-full
      // and h-full take it from a block parent, and min-h keeps it from
      // collapsing in one that has no height of its own.
      className={cn("lw-loading flex h-full min-h-40 w-full min-w-0 flex-1 flex-col items-center justify-center gap-3 p-6 text-center font-lw", className)}
    >
      <Loader2 aria-hidden="true" size={20} className="animate-spin text-lw-faint motion-reduce:animate-none" />
      <div className="flex flex-col gap-1">
        <span className="text-[13px] font-medium text-lw-text-2">{label}</span>
        {detail && <span className="font-lw-mono text-xs text-lw-faint">{detail}</span>}
      </div>
    </div>
  )
}
