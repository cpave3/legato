import { describe, expect, it, afterEach, vi } from "vitest"
import { render, screen, cleanup, fireEvent } from "@testing-library/react"
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

  it("moves a dragged card when dropped on the column", () => {
    const onCardDrop = vi.fn()
    const data = new Map<string, string>()
    const dataTransfer = {
      dropEffect: "none",
      effectAllowed: "none",
      getData: (type: string) => data.get(type) ?? "",
      setData: (type: string, value: string) => data.set(type, value),
    }
    const { container } = render(
      <BoardColumn
        name="Doing"
        cards={[makeCard()]}
        selectedIndex={0}
        isActive={false}
        showWorkspace={false}
        onCardClick={() => {}}
        onCardDrop={onCardDrop}
        colIndex={0}
      />
    )

    fireEvent.dragStart(screen.getByText("Test title").closest("[draggable=true]")!, { dataTransfer })
    fireEvent.dragOver(container.firstElementChild!, { dataTransfer })
    fireEvent.drop(container.firstElementChild!, { dataTransfer })

    expect(onCardDrop).toHaveBeenCalledWith("T-1", "Doing")
  })
})
