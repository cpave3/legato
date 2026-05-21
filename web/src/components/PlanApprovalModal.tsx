import { useState, useEffect, useCallback, useRef } from "react"
import { useWebSocket } from "../hooks/useWebSocket"
import { useServer } from "../hooks/useServer"
import { useToast } from "../hooks/useToast"
import { getAllPendingPlans, type PendingPlanData } from "../lib/swarm"
import { X, CheckCircle, XCircle, FileText, Users, HardDrive } from "lucide-react"

export function PlanApprovalModal() {
  const { subscribe, send, connected } = useWebSocket()
  const { baseUrl } = useServer()
  const { addToast } = useToast()
  const [plan, setPlan] = useState<PendingPlanData | null>(null)
  const [isOpen, setIsOpen] = useState(false)
  const [rejectMode, setRejectMode] = useState(false)
  const [notes, setNotes] = useState("")
  const [sending, setSending] = useState(false)
  const [verdictError, setVerdictError] = useState("")
  const isVerdictedRef = useRef(false)
  const connectedRef = useRef(true)

  // currentPlanIdRef mirrors the displayed plan's parent_task_id without
  // creating a render dependency. Used by discoverPlans to skip re-showing
  // the plan we're already reviewing — otherwise a fresh plan_proposed event
  // for the same parent would reset rejectMode/notes mid-typing.
  const currentPlanIdRef = useRef<string | null>(null)
  const [isExtension, setIsExtension] = useState(false)
  const pendingModeRef = useRef<string | null>(null)

  const showPlan = useCallback((p: PendingPlanData) => {
    if (isVerdictedRef.current) return
    setPlan(p)
    currentPlanIdRef.current = p.parent_task_id
    setIsExtension(pendingModeRef.current === "extension")
    pendingModeRef.current = null
    setIsOpen(true)
    setRejectMode(false)
    setNotes("")
    setVerdictError("")
  }, [])

  const discoverPlans = useCallback(async () => {
    try {
      const plans = await getAllPendingPlans(baseUrl)
      if (plans.length === 0 || isVerdictedRef.current) return
      // Same plan we're already showing — leave rejectMode/notes alone.
      if (currentPlanIdRef.current === plans[0].parent_task_id) return
      // Show the oldest plan first
      showPlan(plans[0])
    } catch {
      // ignore discovery errors
    }
  }, [baseUrl, showPlan])

  // Discovery: on mount and every reconnect. Gate on baseUrl so the modal
  // doesn't fire a spurious request before the server URL is configured.
  useEffect(() => {
    if (baseUrl) discoverPlans()
  }, [baseUrl, discoverPlans])

  useEffect(() => {
    if (connected && !connectedRef.current) {
      // Just reconnected — poll for any plans that arrived while offline
      discoverPlans()
    }
    connectedRef.current = connected
  }, [connected, discoverPlans])

  // Listen for incoming plan_proposed and swarm_changed (cross-surface dismissal)
  useEffect(() => {
    return subscribe((msg) => {
      if (msg.type === "plan_proposed" && msg.parent_task_id) {
        pendingModeRef.current = msg.mode ?? null
        discoverPlans()
      }
      if (msg.type === "swarm_changed" && msg.parent_task_id) {
        if (
          plan?.parent_task_id === msg.parent_task_id &&
          (msg.status === "plan_applied" || msg.status === "rejected")
        ) {
          setIsOpen(false)
          setPlan(null)
          currentPlanIdRef.current = null
          isVerdictedRef.current = true
          const verb = msg.status === "plan_applied" ? "approved" : "rejected"
          addToast(`Plan ${verb} on another surface`, "info")
          setTimeout(() => { isVerdictedRef.current = false }, 5000)
        }
      }
    })
  }, [subscribe, plan, discoverPlans, addToast])

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
    // Dismissal is local only — the conductor is still blocked waiting
    // for a verdict. The persistent plan stays in the DB so the user
    // can rediscover it later.
    setIsOpen(false)
    setRejectMode(false)
    setNotes("")
    setVerdictError("")
    currentPlanIdRef.current = null
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
      currentPlanIdRef.current = null
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

  const planContent = plan.plan

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
      <div className="max-h-[85vh] w-full max-w-xl overflow-y-auto rounded-lg border border-zinc-700 bg-zinc-900 shadow-2xl">
        <div className="flex items-center justify-between border-b border-zinc-800 px-5 py-3">
          <h2 className="text-sm font-semibold text-zinc-200">
            {isExtension ? "Extension plan — append to existing swarm" : "Plan Proposed"}
          </h2>
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

          {/* Parent task */}
          <div className="space-y-1">
            <div className="text-xs text-zinc-500">Parent Task</div>
            <div className="text-sm font-mono text-zinc-300">{plan.parent_task_id}</div>
          </div>

          {/* Plan content */}
          {planContent ? (
            <>
              <div className="space-y-1">
                <div className="flex items-center gap-1.5 text-xs text-zinc-500">
                  <HardDrive size={12} />
                  Working Directory
                </div>
                <div className="text-sm font-mono text-zinc-300">{planContent.header.working_dir}</div>
              </div>

              {planContent.header.summary && (
                <div className="space-y-1">
                  <div className="flex items-center gap-1.5 text-xs text-zinc-500">
                    <FileText size={12} />
                    Summary
                  </div>
                  <div className="text-sm text-zinc-300 whitespace-pre-wrap">{planContent.header.summary}</div>
                </div>
              )}

              <div className="space-y-2">
                <div className="flex items-center gap-1.5 text-xs text-zinc-400 font-medium">
                  <Users size={12} />
                  Sub-tasks ({planContent.subtasks.length})
                </div>
                <div className="space-y-2">
                  {planContent.subtasks.map((st, i) => (
                    <div key={i} className="rounded border border-zinc-800 bg-zinc-950/60 px-3 py-2.5">
                      <div className="flex items-center gap-2">
                        <span className="text-xs font-mono text-zinc-500">{i + 1}.</span>
                        <span className="text-sm font-medium text-zinc-200">{st.title}</span>
                      </div>
                      <div className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-[11px] text-zinc-500">
                        {st.role && (
                          <span>role: <span className="text-zinc-400">{st.role}</span></span>
                        )}
                        {st.agent && (
                          <span>agent: <span className="text-zinc-400">{st.agent}</span></span>
                        )}
                        {st.tier && (
                          <span>tier: <span className="text-zinc-400">{st.tier}</span></span>
                        )}
                        {st.scope && st.scope.length > 0 && (
                          <span>scope: <span className="text-zinc-400">{st.scope.join(", ")}</span></span>
                        )}
                      </div>
                      {st.prompt && (
                        <div className="mt-1.5 text-xs text-zinc-500 line-clamp-2">
                          {st.prompt}
                        </div>
                      )}
                    </div>
                  ))}
                </div>
              </div>
            </>
          ) : plan.load_error ? (
            <div className="rounded border border-red-900/50 bg-red-950/30 px-3 py-2 text-xs text-red-400">
              Could not load plan: {plan.load_error}
              <div className="mt-1 text-zinc-500">Path: {plan.plan_path}</div>
            </div>
          ) : (
            <div className="text-xs text-zinc-500">
              Plan path: <span className="font-mono text-zinc-400">{plan.plan_path}</span>
            </div>
          )}

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
