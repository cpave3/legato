import { useQueryClient } from "@tanstack/react-query"
import { useState } from "react"
import { AlertTriangle, LoaderCircle } from "lucide-react"
import { usePreparingAgents, useRepositoryCache } from "../hooks/usePreparingAgents"
import { useServer } from "../hooks/useServer"
import { agentKindLabel } from "../lib/agentKind"
import { describePreparing, dismissPreparingAgent } from "../lib/preparingAgents"
import { queryKeys } from "../lib/query"
import { cn } from "../lib/utils"

// PreparingAgents lists the agents the server is starting in the background:
// those whose clone is being made, with the repository's copy as it goes,
// and those that failed, which stay until dismissed. It shows nothing when
// there are none.
export function PreparingAgents({ className }: { className?: string }) {
  const { baseUrl } = useServer()
  const queryClient = useQueryClient()
  const { data: preparing = [] } = usePreparingAgents()
  const { data: cache = {} } = useRepositoryCache()
  const [error, setError] = useState("")

  if (preparing.length === 0) return null

  const dismiss = async (taskId: string) => {
    setError("")
    try {
      await dismissPreparingAgent(baseUrl, taskId)
      await queryClient.invalidateQueries({ queryKey: queryKeys.preparingAgents(baseUrl) })
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    }
  }

  return (
    <section className={cn("flex flex-col gap-0.5", className)}>
      <h2 className="m-0 px-2 pb-0.5 pt-2.5 text-[11px] font-semibold uppercase tracking-[1px] text-lw-text-2">
        Starting · {preparing.length}
      </h2>
      {error && <p role="alert" className="px-2 text-xs text-lw-error">{error}</p>}
      <ul className="m-0 flex list-none flex-col gap-0.5 p-0">
        {preparing.map((agent) => {
          const state = describePreparing(agent, agent.repo ? cache[agent.repo] : undefined)
          return (
            <li key={agent.task_id} aria-label={agent.title || agent.task_id} className="flex flex-col gap-1 rounded-md px-2 py-1.5">
              <div className="flex min-w-0 items-center gap-2">
                {state.failed
                  ? <AlertTriangle size={13} aria-hidden="true" className="shrink-0 text-lw-error" />
                  : <LoaderCircle size={13} aria-hidden="true" className="shrink-0 animate-spin text-lw-live motion-reduce:animate-none" />}
                <span className="min-w-0 flex-1 truncate font-medium">{agent.title || agent.task_id}</span>
                {agent.agent_kind && <span className="shrink-0 text-[11px] text-lw-faint">{agentKindLabel(agent.agent_kind)}</span>}
              </div>
              <p className={cn("m-0 break-words pl-[21px] text-xs", state.failed ? "text-lw-error" : "text-lw-text-2")}>{state.line}</p>
              {state.detail && <p className="m-0 truncate pl-[21px] font-lw-mono text-[11px] text-lw-faint">{state.detail}</p>}
              {state.percent != null && (
                <div
                  role="progressbar"
                  aria-label={state.line}
                  aria-valuemin={0}
                  aria-valuemax={100}
                  aria-valuenow={state.percent}
                  className="ml-[21px] h-1 overflow-hidden rounded-full bg-lw-line-strong"
                >
                  <div className="h-full rounded-full bg-lw-live transition-[width] duration-300 motion-reduce:transition-none" style={{ width: `${state.percent}%` }} />
                </div>
              )}
              {state.failed && (
                <button
                  type="button"
                  onClick={() => void dismiss(agent.task_id)}
                  className="ml-[21px] w-fit rounded px-1.5 py-0.5 text-xs text-lw-text-2 hover:bg-lw-raised hover:text-lw-text"
                >
                  Dismiss
                </button>
              )}
            </li>
          )
        })}
      </ul>
    </section>
  )
}
