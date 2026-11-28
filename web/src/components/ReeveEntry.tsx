import { useState } from "react"
import { Crown } from "lucide-react"
import type { AgentInfo } from "../lib/agentWire"
import { resumeReeve, startReeve, type ReeveOverview } from "../lib/reeve"
import { useReeveOverview } from "../hooks/useReeveOverview"
import { cn } from "../lib/utils"
import { AgentKindBadge } from "./AgentKindBadge"
import { ActivityBar } from "./AgentMeters"
import { formatAgentTime, stateSeconds } from "../lib/agentList"
import { VoiceButton } from "../voice/VoiceLauncher"
import { useReeveName } from "../lib/reeveName"

// The Reeve's violet: a wash from the left edge and a faint border, apart
// from the selection accent.
export const reeveWash = "bg-gradient-to-r from-[var(--theme-keyword,#8b5cf6)]/20 via-[var(--theme-keyword,#8b5cf6)]/[0.06] to-transparent shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--theme-keyword,#a78bfa)_28%,transparent)]"

// The word for the Reeve's state on its entry.
function reeveStateWord(starting: boolean, agent: AgentInfo | undefined, suspended: boolean): string {
  if (starting) return "Starting…"
  if (!agent) return suspended ? "Suspended" : "Not running"
  if (agent.activity === "working") return "Running"
  return agent.activity === "waiting" ? "Needs you" : "Idle"
}

// Starting a Reeve session, or resuming the suspended one, then selecting
// it; with why it failed.
function useStartReeve(baseUrl: string, current: ReeveOverview["current"], refetch: () => unknown, onSelect: (taskId: string) => void) {
  const [starting, setStarting] = useState(false)
  const [error, setError] = useState("")
  const start = async () => {
    setStarting(true)
    setError("")
    try {
      const session = current?.state === "suspended" ? await resumeReeve(baseUrl, current.task_id) : await startReeve(baseUrl, {})
      refetch()
      onSelect(session.task_id)
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    } finally {
      setStarting(false)
    }
  }
  return { starting, error, start }
}

// The state and its time, the agent tool, and with no session how picking
// it would start one.
function ReeveStateLine({ word, agent, suspended }: { word: string; agent?: AgentInfo; suspended: boolean }) {
  const time = agent ? formatAgentTime(stateSeconds(agent)) : ""
  return (
    <span className="flex min-w-0 items-center gap-1.5 text-[11px] text-lw-text-2">
      <span>{word}{time ? ` ${time}` : ""}</span>
      {agent && <AgentKindBadge kind={agent.agent_kind} isNative={!!agent.has_chimera_endpoint} />}
      {!agent && <span className="text-lw-faint">· {suspended ? "pick to resume" : "pick to start"}</span>}
    </span>
  )
}

// The first line: the Reeve, this instance, and how many approvals wait.
function ReeveHeadline({ instance, toApprove }: { instance: string; toApprove: number }) {
  const reeve = useReeveName()
  return (
    <span className="flex min-w-0 items-center gap-1.5">
      <Crown size={12} aria-hidden="true" className="shrink-0 text-[var(--theme-keyword,#a78bfa)]" />
      <span className="font-semibold">{reeve.label}</span>
      <span className="min-w-0 truncate text-xs text-lw-text-2">{instance}</span>
      {toApprove > 0 && (
        <span className="ml-auto shrink-0 rounded-full bg-lw-needs-soft px-1.5 text-[11px] text-lw-needs" title={`Approvals ${reeve.mid} waits on`}>{toApprove} to approve</span>
      )}
    </span>
  )
}

// The entry as drawn: quieter with no session, ringed when selected.
function ReeveCard({ data, agent, selected, compact, starting, error, onPick }: {
  data: ReeveOverview
  agent?: AgentInfo
  selected: boolean
  compact?: boolean
  starting: boolean
  error: string
  onPick: () => void
}) {
  const reeve = useReeveName()
  const suspended = data.current?.state === "suspended"
  const live = Boolean(agent)
  return (
    <div data-testid="reeve-entry" className={cn("relative rounded-md", compact ? "" : "mb-1", reeveWash, selected && "shadow-[inset_0_0_0_1px] shadow-lw-accent-ring", !live && "opacity-70")}>
      <button type="button" onClick={onPick} disabled={starting} aria-label={live ? reeve.start : `Start ${reeve.mid}`}
        className={cn("flex w-full min-w-0 flex-col gap-[3px] px-2.5 py-2 text-left", live ? "text-lw-text" : "text-lw-text-2")}>
        <ReeveHeadline instance={data.instance} toApprove={data.to_approve} />
        <ReeveStateLine word={reeveStateWord(starting, agent, suspended)} agent={agent} suspended={suspended} />
        {agent && !compact && <ActivityBar states={agent.state_timeline} />}
      </button>
      <VoiceButton className={cn("absolute right-1.5 rounded-full p-0.5 hover:bg-white/5", compact ? "top-1/2 -translate-y-1/2" : "bottom-1.5")} />
      <StartError error={error} />
    </div>
  )
}

// Why the Reeve could not be started.
function StartError({ error }: { error: string }) {
  if (!error) return null
  return <p role="alert" className="px-2.5 pb-2 text-[11px] text-lw-error">{error}</p>
}

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
  const data: ReeveOverview | undefined = overview.data
  const { starting, error, start } = useStartReeve(baseUrl, data?.current, overview.refetch, onSelect)
  if (overview.isError || !data) return null
  const pick = () => agent ? onSelect(agent.task_id) : void start()
  return <ReeveCard data={data} agent={agent} selected={selected} compact={compact} starting={starting} error={error} onPick={pick} />
}

// ReeveStatus is what the conversation's status bar adds for the Reeve:
// that it runs in the sandbox, the instance with its role, and its engram.
export function ReeveStatus({ baseUrl }: { baseUrl: string }) {
  const overview = useReeveOverview(baseUrl)
  const reeve = useReeveName()
  if (!overview.data) return null
  return (
    <span data-testid="reeve-status" className="flex min-w-0 items-center gap-1.5 truncate">
      <span className="rounded bg-[var(--theme-selection,rgba(139,92,246,0.2))] px-1.5 text-[var(--theme-keyword,#c4b5fd)]">{reeve.label}</span>
      <span>sandboxed</span>
      <span aria-hidden="true" className="text-lw-faint">·</span>
      <span className="truncate">{overview.data.instance} ({overview.data.role})</span>
      <span aria-hidden="true" className="text-lw-faint">·</span>
      {overview.data.engram
        ? <span className="truncate" title={`${reeve.start}'s engram`}>{overview.data.engram}</span>
        : <span className="text-lw-faint">no engram</span>}
    </span>
  )
}
