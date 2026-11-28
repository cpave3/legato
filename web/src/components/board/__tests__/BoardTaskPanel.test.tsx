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

  function renderPanel(more: Partial<BoardCard> = {}, props: Partial<React.ComponentProps<typeof BoardTaskPanel>> = {}) {
    const handlers = { onClose: vi.fn(), onOpenDetails: vi.fn(), onMove: vi.fn(), onOpenURL: vi.fn() }
    render(<BoardTaskPanel card={{ ...card, ...more }} column="Doing" detail={null} {...handlers} {...props} />)
    const panel = screen.getByRole("complementary", { name: "Selected task" })
    const value = (label: string) => within(panel).getByText(label, { selector: "dt" }).nextElementSibling as HTMLElement
    return { ...handlers, panel, value }
  }

  it("says where a task stands: column, priority, type, workspace, no agent yet and the time agents spent", () => {
    const { panel, value } = renderPanel({ priority: "High", issue_type: "Bug", working_seconds: 3600 })
    expect(value("Column").textContent).toBe("Doing")
    expect(value("Priority").querySelector("span")?.className).toBe("text-lw-error")
    expect(value("Type").textContent).toBe("Bug")
    expect(value("Workspace").textContent).toBe("Unassigned")
    expect(value("Agent").textContent).toBe("None yet")
    expect(value("Agent time").textContent).toBe("1h")
    expect(within(panel).queryByText("Worktree", { selector: "dt" })).toBeNull()
  })

  it("names a running agent and its tool, and leaves out the time spent", () => {
    const { panel, value } = renderPanel({ agent_active: true, agent_state: "working", agent_kind: "codex", working_seconds: 60, workspace_name: "Web" })
    expect(value("Agent").textContent).toMatch(/· Codex$/)
    expect(value("Workspace").textContent).toBe("Web")
    expect(within(panel).queryByText("Agent time", { selector: "dt" })).toBeNull()
  })

  it("names a pull request's state, one without an address unlinked, in the singular", () => {
    const { value } = renderPanel({ pull_requests: [pr(7, { pr_url: "", state: "MERGED", review_decision: "CHANGES_REQUESTED" })] })
    const item = value("Pull request")
    expect(within(item).queryByRole("link")).toBeNull()
    expect(within(item).getByText("#7")).toBeTruthy()
    expect(within(item).getByText("merged")).toBeTruthy()
    expect(within(item).getByText("changes requested").className).toContain("text-lw-error")
  })

  it("reads the task, then shows its description, its worktree and the way to its ticket", () => {
    const { panel, onOpenURL } = renderPanel({ provider: "jira", remote_id: "A-1" })
    expect(within(panel).getByText("Reading the task…")).toBeTruthy()
    cleanup()
    const detail = { id: "T-1", description_md: "", worktree: { branch: "t-1", path: "/w" }, remote_meta: { url: "https://jira/A-1" } } as never
    const again = renderPanel({ provider: "jira", remote_id: "A-1" }, { detail, onOpenURL })
    expect(within(again.panel).getByText("No description")).toBeTruthy()
    expect(again.value("Worktree").textContent).toBe("t-1")
    fireEvent.click(within(again.panel).getByRole("button", { name: "Open in Jira" }))
    expect(onOpenURL).toHaveBeenCalledWith("https://jira/A-1")
  })

  it("keeps reading while the record read is another task's", () => {
    const { panel } = renderPanel({}, { detail: { id: "T-2", description_md: "Other" } as never })
    expect(within(panel).getByText("Reading the task…")).toBeTruthy()
    expect(within(panel).queryByText("Other")).toBeNull()
  })

  it("opens the pull request when there is no ticket page, and closes", () => {
    const { panel, onOpenURL, onClose, onMove, onOpenDetails } = renderPanel()
    fireEvent.click(within(panel).getByRole("button", { name: "Open PR" }))
    expect(onOpenURL).toHaveBeenCalledWith("https://github.com/acme/widget/pull/11")
    fireEvent.click(within(panel).getByRole("button", { name: "Move" }))
    fireEvent.click(within(panel).getByRole("button", { name: "Open details" }))
    fireEvent.click(within(panel).getByRole("button", { name: "Close task panel" }))
    expect([onMove, onOpenDetails, onClose].map(f => f.mock.calls.length)).toEqual([1, 1, 1])
    expect(within(panel).queryByRole("button", { name: "Hand off…" })).toBeNull()
  })

  it("offers no page to open without a ticket or pull request address", () => {
    const { panel } = renderPanel({ pull_requests: [] })
    expect(within(panel).queryByRole("button", { name: /^Open (PR|in)/ })).toBeNull()
  })

  it("links an away task's reviews and its session history on the worker", () => {
    const away: BoardCard = {
      ...card, pull_requests: [], priority: "Low",
      away: {
        worker_instance_id: "srv-1", worker: "vm-1", handoff_id: "ho-1", worker_task_id: "w-1", purpose: "make-change", kind: "new-branch",
        repo: "acme/widget", branch: "legato/t-1", left_column: "Doing", handed_off_at: "2026-11-05T09:00:00Z", state: "working",
        url: "https://vm-1.lan:3080/board?highlight_task=w-1", review: { links: [{ pr: 3, url: "https://vm-1/review/3" }] },
      } as BoardCard["away"],
    }
    render(<BoardTaskPanel card={away} column="Away" detail={null} onClose={() => {}} onOpenDetails={() => {}} onMove={() => {}} onOpenURL={() => {}} />)
    const panel = screen.getByRole("complementary", { name: "Selected task" })
    expect(within(panel).getByRole("link", { name: "Review of #3" }).getAttribute("href")).toBe("https://vm-1/review/3")
    expect(within(panel).getByRole("link", { name: "Session history on vm-1" })).toBeTruthy()
    expect(within(panel).getByText(/Changes come from vm-1 when the task comes back/)).toBeTruthy()
    expect(within(panel).queryByText("Column", { selector: "dt" })).toBeNull()
    expect(within(panel).queryByText("Agent", { selector: "dt" })).toBeNull()
    expect(within(panel).getByText("Priority", { selector: "dt" })).toBeTruthy()
  })

  it("shows an away task's Return and Recall in its Away section", () => {
    const away: BoardCard = {
      ...card, pull_requests: [],
      away: {
        worker_instance_id: "srv-1", worker: "vm-1", handoff_id: "ho-1", worker_task_id: "w-1", purpose: "make-change", kind: "new-branch",
        repo: "acme/widget", branch: "legato/t-1", left_column: "Doing", handed_off_at: "2026-11-05T09:00:00Z", state: "done", view: "done",
      },
    }
    render(<BoardTaskPanel card={away} column="Away" detail={null} onClose={() => {}} onOpenDetails={() => {}} onMove={() => {}} onOpenURL={() => {}}
      awayActions={<button type="button">Return</button>} />)
    const section = screen.getByRole("region", { name: "Away" })
    expect(within(section).getByRole("button", { name: "Return" })).toBeTruthy()
  })
})
