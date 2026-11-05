import type { PRMetaView } from "./board-types"

// The PRs of a task away on a worker, as the main tracks them itself from
// the branches the worker reports. See docs/ai/pr-tracking.md § Away tasks.

export type PRReadiness = "draft" | "ready" | "merged" | "closed"

// How a PR stands: a draft, ready for review, merged or closed.
export function prReadiness(pr: PRMetaView): PRReadiness {
  switch (pr.state.toUpperCase()) {
    case "MERGED": return "merged"
    case "CLOSED": return "closed"
  }
  return pr.is_draft ? "draft" : "ready"
}

// What blocks a ready PR's merge, as Attention's Merge blockers say it:
// nothing for a draft, a merged or a closed PR.
export function prBlockers(pr: PRMetaView): string[] {
  if (prReadiness(pr) !== "ready") return []
  const blockers: string[] = []
  if (pr.check_status === "fail") blockers.push("CI failed")
  if (pr.review_decision === "CHANGES_REQUESTED") blockers.push("changes requested")
  else if (pr.review_decision === "REVIEW_REQUIRED") blockers.push("review required")
  return blockers
}

export interface AwayPRSummary {
  // "PR #12 · draft", "3 PRs · 2 draft · 1 ready · #14 CI failed"
  text: string
  // A ready PR has a blocker.
  blocked: boolean
}

const order: PRReadiness[] = ["merged", "draft", "ready", "closed"]

// One line on an away row: the task's PRs and what blocks them; null with
// none.
export function awayPRSummary(prs: PRMetaView[] | undefined): AwayPRSummary | null {
  if (!prs || prs.length === 0) return null
  const parts: string[] = []
  if (prs.length === 1) {
    parts.push(`PR #${prs[0].pr_number}`, prReadiness(prs[0]))
  } else {
    parts.push(`${prs.length} PRs`)
    const counts = new Map<PRReadiness, number>()
    for (const pr of prs) counts.set(prReadiness(pr), (counts.get(prReadiness(pr)) ?? 0) + 1)
    for (const r of order) if (counts.get(r)) parts.push(`${counts.get(r)} ${r}`)
  }
  const blocked = prs.filter(pr => prBlockers(pr).length > 0)
  if (blocked.length > 0) {
    parts.push(blocked.map(pr => (prs.length === 1 ? "" : `#${pr.pr_number} `) + prBlockers(pr).join(", ")).join("; "))
  }
  return { text: parts.join(" · "), blocked: blocked.length > 0 }
}
