import { useState, useEffect, useCallback, useRef } from "react"
import { useWebSocket } from "../hooks/useWebSocket"
import { useServer } from "../hooks/useServer"
import { useToast } from "../hooks/useToast"
import { getPendingPlan, type PendingPlanData } from "../lib/swarm"
import { X, CheckCircle, XCircle } from "lucide-react"

export function PlanApprovalModal({ parentIds }: { parentIds: string[] }) {
  const { subscribe } = useWebSocket()
  const { baseUrl } = useServer()
  const { addToast } = useToast()
  const [plan, setPlan] = useState<PendingPlanData | null>(null)
  const [isOpen, setIsOpen] = useState(false)
  const [rejectMode, setRejectMode] = useState(false)
  const [notes, setNotes] = useState("")
  const [sending, setSending] = useState(false)
  const [verdictError, setVerdictError] = useState("")
  const { send } = useWebSocket()
  const isVerdictedRef = useRef(false)

  const showPlan = useCallback((p: PendingPlanData) => {
    if (isVerdictedRef.current) return
    setPlan(p)
    setIsOpen(true)
    setRejectMode(false)
    setNotes("")
    setVerdictError("")
  }, [])

  // Listen for incoming plan_proposed messages.
  useEffect(() => {
    return subscribe((msg) => {
      if (msg.type === "plan_proposed" && msg.parent_task_id && parentIds.includes(msg.parent_task_id)) {
        getPendingPlan(baseUrl, msg.parent_task_id).then((fullPlan) => {
          if (fullPlan) showPlan(fullPlan)
        })
      }
    })
  }, [subscribe, parentIds, baseUrl, showPlan])

  // On reconnect, check for pending plans for all active parents.
  const connectedRef = useRef(true)
  const { connected } = useWebSocket()
  useEffect(() => {
    if (connected && !connectedRef.current) {
      // Just reconnected — poll for pending plans.
      parentIds.forEach((pid) => {
        getPendingPlan(baseUrl, pid).then((p) => {
          if (p) showPlan(p)
        })
      })
    }
    connectedRef.current = connected
  }, [connected, parentIds, baseUrl, showPlan])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && isOpen) {
        handleClose()
      }
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [isOpen])

  const handleClose = () => {
    setIsOpen(false)
    setRejectMode(false)
    setNotes("")
    setVerdictError("")
  }

  const sendVerdict = async (status: "approved" | "rejected", verdictNotes?: string) => {
    if (!plan) return
    setSending(true)
    setVerdictError("")

    const msg = {
      type: "plan_verdict" as const,
      parent_task_id: plan.parent_task_id,
      plan_path: plan.plan_path,
      reply_socket: plan.reply_socket,
      status,
      notes: verdictNotes ?? "",
    }

    try {
      send(msg)
      isVerdictedRef.current = true
      setIsOpen(false)
      setPlan(null)
      addToast(status === "approved" ? "Plan approved" : "Plan rejected with notes", status === "approved" ? "success" : "error")
      setTimeout(() => { isVerdictedRef.current = false }, 5000)
    } catch {
      setVerdictError("Failed to send verdict. Please try again.")
    } finally {
      setSending(false)
    }
  }

  const handleApprove = () => sendVerdict("approved")
  const handleReject = () => {
    if (!rejectMode) {
      setRejectMode(true)
      return
    }
    if (!notes.trim()) {
      setVerdictError("Please provide notes for the rejection.")
      return
    }
    sendVerdict("rejected", notes.trim())
  }

  if (!isOpen || !plan) return null

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
      <div className="max-h-[80vh] w-full max-w-lg overflow-y-auto rounded-lg border border-zinc-700 bg-zinc-900 shadow-2xl">
        <div className="flex items-center justify-between border-b border-zinc-800 px-5 py-3">
          <h2 className="text-sm font-semibold text-zinc-200">Plan Proposed</h2>
          <button
            onClick={handleClose}
            className="rounded p-1 text-zinc-500 transition-colors hover:bg-zinc-800 hover:text-zinc-200"
          >
            <X size={16} />
          </button>
        </div>

        <div className="px-5 py-4 space-y-4">
          {verdictError && (
            <div className="rounded border border-red-800 bg-red-950/50 px-3 py-2 text-xs text-red-300">
              {verdictError}
            </div>
          )}

          <div className="space-y-1">
            <div className="text-xs text-zinc-500">Parent Task</div>
            <div className="text-sm font-mono text-zinc-300">{plan.parent_task_id}</div>
          </div>

          <div className="space-y-1">
            <div className="text-xs text-zinc-500">Plan Path</div>
            <div className="text-sm font-mono text-zinc-300">{plan.plan_path}</div>
          </div>

          {rejectMode && (
            <div className="space-y-2">
              <label className="text-xs text-zinc-500">Rejection Notes</label>
              <textarea
                value={notes}
                onChange={(e) => setNotes(e.target.value)}
                placeholder="Describe what needs to change..."
                rows={3}
                className="w-full rounded border border-zinc-700 bg-zinc-950 px-3 py-2 text-sm text-zinc-200 outline-none focus:border-indigo-500 resize-none"
                autoFocus
              />
            </div>
          )}
        </div>

        <div className="flex items-center justify-end gap-2 border-t border-zinc-800 px-5 py-3">
          {!rejectMode ? (
            <>
              <button
                onClick={handleClose}
                className="rounded px-3 py-1.5 text-xs text-zinc-400 transition-colors hover:bg-zinc-800 hover:text-zinc-200"
              >
                Dismiss
              </button>
              <button
                onClick={handleReject}
                className="flex items-center gap-1.5 rounded border border-red-900 bg-red-950 px-3 py-1.5 text-xs text-red-400 transition-colors hover:bg-red-900 hover:text-red-300"
              >
                <XCircle size={14} />
                Reject
              </button>
              <button
                onClick={handleApprove}
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
                onClick={() => { setRejectMode(false); setVerdictError("") }}
                className="rounded px-3 py-1.5 text-xs text-zinc-400 transition-colors hover:bg-zinc-800 hover:text-zinc-200"
              >
                Back
              </button>
              <button
                onClick={handleReject}
                disabled={sending}
                className="flex items-center gap-1.5 rounded bg-red-600 px-3 py-1.5 text-xs text-white transition-colors hover:bg-red-500 disabled:opacity-40"
              >
                <XCircle size={14} />
                {sending ? "Sending..." : "Reject with Notes"}
              </button>
            </>
          )}
        </div>
      </div>
    </div>
  )
}
