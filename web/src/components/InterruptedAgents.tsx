import { useQuery, useQueryClient } from "@tanstack/react-query"
import { useState } from "react"
import { CirclePause } from "lucide-react"
import { useServer } from "../hooks/useServer"
import { agentKindLabel } from "../lib/agentKind"
import {
  dismissInterruptedAgent,
  fetchInterruptedAgents,
  resumeAllInterruptedAgents,
  resumeInterruptedAgent,
} from "../lib/interruptedAgents"
import { queryKeys } from "../lib/query"
import { cn } from "../lib/utils"

// InterruptedAgents lists the agents a crash interrupted: running when
// Legato last stopped, with their tmux session gone when it started again.
// Each can be resumed where it was, or, when it never reported a
// conversation, replaced by a new agent on its task; or dismissed. It shows
// nothing when there are none.
export function InterruptedAgents({ className }: { className?: string }) {
  const { baseUrl } = useServer()
  const queryClient = useQueryClient()
  const { data: interrupted = [] } = useQuery({
    queryKey: queryKeys.interruptedAgents(baseUrl),
    queryFn: () => fetchInterruptedAgents(baseUrl),
  })
  const [busy, setBusy] = useState<string | null>(null)
  const [error, setError] = useState("")

  if (interrupted.length === 0) return null

  const act = async (key: string, action: () => Promise<unknown>) => {
    setError("")
    setBusy(key)
    try {
      await action()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    } finally {
      setBusy(null)
      await queryClient.invalidateQueries({ queryKey: queryKeys.interruptedAgents(baseUrl) })
    }
  }

  const resumeAll = () => act("all", async () => {
    const failed = await resumeAllInterruptedAgents(baseUrl)
    if (failed.length > 0) throw new Error(failed.map((f) => `${f.task_id}: ${f.error}`).join("\n"))
  })

  const resumable = interrupted.filter((a) => a.resumable).length

  return (
    <section aria-label="Interrupted agents" className={cn("flex flex-col gap-0.5", className)}>
      <div className="flex items-center justify-between gap-2 px-2 pb-0.5 pt-2.5">
        <h2 className="m-0 text-[11px] font-semibold uppercase tracking-[1px] text-lw-needs">
          Interrupted · {interrupted.length}
        </h2>
        {resumable > 0 && interrupted.length > 1 && (
          <button
            type="button"
            disabled={busy !== null}
            onClick={() => void resumeAll()}
            className="rounded px-1.5 py-0.5 text-xs text-lw-link hover:bg-lw-raised disabled:opacity-50"
          >
            Resume all
          </button>
        )}
      </div>
      {error && <p role="alert" className="m-0 whitespace-pre-line break-words px-2 text-xs text-lw-error">{error}</p>}
      <ul className="m-0 flex list-none flex-col gap-0.5 p-0">
        {interrupted.map((agent) => {
          const name = agent.title || agent.task_id
          return (
            <li key={agent.task_id} aria-label={name} className="flex flex-col gap-1 rounded-md px-2 py-1.5">
              <div className="flex min-w-0 items-center gap-2">
                <CirclePause size={13} aria-hidden="true" className="shrink-0 text-lw-needs" />
                <span className="min-w-0 flex-1 truncate font-medium">{name}</span>
                {agent.agent_kind && <span className="shrink-0 text-[11px] text-lw-faint">{agentKindLabel(agent.agent_kind)}</span>}
              </div>
              {agent.working_dir && <p className="m-0 truncate pl-[21px] font-lw-mono text-[11px] text-lw-faint" title={agent.working_dir}>{agent.working_dir}</p>}
              {agent.reason && <p className="m-0 break-words pl-[21px] text-xs text-lw-text-2">Cannot resume: {agent.reason}</p>}
              <div className="flex gap-1 pl-[17px]">
                {agent.resumable && (
                  <button
                    type="button"
                    disabled={busy !== null}
                    onClick={() => void act(agent.task_id, () => resumeInterruptedAgent(baseUrl, agent.task_id))}
                    className="rounded px-1.5 py-0.5 text-xs text-lw-link hover:bg-lw-raised disabled:opacity-50"
                  >
                    Resume
                  </button>
                )}
                {!agent.resumable && agent.can_start_new && (
                  <button
                    type="button"
                    disabled={busy !== null}
                    onClick={() => void act(agent.task_id, () => resumeInterruptedAgent(baseUrl, agent.task_id, true))}
                    title="Start a new agent on this task, as it was set up, without its conversation"
                    className="rounded px-1.5 py-0.5 text-xs text-lw-link hover:bg-lw-raised disabled:opacity-50"
                  >
                    Start new
                  </button>
                )}
                <button
                  type="button"
                  disabled={busy !== null}
                  onClick={() => void act(agent.task_id, () => dismissInterruptedAgent(baseUrl, agent.task_id))}
                  className="rounded px-1.5 py-0.5 text-xs text-lw-text-2 hover:bg-lw-raised hover:text-lw-text disabled:opacity-50"
                >
                  Dismiss
                </button>
              </div>
            </li>
          )
        })}
      </ul>
    </section>
  )
}
