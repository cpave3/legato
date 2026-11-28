import { useCallback, useEffect, useRef, useState } from "react"
import { useWebSocket } from "../hooks/useWebSocket"
import { useServer } from "../hooks/useServer"
import { useToast } from "../hooks/useToast"
import { getAllPendingPlans, type PendingPlanData } from "../lib/swarm"
import { newPendingPlan, otherSurfaceVerdict, planVerdictMessage, verdictToast, type PlanVerdict } from "../lib/planVerdict"

// After a verdict, a plan is not shown again for a while: the conductor may
// propose the same plan once more before it has read the verdict.
const VERDICT_QUIET_MS = 5000

// Discovery: on mount and every reconnect. Gate on baseUrl so the modal
// doesn't fire a spurious request before the server URL is configured.
function useRediscover(baseUrl: string, connected: boolean, discoverPlans: () => void) {
  const connectedRef = useRef(true)
  useEffect(() => {
    if (baseUrl) discoverPlans()
  }, [baseUrl, discoverPlans])

  useEffect(() => {
    // Just reconnected — poll for any plans that arrived while offline
    if (connected && !connectedRef.current) discoverPlans()
    connectedRef.current = connected
  }, [connected, discoverPlans])
}

// usePendingPlan is the swarm plan waiting for a verdict: found on mount and
// on every reconnect, and when a conductor proposes one; closed when the
// person dismisses it, sends a verdict, or another surface decides it.
// onShow runs whenever a new plan is shown.
export function usePendingPlan(onShow: () => void) {
  const { subscribe, connected } = useWebSocket()
  const { baseUrl } = useServer()
  const { addToast } = useToast()
  const [plan, setPlan] = useState<PendingPlanData | null>(null)
  const [isOpen, setIsOpen] = useState(false)
  const [isExtension, setIsExtension] = useState(false)
  const isVerdictedRef = useRef(false)
  // currentPlanIdRef mirrors the displayed plan's parent_task_id without
  // creating a render dependency. Used by discoverPlans to skip re-showing
  // the plan we're already reviewing — otherwise a fresh plan_proposed event
  // for the same parent would reset rejectMode/notes mid-typing.
  const currentPlanIdRef = useRef<string | null>(null)
  const pendingModeRef = useRef<string | null>(null)

  const showPlan = useCallback((p: PendingPlanData) => {
    if (isVerdictedRef.current) return
    setPlan(p)
    currentPlanIdRef.current = p.parent_task_id
    setIsExtension(pendingModeRef.current === "extension")
    pendingModeRef.current = null
    setIsOpen(true)
    onShow()
  }, [onShow])

  const discoverPlans = useCallback(async () => {
    try {
      const next = newPendingPlan(await getAllPendingPlans(baseUrl), currentPlanIdRef.current)
      if (next) showPlan(next)
    } catch {
      // ignore discovery errors
    }
  }, [baseUrl, showPlan])

  useRediscover(baseUrl, connected, discoverPlans)

  // markVerdicted closes the plan once a verdict is given, here or elsewhere.
  const markVerdicted = useCallback(() => {
    isVerdictedRef.current = true
    setIsOpen(false)
    setPlan(null)
    currentPlanIdRef.current = null
    setTimeout(() => { isVerdictedRef.current = false }, VERDICT_QUIET_MS)
  }, [])

  // Listen for incoming plan_proposed and swarm_changed (cross-surface dismissal)
  useEffect(() => subscribe((msg) => {
    if (msg.type === "plan_proposed" && msg.parent_task_id) {
      pendingModeRef.current = msg.mode ?? null
      discoverPlans()
    }
    const decided = otherSurfaceVerdict(msg, plan?.parent_task_id)
    if (!decided) return
    markVerdicted()
    addToast(`Plan ${decided} on another surface`, "info")
  }), [subscribe, plan, discoverPlans, addToast, markVerdicted])

  // Dismissal is local only — the conductor is still blocked waiting for a
  // verdict. The persistent plan stays in the DB so the user can rediscover
  // it later.
  const close = () => {
    setIsOpen(false)
    currentPlanIdRef.current = null
  }

  return { plan, isOpen, isExtension, close, markVerdicted }
}

// useVerdictForm is the verdict on the plan shown: approving, or rejecting
// with notes, which takes a second step to write them. onSent runs once a
// verdict has left.
export function useVerdictForm() {
  const { send } = useWebSocket()
  const { addToast } = useToast()
  const [rejectMode, setRejectMode] = useState(false)
  const [notes, setNotes] = useState("")
  const [sending, setSending] = useState(false)
  const [verdictError, setVerdictError] = useState("")

  const reset = useCallback(() => {
    setRejectMode(false)
    setNotes("")
    setVerdictError("")
  }, [])

  const sendVerdict = (plan: PendingPlanData, onSent: () => void, status: PlanVerdict, verdictNotes?: string) => {
    setSending(true)
    setVerdictError("")
    try {
      send(planVerdictMessage(plan, status, verdictNotes))
      onSent()
      addToast(...verdictToast(status))
    } catch {
      setVerdictError("Failed to send verdict. Please try again.")
    } finally {
      setSending(false)
    }
  }

  const approve = (plan: PendingPlanData, onSent: () => void) => sendVerdict(plan, onSent, "approved")
  const reject = (plan: PendingPlanData, onSent: () => void) => {
    if (!rejectMode) {
      setRejectMode(true)
      return
    }
    if (!notes.trim()) {
      setVerdictError("Please provide notes for the rejection.")
      return
    }
    sendVerdict(plan, onSent, "rejected", notes.trim())
  }
  const back = () => {
    setRejectMode(false)
    setVerdictError("")
  }

  return { rejectMode, notes, setNotes, sending, verdictError, reset, approve, reject, back }
}
