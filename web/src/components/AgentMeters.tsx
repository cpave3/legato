import { cn } from "../lib/utils"

const cellTone: Record<string, string> = {
  working: "bg-lw-live",
  waiting: "bg-lw-needs",
  error: "bg-lw-error",
}

// An agent's recent activity, one slanted cell for each stretch of time, as
// the TUI draws it with ▰ and ▱: lit for a stretch it worked, waited or
// failed, dark for one it was idle.
export function ActivityBar({ states, className }: { states?: string[]; className?: string }) {
  if (!states?.length) return null
  return (
    <div aria-label="Recent activity" role="img" className={cn("flex items-center gap-[3px] px-0.5", className)}>
      {states.map((state, index) => (
        <span
          key={index}
          data-state={state}
          className={cn("h-2.5 w-[7px] shrink-0 -skew-x-[20deg] rounded-[1px]", cellTone[state] ?? "bg-lw-line-strong")}
        />
      ))}
    </div>
  )
}

// How full an agent's context window is: green, amber from 70%, red from 85%.
export function ContextMeter({ share, width = "w-12", suffix }: { share: number; width?: string; suffix?: string }) {
  const tone = share >= 85 ? "text-lw-error" : share >= 70 ? "text-lw-needs" : "text-lw-text-2"
  const fill = share >= 85 ? "bg-lw-error" : share >= 70 ? "bg-lw-needs" : "bg-lw-live"
  return (
    <span role="img" aria-label={`Context ${share}% full`} className="flex shrink-0 items-center gap-1.5 text-[11px] text-lw-text-2">
      ctx
      <span className={cn("flex h-1 overflow-hidden rounded-sm bg-lw-line-strong", width)}>
        <span className={cn("rounded-sm", fill)} style={{ width: `${share}%` }} />
      </span>
      <span className={cn("tabular-nums", tone)}>{share}%{suffix}</span>
    </span>
  )
}
