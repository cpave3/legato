import { Dialog, DialogPanel, DialogHeader, DialogTitle, DialogBody, DialogFooter } from "./ui/Dialog"
import type { PendingPlanSubtask } from "../lib/swarm"
import { X, CheckCircle, XCircle, FileText, Users, HardDrive } from "lucide-react"
import { usePendingPlan, useVerdictForm } from "./usePlanApproval"

export function PlanApprovalModal() {
  const form = useVerdictForm()
  const { plan, isOpen, isExtension, close, markVerdicted } = usePendingPlan(form.reset)

  if (!isOpen || !plan) return null

  const handleClose = () => {
    close()
    form.reset()
  }

  return (
    <Dialog open={isOpen} onClose={handleClose}>
      <DialogPanel className="w-full max-w-xl rounded-lg border border-zinc-700 bg-zinc-900 shadow-2xl">
        <DialogHeader className="flex items-center justify-between border-b border-zinc-800 px-5 py-3">
          <DialogTitle className="text-sm font-semibold text-zinc-200">
            {isExtension ? "Extension plan — append to existing swarm" : "Plan Proposed"}
          </DialogTitle>
          <button aria-label="Close"
            onClick={handleClose}
            className="rounded p-1 text-zinc-500 transition-colors hover:bg-zinc-800 hover:text-zinc-200"
          >
            <X size={16} />
          </button>
        </DialogHeader>

        <DialogBody className="px-5 py-4 space-y-4">
          {form.verdictError && (
            <div role="alert" className="rounded border border-red-800 bg-red-950/50 px-3 py-2 text-xs text-red-300">
              {form.verdictError}
            </div>
          )}

          {/* Parent task */}
          <div className="space-y-1">
            <div className="text-xs text-zinc-500">Parent Task</div>
            <div className="text-sm font-mono text-zinc-300">{plan.parent_task_id}</div>
          </div>

          {/* What the plan proposes, or why it cannot be shown. */}
          {plan.plan ? (
            <>
              <div className="space-y-1">
                <div className="flex items-center gap-1.5 text-xs text-zinc-500">
                  <HardDrive size={12} />
                  Working Directory
                </div>
                <div className="text-sm font-mono text-zinc-300">{plan.plan.header.working_dir}</div>
              </div>

              {plan.plan.header.summary && (
                <div className="space-y-1">
                  <div className="flex items-center gap-1.5 text-xs text-zinc-500">
                    <FileText size={12} />
                    Summary
                  </div>
                  <div className="text-sm text-zinc-300 whitespace-pre-wrap">{plan.plan.header.summary}</div>
                </div>
              )}

              <div className="space-y-2">
                <div className="flex items-center gap-1.5 text-xs text-zinc-400 font-medium">
                  <Users size={12} />
                  Sub-tasks ({plan.plan.subtasks.length})
                </div>
                <div className="space-y-2">
                  {plan.plan.subtasks.map((st, i) => (
                    <SubtaskRow key={i} subtask={st} index={i} />
                  ))}
                </div>
              </div>
            </>
          ) : plan.load_error ? (
            <div role="alert" className="rounded border border-red-900/50 bg-red-950/30 px-3 py-2 text-xs text-red-400">
              Could not load plan: {plan.load_error}
              <div className="mt-1 text-zinc-500">Path: {plan.plan_path}</div>
            </div>
          ) : (
            <div className="text-xs text-zinc-500">
              Plan path: <span className="font-mono text-zinc-400">{plan.plan_path}</span>
            </div>
          )}

          {form.rejectMode && (
            <div className="space-y-2">
              <label htmlFor="plan-rejection-notes" className="text-xs text-zinc-500">Rejection Notes</label>
              <textarea
                id="plan-rejection-notes"
                value={form.notes}
                onChange={(e) => form.setNotes(e.target.value)}
                placeholder="Describe what needs to change..."
                rows={3}
                className="w-full rounded border border-zinc-700 bg-zinc-950 px-3 py-2 text-sm text-zinc-200 outline-none focus:border-indigo-500 resize-none"
                autoFocus
              />
            </div>
          )}
        </DialogBody>

        <VerdictFooter
          rejectMode={form.rejectMode}
          sending={form.sending}
          onDismiss={handleClose}
          onApprove={() => form.approve(plan, markVerdicted)}
          onReject={() => form.reject(plan, markVerdicted)}
          onBack={form.back}
        />
      </DialogPanel>
    </Dialog>
  )
}

function SubtaskRow({ subtask, index }: { subtask: PendingPlanSubtask; index: number }) {
  return (
    <div className="rounded border border-zinc-800 bg-zinc-950/60 px-3 py-2.5">
      <div className="flex items-center gap-2">
        <span className="text-xs font-mono text-zinc-500">{index + 1}.</span>
        <span className="text-sm font-medium text-zinc-200">{subtask.title}</span>
      </div>
      <div className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-[11px] text-zinc-500">
        {subtask.role && (
          <span>role: <span className="text-zinc-400">{subtask.role}</span></span>
        )}
        {subtask.agent && (
          <span>agent: <span className="text-zinc-400">{subtask.agent}</span></span>
        )}
        {subtask.tier && (
          <span>tier: <span className="text-zinc-400">{subtask.tier}</span></span>
        )}
        {subtask.scope && subtask.scope.length > 0 && (
          <span>scope: <span className="text-zinc-400">{subtask.scope.join(", ")}</span></span>
        )}
      </div>
      {subtask.prompt && (
        <div className="mt-1.5 text-xs text-zinc-500 line-clamp-2">
          {subtask.prompt}
        </div>
      )}
    </div>
  )
}

interface VerdictFooterProps {
  rejectMode: boolean
  sending: boolean
  onDismiss: () => void
  onApprove: () => void
  onReject: () => void
  onBack: () => void
}

// Dismiss, Reject and Approve; or, while writing rejection notes, Back and
// Reject with Notes.
function VerdictFooter({ rejectMode, sending, onDismiss, onApprove, onReject, onBack }: VerdictFooterProps) {
  return (
    <DialogFooter className="flex items-center justify-end gap-2 border-t border-zinc-800 px-5 py-3">
      {!rejectMode ? (
        <>
          <button
            onClick={onDismiss}
            className="rounded px-3 py-1.5 text-xs text-zinc-400 transition-colors hover:bg-zinc-800 hover:text-zinc-200"
          >
            Dismiss
          </button>
          <button
            onClick={onReject}
            className="flex items-center gap-1.5 rounded border border-red-900 bg-red-950 px-3 py-1.5 text-xs text-red-400 transition-colors hover:bg-red-900 hover:text-red-300"
          >
            <XCircle size={14} />
            Reject
          </button>
          <button
            onClick={onApprove}
            disabled={sending}
            className="flex items-center gap-1.5 rounded bg-emerald-600 px-3 py-1.5 text-xs text-white transition-colors hover:bg-emerald-500 disabled:opacity-40"
          >
            <CheckCircle size={14} />
            {sending ? "Sending..." : "Approve"}
          </button>
        </>
      ) : (
        <>
          <button
            onClick={onBack}
            className="rounded px-3 py-1.5 text-xs text-zinc-400 transition-colors hover:bg-zinc-800 hover:text-zinc-200"
          >
            Back
          </button>
          <button
            onClick={onReject}
            disabled={sending}
            className="flex items-center gap-1.5 rounded bg-red-600 px-3 py-1.5 text-xs text-white transition-colors hover:bg-red-500 disabled:opacity-40"
          >
            <XCircle size={14} />
            {sending ? "Sending..." : "Reject with Notes"}
          </button>
        </>
      )}
    </DialogFooter>
  )
}
