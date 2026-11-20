import { apiFetch } from "./api"
import type { AttentionAction, AttentionItem } from "./attention"

// The tone of an item's kind: the colour of its word and icon. Colour is
// never the only cue: the kind's word and its own icon say it too.
export type NeedsYouTone = "needs" | "question" | "error" | "done" | "plain"

// The kinds an item of the needs-you rail is drawn as, each with its own
// word and icon (lucide names, resolved by the row).
export type NeedsYouKind =
  | "approval" | "action" | "question" | "done" | "other"
  | "waiting" | "error" | "failed" | "gate" | "plan" | "review" | "merge" | "legato"

export const needsYouKinds: Record<NeedsYouKind, { word: string; tone: NeedsYouTone }> = {
  approval: { word: "Approval", tone: "needs" },
  action: { word: "Action", tone: "needs" },
  question: { word: "Question", tone: "question" },
  done: { word: "Done", tone: "done" },
  other: { word: "Note", tone: "plain" },
  waiting: { word: "Waiting", tone: "needs" },
  error: { word: "Error", tone: "error" },
  failed: { word: "Failed", tone: "error" },
  gate: { word: "Gate", tone: "needs" },
  plan: { word: "Plan", tone: "question" },
  review: { word: "Review", tone: "done" },
  merge: { word: "Merge", tone: "error" },
  legato: { word: "Legato", tone: "plain" },
}

// needsYouKind is how an Attention item is drawn, from its signal kind.
export function needsYouKind(item: Pick<AttentionItem, "signal_kind">): NeedsYouKind {
  switch (item.signal_kind) {
    case "approval": return "approval"
    case "operator_action": return "action"
    case "operator_question": return "question"
    case "operator_done": return "done"
    case "operator_other": return "other"
    case "agent_waiting": case "workflow_waiting_agent": return "waiting"
    case "agent_error": return "error"
    case "workflow_failed": case "workflow_trigger_failed": case "handoff_failed": case "restart_rolled_back": return "failed"
    case "workflow_manual_gate": case "workflow_trigger_input": case "workflow_trigger_confirmation": case "workflow_trigger_loop_stop": case "workflow_review_stuck": return "gate"
    case "plan_decision": return "plan"
    case "review_ready": case "handoff_done": return "review"
    case "pr_merge_blocked": case "argus_case": case "argus_needs_you": return "merge"
    default: return "legato"
  }
}

// The how of an item: what the operator runs (a copy action) or opens (a
// navigate action with an address of its own, Open link).
export function itemHow(item: Pick<AttentionItem, "actions">): { copy?: AttentionAction; link?: AttentionAction } {
  const actions = item.actions ?? []
  return {
    copy: actions.find((action) => action.kind === "copy"),
    link: actions.find((action) => action.id === "open_link" && action.kind === "navigate" && action.href),
  }
}

// The task an item is about, which the rail marks "this agent" on that
// agent's page.
export function itemAgent(item: Pick<AttentionItem, "navigation" | "task_id">): string | undefined {
  if (item.navigation?.kind === "agent") return item.navigation.target
  return item.task_id
}

// The rail's view of Attention: the items of the groups its settings show,
// newest first, each with its rail_group, and how many groups are hidden.
export interface RailView {
  items: AttentionItem[]
  hidden: number
}

export async function fetchRailAttention(baseUrl: string): Promise<RailView> {
  const resp = await apiFetch(baseUrl, "/api/attention?view=rail")
  if (!resp.ok) throw new Error(await resp.text())
  const body = await resp.json() as { items?: AttentionItem[]; hidden_groups?: number }
  return { items: newestFirst(body.items ?? []), hidden: body.hidden_groups ?? 0 }
}

// newestFirst orders items by when they last changed, the newest first.
export function newestFirst(items: AttentionItem[]): AttentionItem[] {
  return [...items].sort((a, b) => (b.updated_at ?? "").localeCompare(a.updated_at ?? ""))
}

const FOLDED_KEY = "legato:rail-folded"

// Whether the rail is folded in this browser.
export function readRailFolded(): boolean {
  try {
    return window.localStorage.getItem(FOLDED_KEY) === "1"
  } catch {
    return false
  }
}

export function writeRailFolded(folded: boolean) {
  try {
    if (folded) window.localStorage.setItem(FOLDED_KEY, "1")
    else window.localStorage.removeItem(FOLDED_KEY)
  } catch {
    // Without storage the rail opens each time.
  }
}

// shortAge says how long ago an item changed: now, 4m, 2h, 3d. Attention's
// times are SQLite's, UTC without a zone.
export function shortAge(iso: string, now = Date.now()): string {
  const normal = /[zZ]|[+-]\d\d:?\d\d$/.test(iso) ? iso : iso.replace(" ", "T") + "Z"
  const minutes = Math.floor((now - new Date(normal).getTime()) / 60000)
  if (!Number.isFinite(minutes) || minutes < 1) return "now"
  if (minutes < 60) return `${minutes}m`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}h`
  return `${Math.floor(hours / 24)}d`
}
