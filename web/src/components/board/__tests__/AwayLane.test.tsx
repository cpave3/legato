import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { AwayLane, WorkerReach } from "../AwayLane"
import { MemoryRouter } from "react-router-dom"
import type { AwayOn, AwayReview, BoardCard, BoardWorker } from "../../../lib/board-types"

afterEach(cleanup)

function away(id: string, title: string, more: Partial<AwayOn> = {}): BoardCard {
  return {
    id, title, priority: "", issue_type: "", status: "Doing", provider: "", has_warning: false, agent_active: false, agent_state: "",
    working_seconds: 0, waiting_seconds: 0, error_seconds: 0, workspace_id: null, workspace_name: "", workspace_color: "",
    away: {
      worker_instance_id: "srv-1", worker: "vm-1", handoff_id: "ho-" + id, worker_task_id: "w-" + id, purpose: "make-change", kind: "new-branch",
      repo: "acme/widget", branch: "legato/" + id, left_column: "Doing", handed_off_at: "2026-11-05T09:00:00Z", state: "pending",
      url: "https://vm-1.lan:3080/board?highlight_task=w-" + id, ...more,
    },
  }
}

const vm1: BoardWorker = { instance_id: "srv-1", name: "vm-1", address: "https://vm-1.lan:3080" }
const vm2: BoardWorker = { instance_id: "srv-2", name: "vm-2", address: "https://vm-2.lan:3080" }

function stripOf(name: RegExp | string) {
  const card = screen.getByText(name).closest("[data-card]") as HTMLElement
  return within(card).getByTestId("away-strip")
}

const review = (more: Partial<AwayReview>): AwayReview => ({ pass: 2, phase: "reviewing", blocking_open: 0, comments_open: 0, checks_open: 0, skipped_comments: 0, ...more })

