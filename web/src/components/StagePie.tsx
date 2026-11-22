import { cn } from "../lib/utils"
import { needsYouReason, stageFill, stageLabel } from "../lib/agentStage"
import type { AgentInfo } from "../hooks/useWebSocket"

// StagePie is a stage's glyph: a circle filled a quarter more at each stage,
// empty while exploring, full in the done colour when finished. It is never
// the only sign of the stage: a label always goes with it.
export function StagePie({ stage, size = 12, className, label }: { stage: string | undefined; size?: number; className?: string; label?: string }) {
  const fill = stageFill(stage)
  const finished = stage === "finished"
  const r = 5
  const name = label ?? stageLabel(stage)
  // The filled part, clockwise from twelve o'clock.
  const angle = fill * 2 * Math.PI
  const x = 6 + r * Math.sin(angle)
  const y = 6 - r * Math.cos(angle)
  const slice = fill > 0 && fill < 1 ? `M6 6 L6 1 A${r} ${r} 0 ${fill > 0.5 ? 1 : 0} 1 ${x.toFixed(3)} ${y.toFixed(3)} Z` : ""
  return (
    <svg
      role="img"
      aria-label={name}
      width={size}
      height={size}
      viewBox="0 0 12 12"
      className={cn("shrink-0", finished ? "text-lw-done" : "text-lw-text-2", className)}
    >
      <circle cx="6" cy="6" r={r} fill={finished ? "currentColor" : "none"} stroke="currentColor" strokeWidth="1.5" />
      {slice && <path d={slice} fill="currentColor" />}
    </svg>
  )
}

// StageLine is an agent's stage on one line: the pie, the stage's word and
// why it is there.
export function StageLine({ agent, className }: { agent: Pick<AgentInfo, "stage" | "stage_reason">; className?: string }) {
  const label = stageLabel(agent.stage)
  if (!label) return null
  const finished = agent.stage === "finished"
  return (
    <span className={cn("flex min-w-0 items-center gap-1.5 text-xs", className)}>
      <StagePie stage={agent.stage} />
      <span className={cn("shrink-0", finished ? "text-lw-done" : "text-lw-text")}>{label}</span>
      {agent.stage_reason && <span className="min-w-0 truncate text-lw-text-2">· {agent.stage_reason}</span>}
    </span>
  )
}

// NeedsYouLine says why an agent needs the operator, nothing when it does
// not.
export function NeedsYouLine({ agent, className }: { agent: AgentInfo; className?: string }) {
  const reason = needsYouReason(agent)
  if (!reason) return null
  return <span className={cn("truncate text-xs text-lw-needs", className)}>◆ {reason}</span>
}
