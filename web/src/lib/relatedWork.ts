import type { RelatedWorkItem } from "./task-activity"

// Statuses of work that is still under way: a review being captured, a
// workflow running or waiting at a step.
const UNDER_WAY = new Set(["capturing", "running", "pending", "waiting_agent", "waiting_manual"])

// splitRelated puts the related work still under way apart from the rest,
// keeping the order of each.
export function splitRelated(items: RelatedWorkItem[]): { inProgress: RelatedWorkItem[]; linked: RelatedWorkItem[] } {
  return {
    inProgress: items.filter((item) => UNDER_WAY.has(item.status)),
    linked: items.filter((item) => !UNDER_WAY.has(item.status)),
  }
}

export function relatedStatusLabel(status: string): string {
  return status.replace(/_/g, " ")
}

const TONES: Record<string, string> = {
  capturing: "bg-lw-live-soft text-lw-live",
  running: "bg-lw-live-soft text-lw-live",
  pending: "bg-lw-idle-soft text-lw-text-2",
  waiting_agent: "bg-lw-needs-soft text-lw-needs",
  waiting_manual: "bg-lw-needs-soft text-lw-needs",
}

// The chip of something under way.
export function relatedStatusChip(status: string): string {
  return TONES[status] ?? "bg-lw-idle-soft text-lw-text-2"
}

const TEXT_TONES: Record<string, string> = {
  proposed: "text-lw-needs",
  changes_requested: "text-lw-needs",
  ready: "text-lw-needs",
  approved: "text-lw-live",
  completed: "text-lw-live",
  reviewed: "text-lw-live",
  succeeded: "text-lw-live",
  rejected: "text-lw-error",
  failed: "text-lw-error",
}

// The colour of a status written beside something linked.
export function relatedStatusText(status: string): string {
  return TEXT_TONES[status] ?? "text-lw-text-2"
}

// What a status means, for work under way, where there is something to say.
export function relatedStatusNote(item: RelatedWorkItem): string | undefined {
  if (item.kind === "review" && item.status === "capturing") return "Recording changes as the agent works. Moves to Review when the agent finishes."
  if (item.kind === "workflow_run" && item.status === "waiting_manual") return "Waiting for you at a step."
  if (item.kind === "workflow_run" && item.status === "waiting_agent") return "Waiting for its agent."
  return undefined
}
