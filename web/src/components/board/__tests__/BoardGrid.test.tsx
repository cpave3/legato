import { describe, expect, it, afterEach, vi } from "vitest"
import { render, screen, cleanup, fireEvent } from "@testing-library/react"
import { BoardGrid } from "../BoardGrid"
import type { BoardCard, BoardColumn as BoardColumnType } from "../../../lib/board-types"

afterEach(() => {
  cleanup()
})

function card(id: string, title: string, status: string): BoardCard {
  return { id, title, priority: "", issue_type: "", status, provider: "", has_warning: false, agent_active: false, agent_state: "", working_seconds: 0, waiting_seconds: 0, error_seconds: 0, workspace_id: null, workspace_name: "", workspace_color: "", pr_check_status: "", pr_review_decision: "", pr_comment_count: 0, pr_is_draft: false, pr_number: 0 }
}

const columns: BoardColumnType[] = [
  { name: "Backlog", cards: [card("T-1", "A", "Backlog")] },
  { name: "Ready", cards: [] },
  { name: "Doing", cards: [card("T-2", "B", "Doing")] },
]

function renderGrid(props: Partial<Parameters<typeof BoardGrid>[0]> = {}) {
  return render(<BoardGrid columns={columns} cursorCol={0} cursorRow={0} showWorkspace={false} onCardClick={() => {}} {...props} />)
}

describe("BoardGrid", () => {
  it("renders every column with its cards", () => {
    renderGrid({ showEmpty: true })
    for (const name of ["Backlog", "Ready", "Doing"]) expect(screen.getByRole("region", { name })).toBeTruthy()
    expect(screen.getByText("A")).toBeTruthy()
    expect(screen.getByText("B")).toBeTruthy()
  })

  it("folds empty columns into rails, which open them all again", () => {
    const onToggleEmpty = vi.fn()
    renderGrid({ showEmpty: false, onToggleEmpty })
    expect(screen.queryByRole("region", { name: "Ready" })).toBeNull()
    fireEvent.click(screen.getByRole("button", { name: "Ready, 0 cards" }))
    expect(onToggleEmpty).toHaveBeenCalledTimes(1)
  })

  it("keeps the columns of an empty board open", () => {
    renderGrid({ showEmpty: false, columns: [{ name: "Backlog", cards: [] }, { name: "Done", cards: [] }] })
    expect(screen.getByRole("region", { name: "Backlog" })).toBeTruthy()
    expect(screen.getByRole("region", { name: "Done" })).toBeTruthy()
  })
})
