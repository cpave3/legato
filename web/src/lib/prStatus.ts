import type { PRMetaView } from "./board-types"
import { prIsMine } from "./macros"
import { prReadiness } from "./awayPullRequests"

// What a board card says of one pull request: one word for the state that
// matters, its tone, and a detail or two. See docs/ai/web-ui.md § Per-card
// indicators.

export type PRState = "merged" | "closed" | "draft" | "failing" | "changes" | "running" | "review" | "awaiting" | "ready" | "held"

// The ink a state is written in: lw-done, lw-live, lw-error, lw-needs,
// secondary, or the card's own.
export type PRTone = "done" | "live" | "error" | "needs" | "muted" | "plain"

export interface PRStatus {
  state: PRState
  word: string
  tone: PRTone
  detail: string[]
  // Whether the operator should act on it, and which macro does: Address
  // feedback on their own (my_pr), Review on someone else's (others_pr).
  needsYou: boolean
  action?: "my_pr" | "others_pr"
}

const toneClasses: Record<PRTone, string> = {
  done: "text-lw-done",
  live: "text-lw-live",
  error: "text-lw-error",
  needs: "text-lw-needs",
  muted: "text-lw-text-2",
  plain: "text-lw-text",
}

export function prToneClass(tone: PRTone): string {
  return toneClasses[tone]
}

function comments(count: number): string[] {
  if (count <= 0) return []
  return [count === 1 ? "1 comment" : `${count} comments`]
}

function checks(status: string): string {
  switch (status) {
    case "pass": return "CI passed"
    case "fail": return "CI failing"
    case "pending": return "CI running"
    default: return "no checks"
  }
}

// prStatus says how a pull request stands. heldBelow says a layer below it
// in its stack is not ready, so an approved PR cannot merge yet.
export function prStatus(pr: PRMetaView, heldBelow = false): PRStatus {
  const readiness = prReadiness(pr)
  if (readiness === "merged") return { state: "merged", word: "Merged", tone: "done", detail: [], needsYou: false }
  if (readiness === "closed") return { state: "closed", word: "Closed, not merged", tone: "muted", detail: [], needsYou: false }
  if (readiness === "draft") return { state: "draft", word: "Draft", tone: "muted", detail: [checks(pr.check_status), ...comments(pr.comment_count)], needsYou: false }
  const approved = pr.review_decision === "APPROVED"
  const changes = pr.review_decision === "CHANGES_REQUESTED"
  if (!prIsMine(pr)) {
    // Someone else's PR: the operator's part is the review.
    if (!approved && !changes) return { state: "review", word: "Your review", tone: "needs", detail: [checks(pr.check_status), ...comments(pr.comment_count)], needsYou: true, action: "others_pr" }
  } else {
    if (pr.check_status === "fail") return { state: "failing", word: "CI failing", tone: "error", detail: comments(pr.comment_count), needsYou: true, action: "my_pr" }
    if (changes) return { state: "changes", word: "Changes requested", tone: "error", detail: comments(pr.comment_count), needsYou: true, action: "my_pr" }
  }
  if (pr.check_status === "fail") return { state: "failing", word: "CI failing", tone: "error", detail: comments(pr.comment_count), needsYou: false }
  if (changes) return { state: "changes", word: "Changes requested", tone: "error", detail: comments(pr.comment_count), needsYou: false }
  if (pr.check_status === "pending") return { state: "running", word: "CI running", tone: "needs", detail: comments(pr.comment_count), needsYou: false }
  if (!approved) return { state: "awaiting", word: "Awaiting review", tone: "plain", detail: [checks(pr.check_status), ...comments(pr.comment_count)], needsYou: false }
  if (heldBelow) return { state: "held", word: "Approved", tone: "muted", detail: comments(pr.comment_count), needsYou: false }
  return { state: "ready", word: "Ready to merge", tone: "live", detail: comments(pr.comment_count), needsYou: false }
}
