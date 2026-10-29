import { agentKindLabel, agentKindTone } from "../lib/agentKind"
import { cn } from "../lib/utils"

interface AgentKindBadgeProps {
  kind: string | undefined
  // Driven through its native endpoint rather than its terminal.
  isNative: boolean
  className?: string
}

// Which agent tool a session runs and how Legato is connected to it. Chimera
// and Claude Code share the native panel, so the panel alone does not say.
export function AgentKindBadge({ kind, isNative, className }: AgentKindBadgeProps) {
  const label = agentKindLabel(kind)
  const connection = isNative ? "native" : "terminal"
  return (
    <span
      aria-label={`${label}, ${connection} session`}
      title={`${label} · ${connection} session`}
      className={cn(
        "inline-flex shrink-0 items-center gap-1 rounded border px-1.5 py-0.5 text-[10px] font-medium leading-none",
        agentKindTone(kind),
        className,
      )}
    >
      <span>{label}</span>
      <span className="opacity-60">{connection}</span>
    </span>
  )
}

// An agent that runs outside Legato's sandbox while the sandbox is on.
export function UnsandboxedBadge({ className }: { className?: string }) {
  return (
    <span
      title="Outside Legato's sandbox: it sees your whole home folder"
      className={cn("inline-flex shrink-0 items-center rounded border border-lw-needs/40 bg-lw-needs-soft px-1.5 py-0.5 text-[10px] font-medium leading-none text-lw-needs", className)}
    >
      unsandboxed
    </span>
  )
}
