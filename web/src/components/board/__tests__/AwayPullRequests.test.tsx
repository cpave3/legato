import { cleanup, render, screen, within } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { AwayLane } from "../AwayLane"
import { BoardTaskPanel } from "../BoardTaskPanel"
import type { AwayOn, BoardCard, BoardWorker, PRMetaView } from "../../../lib/board-types"
vi.mock("../TaskAgentAction", () => ({ TaskAgentAction: () => <button type="button">Start agent</button> }))

afterEach(cleanup)

function pr(number: number, more: Partial<PRMetaView> = {}): PRMetaView {
  return { repo: "acme/widget", branch: `legato/t-1-${number}`, pr_number: number, pr_url: `https://github.com/acme/widget/pull/${number}`, state: "OPEN", is_draft: false, review_decision: "", check_status: "", comment_count: 0, pinned: false, match_method: "branch", sync_outcome: "linked", sync_error: "", ...more }
}

function away(prs: PRMetaView[], more: Partial<AwayOn> = {}): BoardCard {
  return {
    id: "T-1", title: "A big change", priority: "", issue_type: "", status: "Doing", provider: "", has_warning: false, agent_active: false, agent_state: "",
    working_seconds: 0, waiting_seconds: 0, error_seconds: 0, workspace_id: null, workspace_name: "", workspace_color: "",
    pull_requests: prs,
    away: {
      worker_instance_id: "srv-1", worker: "vm-1", handoff_id: "ho-1", worker_task_id: "w-1", purpose: "make-change", kind: "new-branch",
      repo: "acme/widget", branch: "legato/t-1", left_column: "Doing", handed_off_at: "2026-11-05T09:00:00Z", state: "in_progress", view: "working", ...more,
    },
  }
}

const vm1: BoardWorker = { instance_id: "srv-1", name: "vm-1", address: "https://vm-1.lan:3080" }

describe("an away task's pull requests", () => {
  it("are summed up on its row in the Away lane", () => {
    const card = away([pr(11, { is_draft: true }), pr(12, { check_status: "fail" })])
    render(<AwayLane workers={[vm1]} cards={[card]} selectedIndex={-1} isActive={false} onRowClick={() => {}} />)
    const row = screen.getByRole("button", { name: /A big change/ })
    expect(within(row).getByTestId("away-prs").textContent).toBe("2 PRs · 1 draft · 1 ready · #12 CI failed")
  })

  it("leave the row as it was while it has none", () => {
    render(<AwayLane workers={[vm1]} cards={[away([])]} selectedIndex={-1} isActive={false} onRowClick={() => {}} />)
    expect(screen.queryByTestId("away-prs")).toBeNull()
  })

  it("are each listed in its panel, in stack order: number, draft or ready, checks and review", () => {
    const card = away([pr(11, { is_draft: true, check_status: "pending" }), pr(12, { check_status: "fail", review_decision: "CHANGES_REQUESTED" }), pr(13, { check_status: "pass", review_decision: "APPROVED" })],
      { branches: ["legato/t-1-11", "legato/t-1-12", "legato/t-1-13", "legato/t-1-docs"] })
    render(<BoardTaskPanel card={card} column="Away" detail={null} onClose={() => {}} onOpenDetails={() => {}} onMove={() => {}} onOpenURL={() => {}} />)
    const list = screen.getByRole("list", { name: "Pull requests" })
    const items = within(list).getAllByRole("listitem")
    expect(items.map(item => within(item).queryByRole("link")?.textContent ?? null)).toEqual(["#11", "#12", "#13", null])
    expect(within(list).getByRole("link", { name: "#12" }).getAttribute("href")).toBe("https://github.com/acme/widget/pull/12")
    expect(items[0].textContent).toContain("draft")
    expect(items[0].textContent).toContain("CI pending")
    expect(items[1].textContent).toContain("ready")
    expect(items[1].textContent).toContain("CI failed")
    expect(items[1].textContent).toContain("changes requested")
    expect(items[2].textContent).toContain("CI passed")
    expect(items[2].textContent).toContain("approved")
    // A branch pushed whose PR the main has not found (yet).
    expect(screen.getByText("legato/t-1-docs").closest("li")?.textContent).toContain("no PR yet")
    // Listed once, in the away section.
    expect(screen.getAllByRole("list", { name: "Pull requests" })).toHaveLength(1)
  })

  it("say in the panel when the worker pushed nothing yet", () => {
    render(<BoardTaskPanel card={away([])} column="Away" detail={null} onClose={() => {}} onOpenDetails={() => {}} onMove={() => {}} onOpenURL={() => {}} />)
    expect(screen.getByText("No branch pushed yet")).toBeTruthy()
  })
})
