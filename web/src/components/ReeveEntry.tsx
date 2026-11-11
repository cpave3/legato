import { useState } from "react"
import { Crown } from "lucide-react"
import type { AgentInfo } from "../hooks/useWebSocket"
import { resumeReeve, startReeve, type ReeveOverview } from "../lib/reeve"
import { useReeveOverview } from "../hooks/useReeveOverview"
import { cn } from "../lib/utils"
import { AgentKindBadge } from "./AgentKindBadge"
import { ActivityBar } from "./AgentMeters"
import { formatAgentTime, stateSeconds } from "../lib/agentList"
import { VoiceButton } from "../voice/VoiceLauncher"

// The Reeve's violet: a wash from the left edge and a faint border, apart
// from the selection accent.
export const reeveWash = "bg-gradient-to-r from-[#8b5cf6]/20 via-[#8b5cf6]/[0.06] to-transparent shadow-[inset_0_0_0_1px_rgba(167,139,250,0.28)]"

// ReeveEntry is the Reeve pinned first in the agents list: its session's
// state and time, agent tool and activity, this instance's name, and how
// many approvals wait. With no session running it stays, quieter, and
// picking it starts (or resumes) one.
export function ReeveEntry({ baseUrl, agent, selected, onSelect, compact }: {
  baseUrl: string
  agent?: AgentInfo
  selected: boolean
  onSelect: (taskId: string) => void
  compact?: boolean
}) {
  const overview = useReeveOverview(baseUrl, agent ? `${agent.task_id}:${agent.activity}` : "")
  const [starting, setStarting] = useState(false)
  const [error, setError] = useState("")
  const data: ReeveOverview | undefined = overview.data
  if (overview.isError || !data) return null
  const current = data.current
  const live = Boolean(agent)
  const pick = async () => {
    if (agent) {
      onSelect(agent.task_id)
      return
    }
    setStarting(true)
    setError("")
    try {
      const session = current?.state === "suspended" ? await resumeReeve(baseUrl, current.task_id) : await startReeve(baseUrl, {})
      overview.refetch()
      onSelect(session.task_id)
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    } finally {
      setStarting(false)
    }
  }
  const time = agent ? formatAgentTime(stateSeconds(agent)) : ""
  const state = starting ? "Starting…" : agent ? (agent.activity === "working" ? "Running" : agent.activity === "waiting" ? "Needs you" : "Idle") : current?.state === "suspended" ? "Suspended" : "Not running"
  return (
    <div data-testid="reeve-entry" className={cn("relative rounded-md", compact ? "" : "mb-1", reeveWash, selected && "shadow-[inset_0_0_0_1px] shadow-lw-accent-ring", !live && "opacity-70")}>
      <button type="button" onClick={() => void pick()} disabled={starting} aria-label={live ? "The Reeve" : "Start the Reeve"}
        className={cn("flex w-full min-w-0 flex-col gap-[3px] px-2.5 py-2 text-left", live ? "text-lw-text" : "text-lw-text-2")}>
        <span className="flex min-w-0 items-center gap-1.5">
          <Crown size={12} aria-hidden="true" className="shrink-0 text-[#a78bfa]" />
          <span className="font-semibold">Reeve</span>
          <span className="min-w-0 truncate text-xs text-lw-text-2">{data.instance}</span>
          {data.to_approve > 0 && (
            <span className="ml-auto shrink-0 rounded-full bg-lw-needs-soft px-1.5 text-[11px] text-lw-needs" title="Approvals the Reeve waits on">{data.to_approve} to approve</span>
          )}
        </span>
        <span className="flex min-w-0 items-center gap-1.5 text-[11px] text-lw-text-2">
          <span>{state}{time ? ` ${time}` : ""}</span>
          {agent && <AgentKindBadge kind={agent.agent_kind} isNative={!!agent.has_chimera_endpoint} />}
          {!live && <span className="text-lw-faint">· {current?.state === "suspended" ? "pick to resume" : "pick to start"}</span>}
        </span>
        {agent && !compact && <ActivityBar states={agent.state_timeline} />}
      </button>
      {!compact && <VoiceButton className="absolute right-1.5 bottom-1.5 rounded-full p-0.5 hover:bg-white/5" />}
      {error && <p role="alert" className="px-2.5 pb-2 text-[11px] text-lw-error">{error}</p>}
    </div>
  )
}

// ReeveStatus is what the conversation's status bar adds for the Reeve:
// that it runs in the sandbox, the instance with its role, and its engram.
export function ReeveStatus({ baseUrl }: { baseUrl: string }) {
  const overview = useReeveOverview(baseUrl)
  if (!overview.data) return null
  return (
    <span data-testid="reeve-status" className="flex min-w-0 items-center gap-1.5 truncate">
      <span className="rounded bg-[#8b5cf6]/20 px-1.5 text-[#c4b5fd]">Reeve</span>
      <span>sandboxed</span>
      <span aria-hidden="true" className="text-lw-faint">·</span>
      <span className="truncate">{overview.data.instance} ({overview.data.role})</span>
      <span aria-hidden="true" className="text-lw-faint">·</span>
      {overview.data.engram
        ? <span className="truncate" title="The Reeve's engram">{overview.data.engram}</span>
        : <span className="text-lw-faint">no engram</span>}
    </span>
  )
}
