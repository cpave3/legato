import type { PRMetaView } from "../../board-types"

export type Shape = "merged" | "ready" | "fail" | "approved" | "run" | "draft" | "closed" | "awaiting"

export function pr(number: number, shape: Shape, base: string, overrides: Partial<PRMetaView> = {}): PRMetaView {
  const look: Partial<PRMetaView> = {
    merged: { state: "MERGED", review_decision: "APPROVED" },
    ready: { review_decision: "APPROVED" },
    approved: { review_decision: "APPROVED" },
    fail: { check_status: "fail", review_decision: "APPROVED" },
    run: { check_status: "pending", review_decision: "APPROVED" },
    draft: { is_draft: true },
    closed: { state: "CLOSED" },
    awaiting: { review_decision: "REVIEW_REQUIRED" },
  }[shape]
  return {
    repo: "cpave3/legato", branch: `b${number}`, base, pr_number: number, pr_url: `https://github.com/cpave3/legato/pull/${number}`,
    state: "OPEN", is_draft: false, review_decision: "", check_status: "pass", comment_count: 0, pinned: false,
    match_method: "", sync_outcome: "", sync_error: "", ...look, ...overrides,
  }
}

// LEG-386: #194–#195 merged, #196–#199 ready, #200 failing with 6
// comments, the rest approved or running above it.
export function leg386(): PRMetaView[] {
  const shapes: [number, Shape, number?][] = [
    [194, "merged"], [195, "merged"], [196, "ready", 1], [197, "ready"], [198, "ready", 2], [199, "ready"],
    [200, "fail", 6], [201, "approved"], [202, "approved"], [203, "run", 2], [204, "approved"], [205, "approved", 1], [207, "approved"],
  ]
  return shapes.map(([n, shape, comments], i) => pr(n, shape, i < 3 ? "main" : `b${shapes[i - 1][0]}`, { comment_count: comments ?? 0 }))
}

