import type { PendingPlanData } from "./swarm"

// The rules of the swarm plan approval modal, apart from how it is drawn.

export type PlanVerdict = "approved" | "rejected"

// The plan_verdict message that answers a conductor waiting on its plan.
export function planVerdictMessage(plan: PendingPlanData, status: PlanVerdict, notes = "") {
  return {
    type: "plan_verdict" as const,
    parent_task_id: plan.parent_task_id,
    plan_path: plan.plan_path,
    reply_socket: plan.reply_socket,
    status,
    notes,
  }
}

export function verdictToast(status: PlanVerdict): [string, "success" | "error"] {
  return status === "approved" ? ["Plan approved", "success"] : ["Plan rejected with notes", "error"]
}

// The plan to show from those pending: the oldest, unless it is the one
// already shown, whose rejection notes may be half typed.
export function newPendingPlan(plans: PendingPlanData[], shownId: string | null): PendingPlanData | null {
  const oldest = plans[0]
  if (!oldest || oldest.parent_task_id === shownId) return null
  return oldest
}

interface SwarmChange {
  type: string
  parent_task_id?: string
  status?: string
}

// How another surface decided the plan on screen, from a swarm_changed
// message, or null when the message is about something else.
export function otherSurfaceVerdict(msg: SwarmChange, shownId: string | undefined): PlanVerdict | null {
  if (msg.type !== "swarm_changed" || !msg.parent_task_id || msg.parent_task_id !== shownId) return null
  if (msg.status === "plan_applied") return "approved"
  return msg.status === "rejected" ? "rejected" : null
}
