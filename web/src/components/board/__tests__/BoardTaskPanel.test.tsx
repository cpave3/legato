import { afterEach, describe, expect, it, vi } from "vitest"
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { BoardTaskPanel } from "../BoardTaskPanel"
import type { BoardCard, PRMetaView } from "../../../lib/board-types"
vi.mock("../TaskAgentAction", () => ({ TaskAgentAction: () => <button type="button">Start agent</button> }))

afterEach(cleanup)

function pr(number: number, more: Partial<PRMetaView> = {}): PRMetaView {
  return { repo: "acme/widget", branch: `stack-${number}`, pr_number: number, pr_url: `https://github.com/acme/widget/pull/${number}`, state: "OPEN", is_draft: false, review_decision: "", check_status: "", comment_count: 0, pinned: false, match_method: "branch", sync_outcome: "linked", sync_error: "", ...more }
}

const card: BoardCard = {
  id: "T-1", title: "Stacked", priority: "", issue_type: "", status: "Doing", provider: "", has_warning: false,
  agent_active: false, agent_state: "", working_seconds: 0, waiting_seconds: 0, error_seconds: 0,
  workspace_id: null, workspace_name: "", workspace_color: "",
  pull_requests: [pr(11, { check_status: "fail" }), pr(12, { is_draft: true })],
}

describe("BoardTaskPanel", () => {
  it("lists every pull request of the task in stack order", () => {
    render(<BoardTaskPanel card={card} column="Doing" detail={null} onClose={() => {}} onOpenDetails={() => {}} onMove={() => {}} onOpenURL={() => {}} />)
    const list = screen.getByRole("list", { name: "Pull requests" })
    const links = within(list).getAllByRole("link")
    expect(links.map(link => link.textContent)).toEqual(["#11", "#12"])
    expect(links[1].getAttribute("href")).toBe("https://github.com/acme/widget/pull/12")
    expect(within(list).getByText("draft")).toBeTruthy()
  })

  it("offers to hand the task off when a worker is paired", () => {
    const onHandOff = vi.fn()
    render(<BoardTaskPanel card={card} column="Doing" detail={null} onClose={() => {}} onOpenDetails={() => {}} onMove={() => {}} onOpenURL={() => {}} onHandOff={onHandOff} />)
    fireEvent.click(screen.getByRole("button", { name: "Hand off…" }))
    expect(onHandOff).toHaveBeenCalledTimes(1)
  })

  it("shows an away task read-only, with where it went and a way to open it there", () => {
    const away: BoardCard = {
      ...card, pull_requests: [],
      away: {
        worker_instance_id: "srv-1", worker: "vm-1", handoff_id: "ho-1", worker_task_id: "w-1", purpose: "find-out", kind: "new-branch",
        repo: "acme/widget", branch: "legato/t-1", left_column: "Doing", handed_off_at: "2026-11-05T09:00:00Z", state: "pending",
        url: "https://vm-1.lan:3080/board?highlight_task=w-1",
      },
    }
    const detail = { id: "T-1", description_md: "Find out why." } as never
    render(<BoardTaskPanel card={away} column="Away" detail={detail} onClose={() => {}} onOpenDetails={() => {}} onMove={() => {}} onOpenURL={() => {}} onHandOff={() => {}} />)
    const panel = screen.getByRole("complementary", { name: "Selected task" })
    expect(within(panel).getByText("vm-1")).toBeTruthy()
    expect(within(panel).getByText("Find out")).toBeTruthy()
    expect(within(panel).getByText("A new branch")).toBeTruthy()
    expect(within(panel).getByText("legato/t-1")).toBeTruthy()
    expect(within(panel).getByText("acme/widget")).toBeTruthy()
    expect(within(panel).getByText("Starting")).toBeTruthy()
    expect(within(panel).getByRole("link", { name: "Open on vm-1" }).getAttribute("href")).toBe("https://vm-1.lan:3080/board?highlight_task=w-1")
    expect(within(panel).getByText(/read-only while away/i)).toBeTruthy()
    expect(within(panel).getByText("Find out why.")).toBeTruthy()
    // Nothing here can change it, start its agent or hand it off again.
    for (const name of ["Move", "Start agent", "Hand off…"]) expect(within(panel).queryByRole("button", { name })).toBeNull()
    expect(within(panel).getByRole("button", { name: "Open details" })).toBeTruthy()
  })
})
