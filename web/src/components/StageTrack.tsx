import { cn } from "../lib/utils"
import { needsYouReason, reeveWaitLabel, stageLabel, stageSteps } from "../lib/agentStage"
import { formatAgentTime } from "../lib/agentList"
import { useNow } from "../hooks/useNow"
import type { AgentInfo } from "../hooks/useWebSocket"
import { useReeveName } from "../lib/reeveName"

// The sizes of a track's bars: "sm" in rows and chips, "xs" on board cards
// and the phone, "lg" where it needs more weight.
export type StageTrackSize = "xs" | "sm" | "lg"

const bars: Record<StageTrackSize, string> = {
  xs: "h-[9px] w-[3px]",
  sm: "h-[10px] w-[4px]",
  lg: "h-[14px] w-[6px]",
}

const gaps: Record<StageTrackSize, string> = { xs: "gap-[2px]", sm: "gap-[2px]", lg: "gap-[3px]" }

// StageTrack is a stage's mark: five bars, lit up to the stage, all five in
// the done colour when finished. It is never the only sign of the stage:
// its word goes beside it (then label is "" and the track is hidden from
// assistive technology), or it is named by the word (the default) or label.
export function StageTrack({ stage, size = "sm", className, label }: { stage: string | undefined; size?: StageTrackSize; className?: string; label?: string }) {
  const steps = stageSteps(stage)
  if (steps === 0) return null
  const finished = stage === "finished"
  const name = label ?? stageLabel(stage)
  const named = name !== ""
  return (
    <span
      role={named ? "img" : undefined}
      aria-label={named ? name : undefined}
      aria-hidden={named ? undefined : true}
      title={named ? name : undefined}
      data-stage-track={stage}
      className={cn("inline-flex shrink-0 items-center", gaps[size], className)}
    >
      {[1, 2, 3, 4, 5].map((step) => (
        <span
          key={step}
          data-lit={step <= steps || undefined}
          className={cn("rounded-[1px]", bars[size], step > steps ? "bg-lw-track-off" : finished ? "bg-lw-done" : "bg-lw-text")}
        />
      ))}
    </span>
  )
}

// StageLine is an agent's stage on one line: the track, the stage's word and
// why it is there.
export function StageLine({ agent, className }: { agent: Pick<AgentInfo, "stage" | "stage_reason">; className?: string }) {
  const label = stageLabel(agent.stage)
  if (!label) return null
  const finished = agent.stage === "finished"
  return (
    <span className={cn("flex min-w-0 items-center gap-1.5 text-xs", className)}>
      <StageTrack stage={agent.stage} />
      <span className={cn("shrink-0", finished ? "text-lw-done" : "text-lw-text")}>{label}</span>
      {agent.stage_reason && <span className="min-w-0 truncate text-lw-text-2">· {agent.stage_reason}</span>}
    </span>
  )
}

// NeedsYouLine says why an agent needs the operator, nothing when it does
// not.
export function NeedsYouLine({ agent, className }: { agent: AgentInfo; className?: string }) {
  const reason = needsYouReason(agent, useReeveName())
  if (!reason) return null
  return <span className={cn("truncate text-xs text-lw-needs", className)}>◆ {reason}</span>
}

// ReeveWaitLine is the line of an agent working because it waits on the
// Reeve's answer: in the Reeve's violet, with how long it has waited.
export function ReeveWaitLine({ agent, className }: { agent: AgentInfo; className?: string }) {
  const now = useNow(30_000)
  const label = reeveWaitLabel(agent, useReeveName())
  if (!label) return null
  const since = agent.waiting_since ? Date.parse(agent.waiting_since) : NaN
  const waited = Number.isNaN(since) ? "" : formatAgentTime(Math.max(0, Math.floor((now - since) / 1000)))
  return <span className={cn("truncate text-xs text-[var(--theme-keyword,#a78bfa)]", className)}>{waited ? `${label} · ${waited}` : label}</span>
}

// FinishedReason is a Finished agent's line where its group already says
// Finished: only why it finished, and that it left a note for the operator.
export function FinishedReason({ agent, unread, className }: { agent: Pick<AgentInfo, "stage_reason">; unread: boolean; className?: string }) {
  const text = [agent.stage_reason, unread && "a note for you"].filter(Boolean).join(" · ")
  if (!text) return null
  return <span className={cn("truncate text-xs text-lw-text-2", className)}>{text}</span>
}
