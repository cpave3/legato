import { cn } from "../lib/utils"
import type { Segment, Tone } from "../lib/workflowView"
import { toneGlyph } from "../lib/workflowTones"

const pillLook: Record<Tone, string> = {
  live: "bg-lw-live-soft text-lw-live",
  needs: "bg-lw-needs-soft text-lw-needs",
  done: "bg-lw-done-soft text-lw-done",
  error: "bg-lw-error-soft text-lw-error",
  idle: "bg-lw-idle-soft text-lw-text-2",
}

export function StatePill({ tone, label, className }: { tone: Tone; label: string; className?: string }) {
  return (
    <span className={cn("inline-flex shrink-0 items-center gap-1.5 whitespace-nowrap rounded-md px-2 py-0.5 text-xs font-medium", pillLook[tone], className)}>
      <span aria-hidden="true">{toneGlyph[tone]}</span>{label}
    </span>
  )
}

const segmentLook: Record<Segment, string> = {
  ok: "bg-lw-faint",
  empty: "bg-lw-line-strong",
  live: "bg-lw-live",
  needs: "bg-lw-needs",
  done: "bg-lw-done",
  error: "bg-lw-error",
  idle: "bg-lw-text-2",
}

// One segment per step of a run. Its label is read out in place of the bar.
export function StepBar({ segments, label, className }: { segments: Segment[]; label: string; className?: string }) {
  return (
    <div role="img" aria-label={label} className={cn("flex gap-[3px]", className)}>
      {segments.map((segment, index) => <span key={index} className={cn("h-1.5 flex-1 rounded-sm", segmentLook[segment])} />)}
    </div>
  )
}
