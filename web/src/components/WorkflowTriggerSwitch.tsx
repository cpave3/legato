import { useState } from "react"
import { Link } from "react-router-dom"
import { setWorkflowTrigger, type WorkflowDefinition, type WorkflowTriggerState } from "../lib/workflow"
import { dueTimeText, triggerLabel } from "../lib/workflowView"
import { useServer } from "../hooks/useServer"
import { cn } from "../lib/utils"

// TriggerSwitch turns one of a workflow's triggers on or off in this Legato
// only; its file, which may say enabled: false, is never changed.
export function TriggerSwitch({ definition, state, onChanged }: { definition: WorkflowDefinition; state: WorkflowTriggerState; onChanged: () => Promise<unknown> | void }) {
  const { baseUrl } = useServer()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const label = triggerLabel(state.trigger, definition.on, state.schedule?.description)
  const toggle = async () => {
    setBusy(true)
    setError("")
    try {
      await setWorkflowTrigger(baseUrl, definition.name, state.trigger, !state.enabled)
      await onChanged()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    } finally {
      setBusy(false)
    }
  }
  return <span className="inline-flex items-center gap-2">
    <button type="button" role="switch" aria-checked={state.enabled} aria-label={label} disabled={busy} onClick={() => void toggle()}
      title={state.overridden ? `Switched here; the file says ${state.file_enabled ? "on" : "paused"}` : "As its file says"}
      className={cn("inline-flex min-h-8 items-center gap-2 rounded-full border px-2.5 text-xs", state.enabled ? "border-lw-done/50 text-lw-done" : "border-lw-line-strong text-lw-text-2")}>
      <span aria-hidden="true" className={cn("h-2 w-2 rounded-full", state.enabled ? "bg-lw-done" : "bg-lw-faint")} />
      {state.enabled ? "on here" : "paused here"}
    </button>
    {error && <span role="alert" className="text-xs text-lw-error">{error}</span>}
    {state.schedule && <ScheduleTimes title={definition.title} schedule={state.schedule} />}
  </span>
}

// ScheduleTimes is a schedule's next due times and what its last one did,
// each in the schedule's own zone.
export function ScheduleTimes({ title, schedule }: { title: string; schedule: NonNullable<WorkflowTriggerState["schedule"]> }) {
  return <span className="inline-flex flex-wrap items-center gap-x-3 text-xs text-lw-text-2">
    <span aria-label={`Next runs of ${title}`}><span className="text-lw-faint">Next</span> {schedule.next.map(dueTimeText).join(" · ") || "none"} <span className="text-lw-faint">{schedule.timezone}</span></span>
    {schedule.last && (schedule.last.run_id
      ? <Link to={`/workflows/runs/${encodeURIComponent(schedule.last.run_id)}`} className="hover:text-lw-text">Last run {dueTimeText(schedule.last.due_time)} · {schedule.last.status}</Link>
      : <span title={schedule.last.error}>Last {dueTimeText(schedule.last.due_time)} · {schedule.last.status}</span>)}
    {(schedule.missed ?? []).length > 0 && <span aria-label={`Missed runs of ${title}`} className="text-lw-needs">
      Missed {(schedule.missed ?? []).map(fire => <span key={fire.due_time} title={fire.error} className="ml-1">{dueTimeText(fire.due_time)}</span>)}
      <span className="ml-1 text-lw-faint">· {schedule.catch_up === "skip" ? "skipped" : "caught up once"}</span>
    </span>}
  </span>
}
