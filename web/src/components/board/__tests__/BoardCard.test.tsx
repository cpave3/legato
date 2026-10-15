import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { BoardCard } from "../BoardCard"
import type { BoardCard as BoardCardType } from "../../../lib/board-types"

function makeCard(overrides: Partial<BoardCardType> = {}): BoardCardType {
  return {
    id: "T-1",
    title: "Test title",
    priority: "High",
    issue_type: "Bug",
    status: "Doing",
    provider: "jira",
    has_warning: false,
    agent_active: true,
    agent_state: "working",
    agent_kind: "claude-code",
    working_seconds: 3661,
    waiting_seconds: 125,
    error_seconds: 0,
    workspace_id: null, workspace_name: "Default",
    workspace_color: "#ff0000",
    pr_check_status: "pass",
    pr_review_decision: "APPROVED",
    pr_comment_count: 3,
    pr_is_draft: false,
    pr_number: 42,
    ...overrides,
  }
}

function renderCard(card: BoardCardType, props: Partial<Parameters<typeof BoardCard>[0]> = {}) {
  return render(<MemoryRouter><BoardCard card={card} selected={false} column="Doing" showWorkspace={false} onClick={() => {}} {...props} /></MemoryRouter>)
}

function strip() {
  return screen.getByTestId("agent-strip")
}

