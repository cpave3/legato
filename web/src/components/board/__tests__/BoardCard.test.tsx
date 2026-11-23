import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router-dom"
import { BoardCard } from "../BoardCard"
import type { BoardCard as BoardCardType, PRMetaView } from "../../../lib/board-types"
import { getRailStateForTests as railState } from "../../../lib/railState"
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

  it.each([
    [{ provider: "", id: "f41p6uf0" }, "local · f41p6uf0"],
    [{ provider: "jira", id: "k3x9", remote_id: "AG-7467" }, "Jira · AG-7467"],
    [{ provider: "burnlite", id: "k3x9q2ma", remote_id: "LEG-386" }, "Burnlite · LEG-386"],
    [{ provider: "burnlite-copy", id: "w-1", remote_id: "LEG-374", handoff_from: { main: "erebus", main_task_id: "K7Q2M9XA" } }, "Burnlite · LEG-374 ← erebus"],
  ])("names the source and the ticket's key: %j", (overrides, text) => {
    renderCard(makeCard(overrides as Partial<BoardCardType>))
    expect(within(screen.getByTestId("card-source")).getByText(text)).toBeTruthy()
  })

  it("leaves out a worker's workspace named after its main", () => {
    renderCard(makeCard({ provider: "burnlite-copy", remote_id: "LEG-374", workspace_name: "erebus", handoff_from: { main: "erebus", main_task_id: "K7Q2M9XA" } }), { showWorkspace: true })
    expect(within(screen.getByTestId("card-meta")).queryByText("erebus")).toBeNull()
  })

  it("tags a Burnlite Milestone in the header, not the meta line", () => {
    renderCard(makeCard({ provider: "burnlite", remote_id: "LEG-386", issue_type: "Milestone", priority: "" }))
    expect(within(screen.getByTestId("card-source")).getByText("Milestone")).toBeTruthy()
    expect(screen.queryByTestId("card-meta")).toBeNull()
  })

  it("says what blocks a Burnlite task, its milestone and its labels", () => {
    renderCard(makeCard({ provider: "burnlite", remote_id: "LEG-406", priority: "", issue_type: "Work", blocked_by: ["LEG-405"], milestone: "LEG-404", labels: ["ui", "enhancement"] }))
    const meta = screen.getByTestId("card-meta")
    expect(meta.textContent).toBe("Work·Blocked by LEG-405·LEG-404·ui·enhancement")
    expect(within(meta).getByText("Blocked by LEG-405").className).toContain("text-lw-error")
  })

  it("lets a long title wrap anywhere, and stops it at three lines on a phone", () => {
    renderCard(makeCard({ title: "a-very-long-branch-name-without-any-spaces-at-all" }))
    const title = screen.getByRole("button", { name: /a-very-long/ })
    expect(title.className).toContain("[overflow-wrap:anywhere]")
    expect(title.className).toContain("max-md:line-clamp-3")
  })

  it("says a running agent is working and how long it has worked, its tool only in the tooltip", () => {
    renderCard(makeCard())
    expect(within(strip()).getByText("Working")).toBeTruthy()
    expect(within(strip()).queryByText(/Claude Code/)).toBeNull()
    expect(strip().getAttribute("title")).toBe("Claude Code")
    expect(within(strip()).getByText("1h 1m")).toBeTruthy()
  })

  it.each([
    ["waiting", "Needs you", "2m"],
    ["error", "Error", ""],
    ["background", "Working · background", ""],
    ["mixed", "Agents disagree", ""],
    ["", "Your turn", "1h 3m"],
  ])("says what an agent in state %j is doing", (state, label, time) => {
    renderCard(makeCard({ agent_state: state }))
    expect(within(strip()).getByText(label)).toBeTruthy()
    expect(within(strip()).queryByText(/Running|Idle/)).toBeNull()
    if (time) expect(within(strip()).getByText(time)).toBeTruthy()
  })

  it("says why a card needs you: the agent's open items, with the way to them", () => {
    renderCard(makeCard({ agent_state: "", needs_you: { items: { question: 1, action: 1 } } }))
    const needs = screen.getByTestId("needs-you-strip")
    expect(within(needs).getByText("Needs you · 1 question, 1 action")).toBeTruthy()
    expect(within(needs).getByRole("link", { name: "Open" }).getAttribute("href")).toBe("/agents?task=T-1")
    expect(screen.queryByTestId("agent-strip")).toBeNull()
  })

  it("says an approval waits, and answers it in the rail", () => {
    renderCard(makeCard({ agent_state: "waiting", needs_you: { approvals: { count: 1, id: "ap-1", label: "permission to run a command" }, items: { question: 2 } } }))
    const needs = screen.getByTestId("needs-you-strip")
    expect(within(needs).getByText("Needs you · permission to run a command")).toBeTruthy()
    fireEvent.click(within(needs).getByRole("button", { name: "Answer" }))
    expect(railState().wanted).toBe("ap-1")
    expect(screen.queryByTestId("agent-strip")).toBeNull()
  })

  it("says a plan waits for approval, with a link to it", () => {
    renderCard(makeCard({ agent_state: "", needs_you: { plans: { count: 1, plan_id: "plan-9" } } }))
    const needs = screen.getByTestId("needs-you-strip")
    expect(within(needs).getByText("Plan to approve")).toBeTruthy()
    expect(within(needs).getByRole("link", { name: "Review plan" }).getAttribute("href")).toBe("/plans/plan-9")
  })

  it("says a reviewer needs you above the main agent's own running strip", () => {
    const { container } = renderCard(makeCard({ agent_state: "working", agent_engram: "hestia", slot_agents: [{ slot: "reviewer", state: "waiting" }], needs_you: { slots: [{ slot: "reviewer", label: "question", approval_id: "ap-r" }] } }))
    const strips = Array.from(container.querySelectorAll("[data-testid$='-strip']"))
    expect(strips.map(strip => strip.getAttribute("data-testid"))).toEqual(["needs-you-strip", "agent-strip"])
    expect(within(strips[0] as HTMLElement).getByText("Reviewer needs you · question")).toBeTruthy()
    fireEvent.click(within(strips[0] as HTMLElement).getByRole("button", { name: "Answer" }))
    expect(railState().wanted).toBe("ap-r")
    expect(within(strip()).getByText("Working")).toBeTruthy()
    expect(strip().getAttribute("title")).toBe("Claude Code · hestia")
    expect(within(strip()).getByText("1h 1m")).toBeTruthy()
    expect(within(strip()).queryByText(/reviewer/)).toBeNull()
  })

  it("says one of two agents needs you above the one that runs", () => {
    renderCard(makeCard({ agent_state: "mixed", agent_kind: "codex", needs_you: { agents: 2, waiting_agents: 1, working_agents: 1 } }))
    const needs = screen.getByTestId("needs-you-strip")
    expect(within(needs).getByText("1 of 2 agents needs you")).toBeTruthy()
    expect(within(needs).getByRole("link", { name: "Open" }).getAttribute("href")).toBe("/agents?task=T-1")
    expect(within(strip()).getByText("1 working")).toBeTruthy()
    expect(strip().getAttribute("title")).toBe("Codex")
    expect(screen.queryByText("Agents disagree")).toBeNull()
  })

  it("offers to resume an interrupted agent", async () => {
    const fetch = vi.fn(async () => new Response(JSON.stringify({ task_id: "T-1", resumed: true })))
    vi.stubGlobal("fetch", fetch)
    renderCard(makeCard({ agent_active: false, agent_state: "", needs_you: { interrupted: { agent_kind: "claude-code", resumable: true } } }))
    const needs = screen.getByTestId("needs-you-strip")
    expect(within(needs).getByText("Interrupted · Claude Code")).toBeTruthy()
    fireEvent.click(within(needs).getByRole("button", { name: "Resume" }))
    await vi.waitFor(() => expect(fetch).toHaveBeenCalled())
    const [url, init] = fetch.mock.calls[0] as unknown as [string, RequestInit]
    expect(String(url)).toContain("/api/agents/T-1/resume")
    expect(init.method).toBe("POST")
    vi.unstubAllGlobals()
  })

  it("draws what waits on you above what runs", () => {
    const { container } = renderCard(makeCard({ needs_you: { items: { question: 1 } }, review_ready: true, review_unreviewed: 4, review_tour_id: "tour-1" }))
    const strips = Array.from(container.querySelectorAll("[data-testid$='-strip']")).map(strip => strip.getAttribute("data-testid"))
    expect(strips).toEqual(["needs-you-strip", "review-strip", "agent-strip"])
    expect(within(screen.getByTestId("review-strip")).getByRole("link", { name: "Review" }).getAttribute("href")).toBe("/review/tour-1")
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
    expect(within(review).getByRole("link", { name: "Review" }).getAttribute("href")).toBe("/review/tour-9")
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

  it("marks the selected card, and gives an imported card a highlight ring it scrolls to", () => {
    const scrollIntoView = vi.fn()
    Object.defineProperty(HTMLElement.prototype, "scrollIntoView", { configurable: true, value: scrollIntoView })
    renderCard(makeCard(), { selected: true, highlighted: true })
    const root = screen.getByText("Test title").closest("[data-card]") as HTMLElement
    expect(root.dataset.selected).toBe("true")
    expect(root.dataset.highlightedTask).toBe("true")
    expect(root.className).toContain("lw-highlight")
    expect(scrollIntoView).toHaveBeenCalledWith({ behavior: "smooth", block: "center", inline: "nearest" })
  })

  it("steps a Done card's title down to secondary ink without fading the card", () => {
    renderCard(makeCard(), { column: "Done" })
    const root = screen.getByText("Test title").closest("[data-card]") as HTMLElement
    expect(root.className).not.toContain("opacity")
    expect(screen.getByRole("button", { name: "Test title" }).className).toContain("text-lw-text-2")
  })

  it("traces the edge only while an agent works, unless animation is off", () => {
    renderCard(makeCard({ agent_state: "working" }))
    const working = screen.getByText("Test title").closest("[data-card]") as HTMLElement
    expect(working.className).toContain("lw-trace-working")
    cleanup()
    for (const state of ["", "waiting", "error", "background", "mixed"]) {
      renderCard(makeCard({ agent_state: state }))
      expect((screen.getByText("Test title").closest("[data-card]") as HTMLElement).className).not.toContain("lw-trace")
      cleanup()
    }
    localStorage.setItem("legato:animated-agent-activity", "false")
    renderCard(makeCard())
    expect((screen.getByText("Test title").closest("[data-card]") as HTMLElement).className).not.toContain("lw-trace")
  })

  it.each([
    [{ agent_state: "waiting" }, "needs", "border-lw-needs"],
    [{ agent_state: "", needs_you: { plans: { count: 1, plan_id: "p" } } }, "needs", "border-lw-needs"],
    [{ agent_active: false, agent_state: "", needs_you: { interrupted: { resumable: true } } }, "needs", "border-lw-needs"],
    [{ agent_state: "error" }, "error", "border-lw-error"],
    [{ agent_state: "working" }, undefined, "border-lw-line"],
  ])("gives a card %j a steady edge for what it needs", (overrides, edge, border) => {
    renderCard(makeCard(overrides as Partial<BoardCardType>))
    const root = screen.getByText("Test title").closest("[data-card]") as HTMLElement
    expect(root.dataset.edge).toBe(edge)
    expect(root.className).toContain(border)
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
    expect(strip.textContent).toContain("Working")
    expect(within(strip).getByText("reviewer ● working")).toBeTruthy()
  })
})
