import { useState } from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { Crown } from "lucide-react"
import { deleteReeve, endReeve, fetchReeve, resumeReeve, startReeve, type ReeveSession } from "../../lib/reeve"
import { queryKeys } from "../../lib/query"
import { SettingsCard, primaryButton, quietButton } from "./SettingsUI"

const stateLabel: Record<ReeveSession["state"], string> = { running: "Running", suspended: "Suspended", ended: "Ended" }

// ReeveSessions starts the Reeve (with an optional override of its agent
// tool and model), ends its session, and lists its history.
export function ReeveSessions({ baseUrl }: { baseUrl: string }) {
  const queryClient = useQueryClient()
  const overview = useQuery({ queryKey: queryKeys.reeve(baseUrl), queryFn: () => fetchReeve(baseUrl), retry: false })
  const [agent, setAgent] = useState("")
  const [model, setModel] = useState("")
  const [busy, setBusy] = useState("")
  const [error, setError] = useState("")
  const run = async (what: string, action: () => Promise<unknown>) => {
    setBusy(what)
    setError("")
    try {
      await action()
      await queryClient.invalidateQueries({ queryKey: queryKeys.reeve(baseUrl) })
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    } finally {
      setBusy("")
    }
  }
  if (overview.isError) {
    return <SettingsCard icon={Crown} title="The Reeve" description="This server has no Reeve: it is older, or runs without tmux." />
  }
  const data = overview.data
  if (!data) return null
  const current = data.current
  const past = data.history.filter((s) => s.task_id !== current?.task_id)
  return (
    <SettingsCard icon={Crown} title="The Reeve"
      description={<>The agent of this Legato itself. It runs in Legato's sandbox, in <code>{data.folder}</code>, and reads freely; every change it proposes comes to you to approve.</>}>
      {current ? (
        <div className="flex flex-wrap items-center gap-2" data-testid="reeve-current">
          <span className="text-sm text-lw-text">{stateLabel[current.state]} · {current.agent_kind || "default tool"}{current.model ? ` · ${current.model}` : ""}</span>
          <button type="button" className={quietButton} disabled={busy !== ""} onClick={() => void run("end", () => endReeve(baseUrl, current.task_id))}>End session</button>
        </div>
      ) : (
        <div className="flex flex-wrap items-end gap-2">
          <label className="flex flex-col gap-1 text-xs text-lw-text-2">Agent tool (this session)
            <input aria-label="Agent tool for this session" placeholder="the default" value={agent} onChange={(e) => setAgent(e.target.value)}
              className="min-h-9 w-40 rounded-lg border border-lw-line bg-lw-ground px-2 text-sm text-lw-text" />
          </label>
          <label className="flex flex-col gap-1 text-xs text-lw-text-2">Model (this session)
            <input aria-label="Model for this session" placeholder="the default" value={model} onChange={(e) => setModel(e.target.value)}
              className="min-h-9 w-40 rounded-lg border border-lw-line bg-lw-ground px-2 text-sm text-lw-text" />
          </label>
          <button type="button" className={primaryButton} disabled={busy !== ""}
            onClick={() => void run("start", () => startReeve(baseUrl, { agent: agent.trim() || undefined, model: model.trim() || undefined }))}>
            Start the Reeve
          </button>
        </div>
      )}
      {error && <p role="alert" className="mt-2 text-xs text-lw-error">{error}</p>}
      {past.length > 0 && (
        <section className="mt-4 border-t border-lw-line pt-3" aria-label="The Reeve's history">
          <h3 className="text-sm text-lw-text">History</h3>
          <ul className="mt-1 flex flex-col divide-y divide-lw-line">
            {past.map((s) => (
              <li key={s.task_id} className="flex flex-wrap items-center gap-2 py-2 text-sm" data-testid={`reeve-session-${s.task_id}`}>
                <span className="text-lw-text">{new Date(s.created_at).toLocaleString()}</span>
                <span className="text-xs text-lw-text-2">{s.agent_kind || "default tool"} · {stateLabel[s.state]}</span>
                <span className="ml-auto flex gap-1">
                  <button type="button" className={quietButton} disabled={busy !== "" || Boolean(current)}
                    onClick={() => void run(`resume:${s.task_id}`, () => resumeReeve(baseUrl, s.task_id))}>Resume</button>
                  <button type="button" className={quietButton} disabled={busy !== ""}
                    onClick={() => void run(`delete:${s.task_id}`, () => deleteReeve(baseUrl, s.task_id))}>Delete</button>
                </span>
              </li>
            ))}
          </ul>
        </section>
      )}
    </SettingsCard>
  )
}