describe("BoardCard", () => {
  beforeEach(() => localStorage.clear())
  afterEach(cleanup)

  it("names where the task comes from and its key", () => {
    renderCard(makeCard())
    expect(screen.getByText("jira · T-1")).toBeTruthy()
    renderCard(makeCard({ id: "abc123", provider: "" }))
    expect(screen.getByText("local · abc123")).toBeTruthy()
  })

  it("shows a running agent's tool and how long it has worked", () => {
    renderCard(makeCard())
    expect(within(strip()).getByText("Running · Claude Code")).toBeTruthy()
    expect(within(strip()).getByText("1h 1m")).toBeTruthy()
  })

  it.each([
    ["waiting", "Needs you", "2m"],
    ["error", "Error", ""],
    ["background", "Background", ""],
    ["mixed", "Agents disagree", ""],
    ["", "Idle", ""],
  ])("says what an agent in state %j is doing", (state, label, time) => {
    renderCard(makeCard({ agent_state: state }))
    expect(within(strip()).getByText(label)).toBeTruthy()
    expect(within(strip()).queryByText(/Running/)).toBeNull()
    if (time) expect(within(strip()).getByText(time)).toBeTruthy()
  })

  it("has no agent strip without a running agent, and shows the time agents spent instead", () => {
    renderCard(makeCard({ agent_active: false, agent_state: "", working_seconds: 3600, waiting_seconds: 600 }))
    expect(screen.queryByTestId("agent-strip")).toBeNull()
    expect(screen.getByText("1h 10m")).toBeTruthy()
  })

  it("asks for a review when a ready tour has unreviewed changes, and links to it", () => {
    renderCard(makeCard({ agent_active: false, review_ready: true, review_unreviewed: 5, review_tour_id: "tour-9" }))
    const review = screen.getByTestId("review-strip")
    expect(within(review).getByText("5 changes to review")).toBeTruthy()
    expect(within(review).getByRole("link", { name: /Review/ }).getAttribute("href")).toBe("/review/tour-9")
  })

  it("says a ready tour with nothing left is reviewed", () => {
    renderCard(makeCard({ review_ready: true, review_unreviewed: 0 }))
    expect(screen.queryByTestId("review-strip")).toBeNull()
    expect(screen.getByText("Reviewed")).toBeTruthy()
  })

  it("describes the pull request in one line", () => {
    renderCard(makeCard({ pr_is_draft: true, pr_check_status: "fail", pr_review_decision: "CHANGES_REQUESTED", pr_comment_count: 12 }))
    const pr = screen.getByLabelText("Pull request #42")
    for (const text of ["#42", "draft", "CI failed", "changes requested", "12 comments"]) {
      expect(within(pr).getByText(text)).toBeTruthy()
    }
  })

  it.each([
    ["pass", "APPROVED", ["CI passed", "approved", "ready to merge"]],
    ["pending", "", ["CI pending", "review pending"]],
    ["", "REVIEW_REQUIRED", ["no checks", "review pending"]],
  ])("words CI %j and review %j", (checks, review, texts) => {
    renderCard(makeCard({ pr_check_status: checks, pr_review_decision: review, pr_comment_count: 0 }))
    const pr = screen.getByLabelText("Pull request #42")
    for (const text of texts) expect(within(pr).getByText(text)).toBeTruthy()
    expect(within(pr).queryByText(/comment/)).toBeNull()
  })

  it("never calls a draft ready to merge, and omits the line without a pull request", () => {
    renderCard(makeCard({ pr_is_draft: true }))
    expect(screen.queryByText("ready to merge")).toBeNull()
    cleanup()
    renderCard(makeCard({ pr_number: 0 }))
    expect(screen.queryByLabelText(/Pull request/)).toBeNull()
  })

  it("lists priority, type, workspace and swarm progress under the title", () => {
    renderCard(makeCard({ swarm_stats: { total: 5, done: 2, in_review: 1, building: 1, queued: 1, rejected: 0 } }), { showWorkspace: true })
    const meta = screen.getByTestId("card-meta")
    for (const text of ["High", "Bug", "Default", "2/5 workers"]) expect(within(meta).getByText(text)).toBeTruthy()
  })

  it("leaves the workspace out when the board shows one workspace", () => {
    renderCard(makeCard())
    expect(screen.queryByText("Default")).toBeNull()
  })

  it("warns when the last push to the provider failed", () => {
    renderCard(makeCard({ has_warning: true }))
    expect(screen.getByText("sync failed")).toBeTruthy()
  })

  it("marks the selected card, and gives an imported card a cyan highlight it scrolls to", () => {
    const scrollIntoView = vi.fn()
    Object.defineProperty(HTMLElement.prototype, "scrollIntoView", { configurable: true, value: scrollIntoView })
    renderCard(makeCard(), { selected: true, highlighted: true })
    const root = screen.getByText("Test title").closest("[data-card]") as HTMLElement
    expect(root.dataset.selected).toBe("true")
    expect(root.dataset.highlightedTask).toBe("true")
    expect(root.className).toContain("ring-cyan-400")
    expect(scrollIntoView).toHaveBeenCalledWith({ behavior: "smooth", block: "center", inline: "nearest" })
  })

  it("dims a card in the Done column", () => {
    renderCard(makeCard(), { column: "Done" })
    expect((screen.getByText("Test title").closest("[data-card]") as HTMLElement).className).toContain("opacity-60")
  })

  it("traces the edge of a card with a live agent, faster while it works, unless animation is off", () => {
    renderCard(makeCard({ agent_state: "working" }))
    const working = screen.getByText("Test title").closest("[data-card]") as HTMLElement
    expect(working.className).toContain("lw-trace-working")
    cleanup()
    renderCard(makeCard({ agent_state: "" }))
    expect((screen.getByText("Test title").closest("[data-card]") as HTMLElement).className).toContain("lw-trace-idle")
    cleanup()
    renderCard(makeCard({ agent_active: false, agent_state: "" }))
    expect((screen.getByText("Test title").closest("[data-card]") as HTMLElement).className).not.toContain("lw-trace")
    cleanup()
    localStorage.setItem("legato:animated-agent-activity", "false")
    renderCard(makeCard())
    expect((screen.getByText("Test title").closest("[data-card]") as HTMLElement).className).not.toContain("lw-trace")
  })

  it("opens the action menu from its button and from a right click", () => {
    const onActionMenu = vi.fn()
    const onContextMenu = vi.fn()
    renderCard(makeCard(), { onActionMenu, onContextMenu })
    fireEvent.click(screen.getByRole("button", { name: "Open actions for Test title" }))
    expect(onActionMenu).toHaveBeenCalledTimes(1)
    const card = screen.getByText("Test title").closest("[data-card]") as HTMLElement
    const event = new MouseEvent("contextmenu", { bubbles: true, cancelable: true, clientX: 120, clientY: 80 })
    card.dispatchEvent(event)
    expect(event.defaultPrevented).toBe(true)
    expect(onContextMenu).toHaveBeenCalledWith({ x: 120, y: 80 })
  })

  it("opens on a double click", () => {
    const onDoubleClick = vi.fn()
    renderCard(makeCard(), { onDoubleClick })
    fireEvent.doubleClick(screen.getByText("Test title"))
    expect(onDoubleClick).toHaveBeenCalledTimes(1)
  })
})

describe("BoardCard in a long column", () => {
  afterEach(cleanup)

  it("keeps its own height, so a column with many cards scrolls instead of squeezing them", () => {
    const { container } = renderCard(makeCard())
    // Columns lay their cards out as a flex column; a card that may shrink is
    // squeezed to a sliver once they no longer fit.
    expect((container.firstElementChild as HTMLElement).className).toMatch(/\bshrink-0\b/)
  })
})
