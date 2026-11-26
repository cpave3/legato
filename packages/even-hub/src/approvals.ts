import { whole, type FeedPage, type TextPage } from "./agent"
import { fitItem } from "./glance"
import { CANVAS_WIDTH, FRAME_WIDTH, center, fitLines, inner, TITLE_WIDTH } from "./layout"
import type { Approval } from "./legato"

// Approvals on the glasses: the requests waiting for the operator, listed;
// one in full, with its options when the glasses may decide it; and the
// steps of a decision, which is confirmed with a second tap before it goes.
// The glasses decide only what needs no signature: Legato refuses to allow a
// signed approval without one, and a question has no options to pick.

export type Option = Approval["options"][number]
// An approval in full: its header, what it would do, and the options the
// glasses offer (none when it is decided elsewhere).
export type ApprovalPage = { badge: string; title: string; body: string; options: string[]; hint: string }

export type Decision =
  | { step: "confirm"; option: Option }
  | { step: "deciding"; option: Option }
  | { step: "done"; option: Option }
  | { step: "problem"; option: Option; problem: string }

const hint = (keys: string) => center(keys, CANVAS_WIDTH)

// Whether the glasses may decide it: it has options and needs no signature.
export function actionable(a: Approval): boolean {
  return !a.signed && a.options.length > 0
}

// Who asks: the Reeve for its own requests (whose task is its session's
// hidden one), else the agent's task.
function who(a: Approval): string {
  if (a.source.startsWith("reeve")) return "Reeve"
  return a.task_id || a.source
}

const mark = (a: Approval) => (a.signed ? "◇" : actionable(a) ? "●" : "○")

// The list of approvals waiting. Each item keeps the approval's ID as its
// detail, to open it by.
export function approvalsPage(approvals: Approval[], instance: string): FeedPage {
  const items = approvals.map((a) => ({ text: fitItem(`${mark(a)} ${who(a)} ${a.title.replace(/\s+/g, " ").trim()}`), detail: a.id }))
  return {
    badge: "● to approve",
    title: fitItem(`${approvals.length} waiting · ${instance}`, inner(TITLE_WIDTH), Infinity),
    items: items.length ? items : [{ text: "Nothing waits for you." }],
    hint: hint("Tap: open · Double tap: back"),
  }
}

function what(a: Approval): string {
  const reason = typeof a.details?.reason === "string" ? a.details.reason.trim() : ""
  return [a.title.trim(), a.summary?.trim() ?? "", reason].filter(Boolean).join("\n\n")
}

// The lines of an approval above its options: what it is, in brief. All of
// it shows when an option is chosen, on the page that confirms it.
export const APPROVAL_BODY_LINES = 2

// One approval: in full when the glasses cannot decide it, else in brief
// above its options.
export function approvalPage(a: Approval): ApprovalPage {
  const title = fitItem(who(a), inner(TITLE_WIDTH), Infinity)
  if (a.signed) {
    return { badge: "◇ signed", title, body: whole(`${what(a)}\n\nIt needs your signature: approve it on your phone.`), options: [], hint: hint("Double tap: back") }
  }
  if (!actionable(a)) {
    return { badge: "○ question", title, body: whole(`${what(a)}\n\nAnswer it on your phone or in Legato.`), options: [], hint: hint("Double tap: back") }
  }
  const brief = fitLines(a.title.replace(/\s+/g, " ").trim(), inner(FRAME_WIDTH), APPROVAL_BODY_LINES)
  return { badge: "● approve?", title, body: brief, options: a.options.map((o) => fitItem(o.label)), hint: hint("Tap: choose · Double tap: back") }
}

// The steps of a decision on the option chosen.
export function decisionPage(a: Approval, d: Decision): TextPage {
  const title = fitItem(who(a), inner(TITLE_WIDTH), Infinity)
  const allow = d.option.kind === "allow"
  const page = (badge: string, body: string, keys: string) => ({ badge, title, body: whole(body), hint: keys ? hint(keys) : "" })
  switch (d.step) {
    case "confirm":
      return page(allow ? "Allow?" : "Deny?", `${d.option.label}\n\n${what(a)}`, "Tap: confirm · Double tap: back")
    case "deciding":
      return page("Deciding", `${d.option.label}\n\n${a.title.trim()}`, "")
    case "done":
      return page(allow ? "Allowed" : "Denied", `${d.option.label}\n\n${a.title.trim()}`, "")
    case "problem":
      return page("▲ Not decided", d.problem, "Tap: try again · Double tap: back")
  }
}
