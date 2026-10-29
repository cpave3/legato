import { agentKindLabel, agentKindTone } from "../lib/agentKind"
import { cn } from "../lib/utils"

interface AgentKindBadgeProps {
  kind: string | undefined
  // Driven through its native endpoint rather than its terminal.
  isNative: boolean
  className?: string
  // The sandbox is on and this agent runs outside it.
  unsandboxed?: boolean
}

// Which agent tool a session runs and how Legato is connected to it. Chimera
// and Claude Code share the native panel, so the panel alone does not say.
export function AgentKindBadge({ kind, isNative, unsandboxed, className }: AgentKindBadgeProps) {
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
      {unsandboxed && <span className="rounded-sm bg-lw-needs-soft px-1 text-lw-needs" title="Outside Legato's sandbox: it sees your whole home folder">unsandboxed</span>}
    </span>
  )
}
