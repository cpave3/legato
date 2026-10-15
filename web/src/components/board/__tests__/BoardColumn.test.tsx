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
    error_seconds: 0,
    workspace_id: null, workspace_name: "",
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

  it("marks the header of the column the cursor is in", () => {
    const props = { name: "Doing", cards: [makeCard()], selectedIndex: 0, showWorkspace: false, onCardClick: () => {}, colIndex: 0 }
    const { rerender } = render(<BoardColumn {...props} isActive={false} />)
    expect(screen.getByText("Doing").closest("[data-column-header]")!.getAttribute("data-active")).toBeNull()
    rerender(<BoardColumn {...props} isActive />)
    expect(screen.getByText("Doing").closest("[data-column-header]")!.getAttribute("data-active")).toBe("true")
  })

  it("draws a collapsed column as a rail that names it and opens the empty columns", () => {
    const onExpand = vi.fn()
    render(<BoardColumn name="Ready" cards={[]} collapsed onExpand={onExpand} selectedIndex={0} isActive={false} showWorkspace={false} onCardClick={() => {}} colIndex={1} />)
    const rail = screen.getByRole("button", { name: "Ready, 0 cards" })
    fireEvent.click(rail)
    expect(onExpand).toHaveBeenCalledTimes(1)
  })

  it("still takes a dropped card when collapsed", () => {
    const onCardDrop = vi.fn()
    const dataTransfer = { dropEffect: "none", getData: () => "T-9" }
    render(<BoardColumn name="Ready" cards={[]} collapsed onExpand={() => {}} onCardDrop={onCardDrop} selectedIndex={0} isActive={false} showWorkspace={false} onCardClick={() => {}} colIndex={1} />)
    const rail = screen.getByRole("button", { name: "Ready, 0 cards" })
    fireEvent.dragOver(rail, { dataTransfer })
    fireEvent.drop(rail, { dataTransfer })
    expect(onCardDrop).toHaveBeenCalledWith("T-9", "Ready")
  })

  it("keeps the header fixed while the card list owns vertical scrolling", () => {
    const { container } = render(
      <BoardColumn
        name="Doing"
        cards={[makeCard()]}
        selectedIndex={0}
        isActive={false}
        showWorkspace={false}
        onCardClick={() => {}}
        colIndex={0}
      />
    )

    const column = container.firstElementChild!
    const header = screen.getByText("Doing").closest("[data-column-header]")!
    const cardList = screen.getByText("Test title").closest("[data-board-column-cards]")!

    expect(column.classList.contains("h-full")).toBe(true)
    expect(column.classList.contains("min-h-0")).toBe(true)
    expect(header.contains(cardList)).toBe(false)
    expect(cardList.classList.contains("min-h-0")).toBe(true)
    expect(cardList.classList.contains("flex-1")).toBe(true)
    expect(cardList.classList.contains("overflow-y-auto")).toBe(true)
    expect(cardList.classList.contains("min-w-0")).toBe(true)
    expect(cardList.classList.contains("overflow-x-hidden")).toBe(true)
    expect(cardList.classList.contains("board-column-scrollbar")).toBe(true)

    const cardRow = screen.getByText("Test title").closest("[data-board-column-card]")!
    expect(cardRow.classList.contains("w-full")).toBe(true)
    expect(cardRow.classList.contains("min-w-0")).toBe(true)
  })

  it("reveals keyboard selection only when the column is active", () => {
    const cards = [makeCard(), makeCard({ id: "T-2", title: "Second card" })]
    const props = {
      name: "Doing",
      cards,
      showWorkspace: false,
      onCardClick: () => {},
      colIndex: 0,
    }
    const { rerender } = render(
      <BoardColumn {...props} selectedIndex={0} isActive={false} />
    )
    const scrollIntoView = vi.fn()
    Object.defineProperty(screen.getByText("Second card").closest("[data-board-column-card]")!, "scrollIntoView", {
      value: scrollIntoView,
    })

    rerender(<BoardColumn {...props} selectedIndex={1} isActive={false} />)
    expect(scrollIntoView).not.toHaveBeenCalled()

    rerender(<BoardColumn {...props} selectedIndex={1} isActive />)
    expect(scrollIntoView).toHaveBeenCalledWith({ block: "nearest" })
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
