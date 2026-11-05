import { describe, expect, it } from "vitest"
import { awayPRSummary, prReadiness } from "../awayPullRequests"
import type { PRMetaView } from "../board-types"

function pr(number: number, more: Partial<PRMetaView> = {}): PRMetaView {
  return { repo: "acme/widget", branch: `legato/t-${number}`, pr_number: number, pr_url: `https://github.com/acme/widget/pull/${number}`, state: "OPEN", is_draft: false, review_decision: "", check_status: "", comment_count: 0, pinned: false, match_method: "branch", sync_outcome: "linked", sync_error: "", ...more }
}

describe("prReadiness", () => {
  it("says whether a PR is a draft, ready, merged or closed", () => {
    expect(prReadiness(pr(1, { is_draft: true }))).toBe("draft")
    expect(prReadiness(pr(1))).toBe("ready")
    expect(prReadiness(pr(1, { state: "MERGED" }))).toBe("merged")
    expect(prReadiness(pr(1, { state: "CLOSED", is_draft: true }))).toBe("closed")
  })
})

describe("awayPRSummary", () => {
  it("is nothing without a PR", () => {
    expect(awayPRSummary([])).toBeNull()
    expect(awayPRSummary(undefined)).toBeNull()
  })

  it("names one PR and how it stands", () => {
    expect(awayPRSummary([pr(12, { is_draft: true })])).toEqual({ text: "PR #12 · draft", blocked: false })
    expect(awayPRSummary([pr(12, { check_status: "pass" })])).toEqual({ text: "PR #12 · ready", blocked: false })
  })

  it("counts several PRs by how they stand", () => {
    expect(awayPRSummary([pr(11, { state: "MERGED" }), pr(12, { is_draft: true }), pr(13, { is_draft: true }), pr(14)]))
      .toEqual({ text: "4 PRs · 1 merged · 2 draft · 1 ready", blocked: false })
  })

  it("says what blocks a ready PR, as Attention's Merge blockers do, and nothing of a draft's", () => {
    expect(awayPRSummary([pr(12, { check_status: "fail", review_decision: "CHANGES_REQUESTED" })]))
      .toEqual({ text: "PR #12 · ready · CI failed, changes requested", blocked: true })
    expect(awayPRSummary([pr(11, { is_draft: true, check_status: "fail" }), pr(12, { check_status: "fail" })]))
      .toEqual({ text: "2 PRs · 1 draft · 1 ready · #12 CI failed", blocked: true })
    expect(awayPRSummary([pr(12, { is_draft: true, check_status: "fail" })]))
      .toEqual({ text: "PR #12 · draft", blocked: false })
  })
})
