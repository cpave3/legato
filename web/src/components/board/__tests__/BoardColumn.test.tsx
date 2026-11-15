import { describe, expect, it, afterEach, vi } from "vitest"
import { render, screen, cleanup, createEvent, fireEvent } from "@testing-library/react"
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

  describe("dropping a card", () => {
    // A drag's data, set by dragstart and read by drop, as a browser keeps it.
    function dragData(initial?: string) {
      const data = new Map<string, string>(initial ? [["text/plain", initial]] : [])
      return {
        dropEffect: "none",
        effectAllowed: "none",
        getData: (type: string) => data.get(type) ?? "",
        setData: (type: string, value: string) => data.set(type, value),
      }
    }

    // Three cards 80px tall with 8px between: middles at 40, 128 and 216.
    function renderColumn(onCardDrop: (...args: unknown[]) => void) {
      const cards = [makeCard({ id: "T-1", title: "One" }), makeCard({ id: "T-2", title: "Two" }), makeCard({ id: "T-3", title: "Three" })]
      const view = render(
        <BoardColumn name="Doing" cards={cards} selectedIndex={0} isActive={false} showWorkspace={false} onCardClick={() => {}} onCardDrop={onCardDrop} colIndex={0} />
      )
      view.container.querySelectorAll<HTMLElement>("[data-board-column-card]").forEach((element, i) => {
        element.getBoundingClientRect = () => ({ top: i * 88, height: 80, bottom: i * 88 + 80, left: 0, right: 200, width: 200, x: 0, y: i * 88, toJSON: () => ({}) })
      })
      return { ...view, column: screen.getByRole("region", { name: "Doing" }) }
    }

    // jsdom has no DragEvent, so the pointer's height is set on the event.
    function dragAt(type: "dragOver" | "drop", target: Element, dataTransfer: object, clientY: number) {
      const event = createEvent[type](target, { dataTransfer })
      Object.defineProperty(event, "clientY", { value: clientY })
      fireEvent(target, event)
    }

    function card(title: string) {
      return screen.getByText(title).closest("[draggable=true]")!
    }

    it("places a card dragged within its column before the card it is dropped above", () => {
      const onCardDrop = vi.fn()
      const { column } = renderColumn(onCardDrop)
      const dataTransfer = dragData()
      fireEvent.dragStart(card("Three"), { dataTransfer })
      dragAt("dragOver", column, dataTransfer, 10)
      dragAt("drop", column, dataTransfer, 10)
      expect(onCardDrop).toHaveBeenCalledWith("T-3", "Doing", "T-1")
    })

    it("places a card dropped below the last one at the end", () => {
      const onCardDrop = vi.fn()
      const { column } = renderColumn(onCardDrop)
      const dataTransfer = dragData()
      fireEvent.dragStart(card("One"), { dataTransfer })
      dragAt("dragOver", column, dataTransfer, 400)
      dragAt("drop", column, dataTransfer, 400)
      expect(onCardDrop).toHaveBeenCalledWith("T-1", "Doing", null)
    })

    it("does nothing for a card dropped where it already is", () => {
      const onCardDrop = vi.fn()
      const { column } = renderColumn(onCardDrop)
      const dataTransfer = dragData()
      fireEvent.dragStart(card("Two"), { dataTransfer })
      dragAt("dragOver", column, dataTransfer, 130)
      dragAt("drop", column, dataTransfer, 130)
      expect(onCardDrop).not.toHaveBeenCalled()
    })

    it("places a card from another column where it is dropped", () => {
      const onCardDrop = vi.fn()
      const { column } = renderColumn(onCardDrop)
      const dataTransfer = dragData("X-9")
      dragAt("dragOver", column, dataTransfer, 100)
      dragAt("drop", column, dataTransfer, 100)
      expect(onCardDrop).toHaveBeenCalledWith("X-9", "Doing", "T-2")
    })

    it("shows where the card will land while it is over the column", () => {
      const { column, container } = renderColumn(vi.fn())
      const dataTransfer = dragData("X-9")
      dragAt("dragOver", column, dataTransfer, 100)
      const indicator = container.querySelector("[data-drop-indicator]")
      expect(indicator).not.toBeNull()
      expect(indicator!.closest("[data-board-column-card]")!.textContent).toContain("Two")

      dragAt("drop", column, dataTransfer, 100)
      expect(container.querySelector("[data-drop-indicator]")).toBeNull()
    })

    it("shows no line where the dragged card already is", () => {
      const { column, container } = renderColumn(vi.fn())
      const dataTransfer = dragData()
      fireEvent.dragStart(card("Two"), { dataTransfer })
      dragAt("dragOver", column, dataTransfer, 130)
      expect(container.querySelector("[data-drop-indicator]")).toBeNull()
    })
  })
})
