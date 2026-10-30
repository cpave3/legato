import { afterEach, describe, expect, it } from "vitest"
import type { Approval } from "../approvals"
import { approvalNotice, approvalNotificationsEnabled, freshApprovals, setApprovalNotificationsEnabled } from "../approvalNotifications"

const approval = (id: string, over: Partial<Approval> = {}): Approval => ({
  id, source: "credential", source_key: id, task_id: "AG-10102", title: "Push to legato/ag-10102-fix", summary: "git.push on github:autopilot-dev/autopilot@refs/heads/legato/ag-10102-fix",
  details: { task_title: "Comparable sales", agent_kind: "claude-code" }, options: [], blocking: true, status: "pending", revision: 1, created_at: "2026-10-30T01:00:00Z",
  ...over,
} as Approval)

describe("freshApprovals", () => {
  it("counts what was waiting when the page loaded as seen, and finds each one that arrives after", () => {
    let state = freshApprovals(null, [approval("ap-1")])
    expect(state.fresh).toEqual([])
    state = freshApprovals(state.seen, [approval("ap-1"), approval("ap-2")])
    expect(state.fresh.map((a) => a.id)).toEqual(["ap-2"])
    state = freshApprovals(state.seen, [approval("ap-2")])
    expect(state.fresh).toEqual([])
  })
})

describe("approvalNotice", () => {
  it("says what is asked, by which agent and for which task", () => {
    const notice = approvalNotice(approval("ap-1"))
    expect(notice.title).toBe("Approval needed: Push to legato/ag-10102-fix")
    expect(notice.body).toContain("AG-10102 · Comparable sales · Claude Code")
    expect(notice.body).toContain("git.push on github:autopilot-dev/autopilot")
  })
})

describe("the setting", () => {
  afterEach(() => localStorage.clear())
  it("is on until the person turns it off in this browser", () => {
    expect(approvalNotificationsEnabled()).toBe(true)
    setApprovalNotificationsEnabled(false)
    expect(approvalNotificationsEnabled()).toBe(false)
    setApprovalNotificationsEnabled(true)
    expect(approvalNotificationsEnabled()).toBe(true)
  })
})
