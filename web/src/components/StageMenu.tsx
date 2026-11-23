import { useEffect, useRef, useState } from "react"
import { ChevronDown, X } from "lucide-react"
import type { AgentInfo } from "../hooks/useWebSocket"
import { cn } from "../lib/utils"
import { stageCounts, stageLabel, workStages, type WorkStage } from "../lib/agentStage"
import { StageTrack } from "./StageTrack"

// StageMenu filters a list of agents by stage: one Stage ▾ button that opens
// a menu of All and the five stages, each with its track and count, those
// with no agents dimmed but still choosable. While a filter is on, the
// button reads as the stage (its track and word) and a ✕ beside it clears
// the filter. The sidebar and the phone's picker share it; touch makes every
// control 44px high.
export function StageMenu({ agents, filter, onFilter, touch = false, className }: {
  // The agents counted, the Reeve left out.
  agents: AgentInfo[]
  filter: WorkStage | null
  onFilter: (stage: WorkStage | null) => void
  touch?: boolean
  className?: string
}) {
  const [open, setOpen] = useState(false)
  const box = useRef<HTMLDivElement>(null)
  const counts = stageCounts(agents)

  useEffect(() => {
    if (!open) return
    const close = (event: Event) => {
      if (event instanceof KeyboardEvent ? event.key === "Escape" : !box.current?.contains(event.target as Node)) setOpen(false)
    }
    document.addEventListener("mousedown", close)
    document.addEventListener("keydown", close)
    return () => {
      document.removeEventListener("mousedown", close)
      document.removeEventListener("keydown", close)
    }
  }, [open])

  const choose = (stage: WorkStage | null) => {
    setOpen(false)
    onFilter(stage)
  }

  const height = touch ? "min-h-11" : "h-7"
  const item = (stage: WorkStage | null, count: number) => (
    <button
      key={stage ?? "all"}
      type="button"
      role="menuitemradio"
      aria-checked={filter === stage}
      onClick={() => choose(stage)}
      className={cn(
        "flex w-full items-center gap-2 px-3 text-left hover:bg-lw-raised focus:bg-lw-raised focus:outline-none",
        touch ? "min-h-11 text-[13px]" : "py-1.5 text-xs",
        filter === stage ? "text-lw-link" : count === 0 ? "text-lw-faint" : "text-lw-text",
      )}
    >
      <span className="flex w-[30px] shrink-0 justify-start">{stage && <StageTrack stage={stage} size="xs" className={count === 0 ? "opacity-50" : undefined} />}</span>
      <span className="flex-1">{stage ? stageLabel(stage) : "All"}</span>
      <span className="font-lw-mono text-lw-text-2">{count}</span>
    </button>
  )

  return (
    <div ref={box} className={cn("relative flex shrink-0 items-center", className)}>
      <div className={cn("flex items-center rounded-md border", filter ? "border-lw-accent bg-lw-selected" : "border-lw-line bg-lw-raised")}>
        <button
          type="button"
          aria-label={filter ? `Filter by stage: ${stageLabel(filter)}` : "Filter by stage"}
          aria-haspopup="menu"
          aria-expanded={open}
          onClick={() => setOpen((value) => !value)}
          className={cn("flex items-center gap-1.5 rounded-md px-2 text-xs font-medium text-lw-text hover:text-lw-text", height)}
        >
          {filter ? <><StageTrack stage={filter} size="xs" />{stageLabel(filter)}</> : <>Stage<ChevronDown size={12} aria-hidden="true" className="text-lw-text-2" /></>}
        </button>
        {filter && (
          <button
            type="button"
            aria-label="Clear stage filter"
            title="Clear stage filter"
            onClick={() => onFilter(null)}
            className={cn("flex items-center justify-center rounded-md text-lw-text-2 hover:text-lw-text", touch ? "size-11" : "h-7 w-6")}
          >
            <X size={12} aria-hidden="true" />
          </button>
        )}
      </div>
      {open && (
        <div role="menu" aria-label="Stage" className="absolute right-0 top-full z-40 mt-1 min-w-48 rounded-md border border-lw-line-strong bg-lw-surface py-1 font-lw shadow-xl">
          {item(null, agents.length)}
          {workStages.map((stage) => item(stage, counts[stage]))}
        </div>
      )}
    </div>
  )
}
