import { describe, expect, it, afterEach } from "vitest"
import { render, screen, cleanup } from "@testing-library/react"
import { BoardColumn } from "../BoardColumn"
import type { BoardCard as BoardCardType } from "../../../lib/board-types"

afterEach(() => {
  cleanup()
})

function makeCard(overrides: Partial<BoardCardType> = {}): BoardCardType {
  return {
    id: "T-1",
    title: "Test title",
    priority: "",
    issue_type: "",
    status: "Backlog",
    provider: "",
    has_warning: false,
    agent_active: false,
    agent_state: "",
    working_seconds: 0,
    waiting_seconds: 0,
    workspace_name: "",
    workspace_color: "",
    pr_check_status: "",
    pr_review_decision: "",
    pr_comment_count: 0,
    pr_is_draft: false,
    pr_number: 0,
    ...overrides,
  }
}

describe("BoardColumn", () => {
  it("renders header with column name and card count", () => {
    render(
      <BoardColumn
        name="Doing"
        cards={[makeCard(), makeCard({ id: "T-2" })]}
        selectedIndex={0}
        isActive={false}
        showWorkspace={false}
        onCardClick={() => {}}
        colIndex={0}
      />
    )
    expect(screen.getByText(/Doing/i)).toBeTruthy()
    expect(screen.getByText("2")).toBeTruthy()
  })
})
