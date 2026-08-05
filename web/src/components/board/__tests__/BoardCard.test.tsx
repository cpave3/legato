import { beforeEach, describe, expect, it, vi } from "vitest"
import { fireEvent, render, screen } from "@testing-library/react"
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
    has_warning: true,
    agent_active: true,
    agent_state: "working",
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
    swarm_stats: { total: 5, done: 2, in_review: 1, building: 1, queued: 1, rejected: 0 },
    ...overrides,
  }
}

describe("BoardCard", () => {
  beforeEach(() => localStorage.clear())

  it("renders all indicators when given populated props", () => {
    const card = makeCard()
    render(<BoardCard card={card} selected={false} column="Doing" showWorkspace={true} onClick={() => {}} />)

    expect(screen.getByText("Test title")).toBeTruthy()
    expect(screen.getByText("T-1")).toBeTruthy()
    expect(screen.getByText(/RUNNING/)).toBeTruthy()
    expect(screen.getByText("High")).toBeTruthy()
    expect(screen.getByText("Bug")).toBeTruthy()
    expect(screen.getByText("2/5")).toBeTruthy()
  })

  it("outlines a card with an active agent without replacing its priority stripe", () => {
    const { container } = render(
      <BoardCard card={makeCard()} selected={false} column="Doing" showWorkspace={false} onClick={() => {}} />
    )

    const root = container.firstElementChild as HTMLElement
    expect(root.className).toContain("ring-1")
    expect(root.className).toContain("ring-inset")
    expect(root.className).toContain("ring-indigo-500/60")
    expect(root.className).toContain("border-l-[#E06C75]")
  })

  it("animates active cards slowly when idle and faster while working", () => {
    const idle = render(
      <BoardCard card={makeCard({ agent_state: "" })} selected={false} column="Doing" showWorkspace={false} onClick={() => {}} />
    )
    const idleRoot = idle.container.firstElementChild as HTMLElement
    expect(idleRoot.className).toContain("agent-activity-orbit")
    expect(idleRoot.className).toContain("agent-activity-orbit-idle")
    expect(idleRoot.className).not.toContain("agent-activity-orbit-working")
    idle.unmount()

    const working = render(
      <BoardCard card={makeCard({ agent_state: "working" })} selected={false} column="Doing" showWorkspace={false} onClick={() => {}} />
    )
    const workingRoot = working.container.firstElementChild as HTMLElement
    expect(workingRoot.className).toContain("agent-activity-orbit")
    expect(workingRoot.className).toContain("agent-activity-orbit-working")
  })

  it("keeps the static ring without animation when the preference is disabled", () => {
    localStorage.setItem("legato:animated-agent-activity", "false")
    const { container } = render(
      <BoardCard card={makeCard()} selected={false} column="Doing" showWorkspace={false} onClick={() => {}} />
    )

    const root = container.firstElementChild as HTMLElement
    expect(root.className).toContain("ring-indigo-500/60")
    expect(root.className).not.toContain("agent-activity-orbit")
    expect(root.className).not.toContain("agent-activity-orbit-working")
  })

  it("hides workspace tag when showWorkspace is false", () => {
    const card = makeCard()
    const { container } = render(
      <BoardCard card={card} selected={false} column="Doing" showWorkspace={false} onClick={() => {}} />
    )
    const dot = container.querySelector('[style*="background-color: rgb(255, 0, 0)"]')
    expect(dot).toBeNull()
  })

  it("renders draft pill instead of CI when PR is draft", () => {
    const card = makeCard({ pr_is_draft: true, pr_check_status: "" })
    render(<BoardCard card={card} selected={false} column="Doing" showWorkspace={false} onClick={() => {}} />)
    expect(screen.getByText("Draft")).toBeTruthy()
  })

  it("shows MIXED instead of idle when related agents disagree", () => {
    render(<BoardCard card={makeCard({ agent_state: "mixed" })} selected={false} column="Doing" showWorkspace={false} onClick={() => {}} />)
    expect(screen.getByText(/MIXED/)).toBeTruthy()
    expect(screen.queryByText(/IDLE/)).toBeNull()
  })

  it("shows ERROR in red instead of idle for an errored agent", () => {
    const card = makeCard({ agent_state: "error" })
    render(<BoardCard card={card} selected={false} column="Doing" showWorkspace={false} onClick={() => {}} />)
    const error = screen.getByText(/ERROR/)
    expect(error.className).toContain("text-red-400")
    expect(screen.queryByText(/IDLE/)).toBeNull()
  })

  it("shows IDLE and the outline when agent_active but no state", () => {
    const card = makeCard({ agent_state: "" })
    const { container } = render(
      <BoardCard card={card} selected={false} column="Doing" showWorkspace={false} onClick={() => {}} />
    )

    expect(screen.getByText(/IDLE/)).toBeTruthy()
    expect((container.firstElementChild as HTMLElement).className).toContain("ring-indigo-500/60")
  })

  it("does not outline an inactive card with historical agent durations", () => {
    const card = makeCard({ agent_active: false, agent_state: "", working_seconds: 3661, waiting_seconds: 125 })
    const { container } = render(
      <BoardCard card={card} selected={false} column="Doing" showWorkspace={false} onClick={() => {}} />
    )

    const root = container.firstElementChild as HTMLElement
    expect(root.className).not.toContain("ring-1")
    expect(root.className).not.toContain("ring-indigo-500/60")
    expect(root.className).not.toContain("agent-activity-orbit")
  })

  it("shows review state and unreviewed count", () => {
    const card = makeCard({ review_ready: true, review_unreviewed: 3 })
    render(<BoardCard card={card} selected={false} column="Doing" showWorkspace={false} onClick={() => {}} />)
    expect(screen.getByText("Review 3")).toBeTruthy()
  })

  it("shows a reviewed badge when a ready tour has no remaining steps", () => {
    const card = makeCard({ review_ready: true, review_unreviewed: 0 })
    render(<BoardCard card={card} selected={false} column="Doing" showWorkspace={false} onClick={() => {}} />)
    expect(screen.getByText("Reviewed")).toBeTruthy()
  })

  it("applies muted style in Done column", () => {
    const card = makeCard()
    const { container } = render(
      <BoardCard card={card} selected={false} column="Done" showWorkspace={false} onClick={() => {}} />
    )
    const root = container.querySelector(".opacity-60")
    expect(root).not.toBeNull()
  })

  it("opens the same action path from the accessible card button", () => {
    const onActionMenu = vi.fn()
    render(<BoardCard card={makeCard()} selected={false} column="Doing" showWorkspace={false} onClick={() => {}} onActionMenu={onActionMenu} />)
    fireEvent.click(screen.getByRole("button", { name: "Open actions for Test title" }))
    expect(onActionMenu).toHaveBeenCalledTimes(1)
  })

  it("emits card context coordinates and prevents the browser menu", () => {
    const onContextMenu = vi.fn()
    const { container } = render(<BoardCard card={makeCard()} selected={false} column="Doing" showWorkspace={false} onClick={() => {}} onContextMenu={onContextMenu} />)
    const card = container.firstElementChild as HTMLElement
    const event = new MouseEvent("contextmenu", { bubbles: true, cancelable: true, clientX: 120, clientY: 80 })

    card.dispatchEvent(event)

    expect(event.defaultPrevented).toBe(true)
    expect(onContextMenu).toHaveBeenCalledWith({ x: 120, y: 80 })
  })
})
