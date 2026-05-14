import { describe, expect, it, afterEach } from "vitest"
import { render, screen, cleanup } from "@testing-library/react"
import { BoardGrid } from "../BoardGrid"
import type { BoardColumn as BoardColumnType } from "../../../lib/board-types"

afterEach(() => {
  cleanup()
})

describe("BoardGrid", () => {
  it("renders one BoardColumn per column", () => {
    const columns: BoardColumnType[] = [
      { name: "Backlog", cards: [{ id: "T-1", title: "A", priority: "", issue_type: "", status: "Backlog", provider: "", has_warning: false, agent_active: false, agent_state: "", working_seconds: 0, waiting_seconds: 0, workspace_name: "", workspace_color: "", pr_check_status: "", pr_review_decision: "", pr_comment_count: 0, pr_is_draft: false, pr_number: 0 }] },
      { name: "Doing", cards: [{ id: "T-2", title: "B", priority: "", issue_type: "", status: "Doing", provider: "", has_warning: false, agent_active: false, agent_state: "", working_seconds: 0, waiting_seconds: 0, workspace_name: "", workspace_color: "", pr_check_status: "", pr_review_decision: "", pr_comment_count: 0, pr_is_draft: false, pr_number: 0 }] },
    ]
    render(
      <BoardGrid
        columns={columns}
        cursorCol={0}
        cursorRow={0}
        showWorkspace={false}
        onCardClick={() => {}}
      />
    )
    expect(screen.getByText(/Backlog/i)).toBeTruthy()
    expect(screen.getByText(/Doing/i)).toBeTruthy()
    expect(screen.getByText("A")).toBeTruthy()
    expect(screen.getByText("B")).toBeTruthy()
  })
})
