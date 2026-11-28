import { agentKindTone, agentToolLabel } from "../lib/agentKind"
import { cn } from "../lib/utils"

interface AgentKindBadgeProps {
  kind: string | undefined
  // The engram the agent runs as, named after the tool.
  engram?: string
  accountLabel?: string
  // Driven through its native endpoint rather than its terminal.
  isNative: boolean
  className?: string
}

// Which agent tool a session runs and how Legato is connected to it. Chimera
// and Claude Code share the native panel, so the panel alone does not say.
export function AgentKindBadge({ kind, engram, accountLabel, isNative, className }: AgentKindBadgeProps) {
  const tool = agentToolLabel(kind, engram)
  const label = kind === "claude-code" && accountLabel ? accountLabel : tool
  const identity = label === tool ? label : `${label}, ${tool}`
  const connection = isNative ? "native" : "terminal"
  return (
    <span
      aria-label={`${identity}, ${connection} session`}
      title={`${identity} · ${connection} session`}
      className={cn(
        "inline-flex shrink-0 items-center gap-1 rounded border px-1.5 py-0.5 text-[10px] font-medium leading-none",
        agentKindTone(kind),
        className,
      )}
    >
      <span className="min-w-0 max-w-36 truncate">{label}</span>
      <span className="opacity-60">{connection}</span>
    </span>
  )
}

// A sandboxed agent whose containers may use the host's network.
export function HostNetworkBadge({ className }: { className?: string }) {
  return (
    <span
      title="Its containers may use the host's network: they reach what this machine reaches, Legato's own local services included"
      className={cn("inline-flex shrink-0 items-center rounded border border-lw-needs/40 bg-lw-needs-soft px-1.5 py-0.5 text-[10px] font-medium leading-none text-lw-needs", className)}
    >
      host network
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