describe("AwayLane", () => {
  it("groups away tasks by worker as the board's own cards, each under a header that opens the worker", () => {
    const cards = [away("T-1", "Fix login"), away("T-2", "Find the leak", { left_column: "Ready", state: "failed", failure: "the clone failed" })]
    render(<MemoryRouter><AwayLane workers={[vm1, vm2]} cards={cards} selectedIndex={-1} isActive={false} onRowClick={() => {}} /></MemoryRouter>)
    const lane = screen.getByRole("region", { name: "Away" })
    expect(within(lane).getByText("2")).toBeTruthy()

    const group = within(lane).getByRole("group", { name: "vm-1" })
    expect(within(group).getByRole("link", { name: "Open on vm-1" }).getAttribute("href")).toBe("https://vm-1.lan:3080")
    const card = within(group).getByText("Fix login").closest("[data-card]") as HTMLElement
    expect(within(card).getByTestId("card-source").textContent).toContain("local · T-1 → vm-1")
    expect(stripOf("Fix login").textContent).toMatch(/^◌Startingfrom Doing · /)
    expect(stripOf("Find the leak").textContent).toContain("Failed: the clone failed")
    expect((within(group).getByText("Find the leak").closest("[data-card]") as HTMLElement).dataset.edge).toBe("error")

    // A worker with nothing away still has its group.
    const empty = within(lane).getByRole("group", { name: "vm-2" })
    expect(within(empty).getByText(/Nothing away on vm-2/)).toBeTruthy()
  })

  it.each([
    [{ view: "starting" }, "Starting", undefined],
    [{ view: "working", activity: "working" }, "Running", undefined],
    [{ view: "needs_you", activity: "waiting" }, "Needs you", "needs"],
    [{ view: "idle" }, "Idle", undefined],
    [{ view: "ended" }, "Agent ended", undefined],
    [{ view: "interrupted" }, "Interrupted on worker", "needs"],
    [{ view: "done", state: "done" }, "Done", undefined],
    [{ view: "failed", failure: "no sandbox" }, "Failed: no sandbox", "error"],
    [{ view: "coming_back" }, "Coming back", undefined],
    [{ view: "unreachable", worker_unreachable_since: "2026-11-05T21:40:00Z" }, /^vm-1 unreachable since /, "error"],
    [{ view: "lost", lost: "it was deleted" }, "Lost on vm-1: it was deleted", "error"],
    [{ view: "not_accepted" }, "vm-1 no longer accepts this main", "error"],
  ])("says the away view %j in the card's words", (more, text, edge) => {
    render(<MemoryRouter><AwayLane workers={[vm1]} cards={[away("T-1", "Fix login", { state: "in_progress", ...more } as Partial<AwayOn>)]} selectedIndex={-1} isActive={false} onRowClick={() => {}} /></MemoryRouter>)
    expect(within(stripOf("Fix login")).getByText(text)).toBeTruthy()
    expect((screen.getByText("Fix login").closest("[data-card]") as HTMLElement).dataset.edge).toBe(edge)
  })

  it.each([
    [review({ phase: "reviewing" }), "Review pass 2 · reviewing", ""],
    [review({ phase: "waiting_for_reviewer" }), "Review pass 3 · waiting for a reviewer", ""],
    [review({ phase: "waiting_for_author", waiting_on: "author_working", blocking_open: 3, comments_open: 1, checks_open: 1 }), "Review pass 2 · author fixing", "3 blocking · 1 comment · 1 check"],
    [review({ phase: "waiting_for_author", blocking_open: 2 }), "Review pass 2 · author fixing", "2 blocking"],
    [review({ phase: "clean", checks_pending: 2 }), "Review approved · waiting for 2 checks", ""],
  ])("carries a review %j in the strip, with its open counts", (block, text, counts) => {
    render(<MemoryRouter><AwayLane workers={[vm1]} cards={[away("T-1", "Fix login", { state: "in_progress", view: "in_review", review: block })]} selectedIndex={-1} isActive={false} onRowClick={() => {}} /></MemoryRouter>)
    const strip = stripOf("Fix login")
    expect(within(strip).getByText(text)).toBeTruthy()
    if (counts) expect(within(strip).getByText(counts)).toBeTruthy()
  })

  it("says a review is done and approved, and a stuck one in full on an amber edge", () => {
    const { rerender } = render(<MemoryRouter><AwayLane workers={[vm1]} cards={[away("T-1", "Fix login", { state: "done", view: "done", review: review({ phase: "clean" }) })]} selectedIndex={-1} isActive={false} onRowClick={() => {}} /></MemoryRouter>)
    expect(within(stripOf("Fix login")).getByText("Review done · approved")).toBeTruthy()
    const reason = "the same finding three passes running, on #211's retry loop"
    rerender(<MemoryRouter><AwayLane workers={[vm1]} cards={[away("T-1", "Fix login", { state: "in_progress", view: "needs_you", review: review({ pass: 3, phase: "stuck", reason }) })]} selectedIndex={-1} isActive={false} onRowClick={() => {}} /></MemoryRouter>)
    expect(within(stripOf("Fix login")).getByText(`Review stuck after pass 3: ${reason}`)).toBeTruthy()
    expect((screen.getByText("Fix login").closest("[data-card]") as HTMLElement).dataset.edge).toBe("needs")
  })

  it("puts a card's actions on a wrapping line under its strip", () => {
    const actions = (card: BoardCard) => <><button type="button">Return as is</button><button type="button">Run another pass</button><button type="button">Message the author {card.id}</button></>
    render(<MemoryRouter><AwayLane workers={[vm1]} cards={[away("T-1", "Fix login")]} selectedIndex={-1} isActive={false} onRowClick={() => {}} rowActions={actions} /></MemoryRouter>)
    const bar = screen.getByRole("button", { name: "Return as is" }).parentElement!
    expect(bar.className).toContain("flex-wrap")
    expect(bar.previousElementSibling?.getAttribute("data-testid")).toBe("away-strip")
  })

  it("selects a card as the board does, and opens its menu", () => {
    const onRowClick = vi.fn()
    const onMenu = vi.fn()
    const cards = [away("T-1", "Fix login"), away("T-2", "Find the leak")]
    render(<MemoryRouter><AwayLane workers={[vm1]} cards={cards} selectedIndex={1} isActive onRowClick={onRowClick} onCardContextMenu={onMenu} /></MemoryRouter>)
    expect((screen.getByText("Find the leak").closest("[data-card]") as HTMLElement).dataset.selected).toBe("true")
    fireEvent.click(screen.getByText("Fix login"))
    expect(onRowClick).toHaveBeenCalledWith(0)
    fireEvent.contextMenu(screen.getByText("Fix login").closest("[data-card]")!, { clientX: 5, clientY: 6 })
    expect(onMenu).toHaveBeenCalledWith(cards[0], { x: 5, y: 6 })
    expect((screen.getByText("Fix login").closest("[data-card]") as HTMLElement).getAttribute("draggable")).toBe("false")
  })

  it("updates a card's strip and its group's reach as the worker's rows arrive", () => {
    const now = new Date("2026-11-05T22:00:00Z")
    const reach = (worker: BoardWorker) => <WorkerReach worker={worker} now={now} />
    const working = [away("T-1", "Fix login", { state: "in_progress", activity: "working", view: "working" })]
    const { rerender } = render(<MemoryRouter><AwayLane workers={[{ ...vm1, last_pull_at: "2026-11-05T21:58:00Z" }]} cards={working} selectedIndex={-1} isActive={false} onRowClick={() => {}} groupExtras={reach} /></MemoryRouter>)
    const group = screen.getByRole("group", { name: "vm-1" })
    expect(within(group).getByTestId("worker-reach").textContent).toBe("pulled 2 min ago")
    expect(within(group).getByTestId("away-strip").textContent).toContain("Running")

    const down = [away("T-1", "Fix login", { state: "in_progress", view: "unreachable", worker_unreachable_since: "2026-11-05T21:40:00Z" })]
    rerender(<MemoryRouter><AwayLane workers={[{ ...vm1, unreachable_since: "2026-11-05T21:40:00Z" }]} cards={down} selectedIndex={-1} isActive={false} onRowClick={() => {}} groupExtras={reach} /></MemoryRouter>)
    expect(within(group).getByTestId("worker-reach").textContent).toMatch(/^unreachable since /)
    expect(within(group).getByTestId("away-strip").textContent).toMatch(/vm-1 unreachable since /)
  })

  it("draws an away task's stack without a button: the worker's author fixes it", () => {
    const prs = [
      { repo: "acme/widget", branch: "b210", base: "main", pr_number: 210, pr_url: "", state: "OPEN", is_draft: false, review_decision: "APPROVED", check_status: "pass", comment_count: 0, pinned: false, match_method: "", sync_outcome: "", sync_error: "" },
      { repo: "acme/widget", branch: "b211", base: "b210", pr_number: 211, pr_url: "", state: "OPEN", is_draft: false, review_decision: "CHANGES_REQUESTED", check_status: "fail", comment_count: 0, pinned: false, match_method: "", sync_outcome: "", sync_error: "" },
    ]
    render(<MemoryRouter><AwayLane workers={[vm1]} cards={[{ ...away("T-1", "Fix login"), pull_requests: prs }]} selectedIndex={-1} isActive={false} onRowClick={() => {}} /></MemoryRouter>)
    expect(screen.getByText("Stack of 2 PRs")).toBeTruthy()
    expect(screen.getByTestId("stack-needs-you").textContent).toContain("#211CI failing")
    expect(screen.queryByRole("button", { name: /^Start / })).toBeNull()
  })
})
