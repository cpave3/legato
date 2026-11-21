import { describe, expect, it } from "vitest"
import { prStatus } from "../prStatus"
import type { PRMetaView } from "../board-types"

function pr(overrides: Partial<PRMetaView> = {}): PRMetaView {
  return {
    repo: "acme/app", branch: "b", pr_number: 1, pr_url: "", state: "OPEN", is_draft: false,
    review_decision: "", check_status: "pass", comment_count: 0, pinned: false, match_method: "", sync_outcome: "", sync_error: "",
    ...overrides,
  }
}

describe("prStatus", () => {
  it.each([
    ["merged", pr({ state: "MERGED", check_status: "fail" }), "Merged", "done"],
    ["closed", pr({ state: "CLOSED" }), "Closed, not merged", "muted"],
    ["a draft", pr({ is_draft: true, check_status: "fail" }), "Draft", "muted"],
    ["CI failing", pr({ check_status: "fail", review_decision: "APPROVED" }), "CI failing", "error"],
    ["changes requested", pr({ review_decision: "CHANGES_REQUESTED" }), "Changes requested", "error"],
    ["CI running", pr({ check_status: "pending", review_decision: "APPROVED" }), "CI running", "needs"],
    ["awaiting review", pr({ review_decision: "REVIEW_REQUIRED" }), "Awaiting review", "plain"],
    ["approved with checks passed", pr({ review_decision: "APPROVED" }), "Ready to merge", "live"],
    ["approved with no checks", pr({ review_decision: "APPROVED", check_status: "" }), "Ready to merge", "live"],
  ])("says %s in one word", (_, view, word, tone) => {
    const status = prStatus(view)
    expect(status.word).toBe(word)
    expect(status.tone).toBe(tone)
  })

  it("holds an approved PR below a layer that is not ready", () => {
    expect(prStatus(pr({ review_decision: "APPROVED" }), true)).toMatchObject({ state: "held", word: "Approved", needsYou: false })
  })

  it("adds the comments, and the checks of a PR awaiting review", () => {
    expect(prStatus(pr({ check_status: "fail", comment_count: 6 })).detail).toEqual(["6 comments"])
    expect(prStatus(pr({ comment_count: 1, review_decision: "" })).detail).toEqual(["CI passed", "1 comment"])
    expect(prStatus(pr({ is_draft: true, check_status: "" })).detail).toEqual(["no checks"])
    expect(prStatus(pr({ state: "MERGED", comment_count: 4 })).detail).toEqual([])
  })

  it("needs the operator only for their own PR failing or with changes requested, or someone else's awaiting their review", () => {
    expect(prStatus(pr({ check_status: "fail" }))).toMatchObject({ needsYou: true, action: "my_pr" })
    expect(prStatus(pr({ review_decision: "CHANGES_REQUESTED" }))).toMatchObject({ needsYou: true, action: "my_pr" })
    expect(prStatus(pr({ review_decision: "APPROVED" })).needsYou).toBe(false)
    expect(prStatus(pr({ review_decision: "" })).needsYou).toBe(false)
    expect(prStatus(pr({ is_draft: true, check_status: "fail" })).needsYou).toBe(false)
    expect(prStatus(pr({ state: "MERGED", check_status: "fail" })).needsYou).toBe(false)
    expect(prStatus(pr({ pinned: true, review_decision: "REVIEW_REQUIRED", check_status: "pending", comment_count: 3 }))).toMatchObject({ word: "Your review", tone: "needs", needsYou: true, action: "others_pr", detail: ["CI running", "3 comments"] })
    expect(prStatus(pr({ pinned: true, review_decision: "APPROVED" })).needsYou).toBe(false)
    expect(prStatus(pr({ pinned: true, check_status: "fail", review_decision: "APPROVED" })).needsYou).toBe(false)
  })
})
