import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router-dom"
import { BoardCard } from "../BoardCard"
import type { BoardCard as BoardCardType, PRMetaView } from "../../../lib/board-types"
import { leg386, pr as stackPR } from "../../../lib/__tests__/fixtures/prStacks"

function pr(number: number, overrides: Partial<PRMetaView> = {}): PRMetaView {
  return {
    repo: "acme/app", branch: `branch-${number}`, pr_number: number, pr_url: `https://github.com/acme/app/pull/${number}`,
    state: "OPEN", is_draft: false, review_decision: "APPROVED", check_status: "pass", comment_count: 3,
    pinned: false, match_method: "branch", sync_outcome: "linked", sync_error: "",
    ...overrides,
  }
}

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
    pull_requests: [pr(42)],
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

  it("says which main instance handed a card to this worker", () => {
    renderCard(makeCard({ handoff_from: { main: "workstation", main_task_id: "K7Q2M9XA" } }))
    expect(screen.getByText("from workstation · K7Q2M9XA")).toBeTruthy()
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

  it("describes the pull request in one line: its linked number, one word for its state and a detail", () => {
    renderCard(makeCard({ pull_requests: [pr(191, { review_decision: "", comment_count: 0 })] }))
    const line = screen.getByLabelText("Pull request #191")
    expect(line.textContent).toBe("#191Awaiting review·CI passed")
    expect(within(line).getByRole("link", { name: "#191" }).getAttribute("href")).toBe("https://github.com/acme/app/pull/191")
  })

  it.each([
    [{ state: "MERGED", check_status: "fail" }, "Merged"],
    [{ state: "CLOSED" }, "Closed, not merged"],
    [{ is_draft: true }, "Draft"],
    [{ check_status: "pending" }, "CI running"],
    [{ comment_count: 0 }, "Ready to merge"],
  ])("words a PR %j as %j, with no button", (overrides, word) => {
    renderCard(makeCard({ pull_requests: [pr(42, overrides)] }))
    const line = screen.getByLabelText("Pull request #42")
    expect(within(line).getByText(word)).toBeTruthy()
    expect(within(line).queryByRole("button")).toBeNull()
    expect(screen.queryByText("ready to merge")).toBeNull()
  })

  it("strikes through a closed PR's number", () => {
    renderCard(makeCard({ pull_requests: [pr(42, { state: "CLOSED" })] }))
    expect(screen.getByRole("link", { name: "#42" }).className).toContain("line-through")
  })

  it("puts a PR that needs the operator in a box on its wash", () => {
    renderCard(makeCard({ pull_requests: [pr(200, { check_status: "fail", comment_count: 6 })] }))
    const line = screen.getByLabelText("Pull request #200")
    expect(line.getAttribute("data-needs-you")).toBe("error")
    expect(line.textContent).toContain("CI failing · 6 comments")
  })

  it("folds a stack: its counts, a bar bottom first, the layer that needs you and the run that can merge", () => {
    renderCard(makeCard({ pull_requests: leg386() }))
    const stack = screen.getByTestId("pr-stack")
    expect(within(stack).getByText("Stack of 13 PRs")).toBeTruthy()
    expect(within(stack).getByText("2 merged")).toBeTruthy()
    const bar = within(stack).getByRole("img")
    expect(bar.getAttribute("aria-label")).toBe("2 merged, 4 ready to merge, 1 failing, 1 with CI running, 5 waiting on a layer below")
    expect(Array.from(bar.children).map(seg => seg.getAttribute("data-segment"))).toEqual(["merged", "merged", "ready", "ready", "ready", "ready", "failing", "held", "held", "running", "held", "held", "held"])
    const needs = within(stack).getByTestId("stack-needs-you")
    expect(needs.textContent).toContain("#200CI failing · 6 comments")
    expect(needs.textContent).toContain("Holds back 6 above it")
    expect(stack.textContent).toContain("#196–#199 ready to merge, bottom first")
    expect(screen.queryAllByLabelText(/^Pull request #/)).toEqual([])
  })

  it("opens a stack bottom first on a rail, dashed above the failing layer, and remembers it", () => {
    renderCard(makeCard({ id: "T-open", pull_requests: leg386() }))
    fireEvent.click(screen.getByRole("button", { name: "Show all 13" }))
    const rows = screen.getAllByRole("listitem")
    expect(rows.map(row => row.getAttribute("aria-label"))).toEqual(leg386().map(pr => `Pull request #${pr.pr_number}`))
    expect(within(rows[0]).getByText("Merged")).toBeTruthy()
    expect(within(rows[6]).getByText("CI failing")).toBeTruthy()
    expect(within(rows[7]).getByText("Approved")).toBeTruthy()
    expect(rows.slice(7).every(row => row.hasAttribute("data-held"))).toBe(true)
    expect(rows.slice(0, 7).some(row => row.hasAttribute("data-held"))).toBe(false)
    expect(within(rows[2]).getByRole("link", { name: "#196" }).getAttribute("href")).toContain("/pull/196")
    expect(screen.getByText("cpave3/legato · main")).toBeTruthy()
    expect(screen.getByTestId("stack-needs-you").textContent).toContain("#200 holds back 6")
    cleanup()
    renderCard(makeCard({ id: "T-open", pull_requests: leg386() }))
    expect(screen.getByRole("button", { name: "Show less" }).getAttribute("aria-expanded")).toBe("true")
    fireEvent.click(screen.getByRole("button", { name: "Show less" }))
    expect(screen.getByRole("button", { name: "Show all 13" })).toBeTruthy()
  })

  it("groups a stack by repo, indents a fork and strikes through a closed layer", () => {
    renderCard(makeCard({ id: "T-fork", pull_requests: [
      stackPR(10, "merged", "main", { repo: "acme/api" }),
      stackPR(11, "closed", "main", { repo: "acme/api" }),
      stackPR(12, "approved", "b10", { repo: "acme/api" }),
      stackPR(13, "run", "b10", { repo: "acme/api" }),
      stackPR(4, "draft", "main", { repo: "acme/web" }),
    ] }))
    expect(screen.getByText("5 PRs in 2 repos")).toBeTruthy()
    expect(screen.getByText("1 merged · 1 closed")).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Show all 5" }))
    expect(screen.getAllByRole("list").map(list => list.getAttribute("aria-label"))).toEqual(["acme/api pull requests", "acme/web pull requests"])
    expect(screen.getByText("acme/web · main")).toBeTruthy()
    const rows = screen.getAllByRole("listitem")
    expect(rows.filter(row => row.hasAttribute("data-fork")).map(row => row.getAttribute("aria-label"))).toEqual(["Pull request #12", "Pull request #13"])
    expect(within(rows[1]).getByRole("link", { name: "#11" }).className).toContain("line-through")
    expect(within(rows[1]).getByText("Closed, not merged")).toBeTruthy()
  })

  it("says a fully merged stack in one line", () => {
    renderCard(makeCard({ pull_requests: [stackPR(1, "merged", "main"), stackPR(2, "merged", "main"), stackPR(3, "merged", "main")] }))
    expect(screen.getByTestId("pr-stack").textContent).toBe("✓Stack of 3 PRs, all merged")
    expect(screen.queryByRole("button", { name: /Show all/ })).toBeNull()
  })

  it("says a stack of drafts is drafts, with no layer that needs you", () => {
    renderCard(makeCard({ pull_requests: [stackPR(1, "draft", "main"), stackPR(2, "draft", "b1"), stackPR(3, "draft", "b2")] }))
    expect(screen.getByText("3 drafts")).toBeTruthy()
    expect(screen.getByText("Drafts. CI passing on all 3.")).toBeTruthy()
    expect(screen.queryByTestId("stack-needs-you")).toBeNull()
  })

  it("omits the pull request lines without one", () => {
    renderCard(makeCard({ pull_requests: undefined }))
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

  it("offers a keyboard primary action without nesting the menu or review link", async () => {
    const user = userEvent.setup()
    const onClick = vi.fn()
    const onDoubleClick = vi.fn()
    const onActionMenu = vi.fn()
    renderCard(makeCard({ review_unreviewed: 1, review_tour_id: "tour" }), { onClick, onDoubleClick, onActionMenu })
    const primary = screen.getByRole("button", { name: "Test title" })
    expect(primary.querySelector("button, a")).toBeNull()
    primary.focus()
    await user.keyboard("{Enter}")
    expect(onDoubleClick).toHaveBeenCalledTimes(1)
    await user.keyboard(" ")
    expect(onClick).toHaveBeenCalledTimes(1)
    await user.click(screen.getByRole("button", { name: "Open actions for Test title" }))
    expect(onActionMenu).toHaveBeenCalledTimes(1)
    expect(onClick).toHaveBeenCalledTimes(1)
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

  it("shows a chip for each running named slot beside the main agent", () => {
    renderCard(makeCard({ slot_agents: [{ slot: "reviewer", state: "working" }] }))
    const strip = screen.getByTestId("agent-strip")
    expect(strip.textContent).toContain("Running")
    expect(within(strip).getByText("reviewer · running")).toBeTruthy()
  })
})
