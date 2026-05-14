import { describe, expect, it } from "vitest"
import { render, screen } from "@testing-library/react"
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
    workspace_name: "Default",
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

  it("shows IDLE when agent_active but no state", () => {
    const card = makeCard({ agent_state: "" })
    render(<BoardCard card={card} selected={false} column="Doing" showWorkspace={false} onClick={() => {}} />)
    expect(screen.getByText(/IDLE/)).toBeTruthy()
  })

  it("applies muted style in Done column", () => {
    const card = makeCard()
    const { container } = render(
      <BoardCard card={card} selected={false} column="Done" showWorkspace={false} onClick={() => {}} />
    )
    const root = container.querySelector(".opacity-60")
    expect(root).not.toBeNull()
  })
})
