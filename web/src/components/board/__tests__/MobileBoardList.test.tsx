import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { MemoryRouter } from "react-router-dom"
import { MobileBoardList } from "../MobileBoardList"
import type { BoardCard, BoardColumn } from "../../../lib/board-types"

function card(id: string, title: string, status: string): BoardCard {
  return { id, title, status, priority: "High", issue_type: "Task", provider: "", has_warning: false, agent_active: false, agent_state: "", working_seconds: 0, waiting_seconds: 0, error_seconds: 0, workspace_id: null, workspace_name: "", workspace_color: "", pr_check_status: "", pr_review_decision: "", pr_comment_count: 0, pr_is_draft: false, pr_number: 0 }
}

const columns: BoardColumn[] = [
  { name: "Backlog", cards: [] },
  { name: "Doing", cards: [card("task-1", "Mobile task", "Doing")] },
  { name: "Review", cards: [card("task-2", "Reviewed task", "Review")] },
]

afterEach(() => {
  cleanup()
  localStorage.clear()
})

function renderList(props: Partial<Parameters<typeof MobileBoardList>[0]> = {}) {
  return render(<MemoryRouter><MobileBoardList columns={columns} showWorkspace onCardClick={() => undefined} {...props} /></MemoryRouter>)
}

function chips() {
  return screen.getByRole("tablist", { name: "Columns" })
}

describe("MobileBoardList", () => {
  it("names each column with its count and shows the first one with cards", () => {
    renderList()
    expect(within(chips()).getAllByRole("tab").map((tab) => tab.textContent)).toEqual(["Backlog 0", "Doing 1", "Review 1"])
    expect(within(chips()).getByRole("tab", { name: "Doing 1" }).getAttribute("aria-selected")).toBe("true")
    expect(screen.getByText("Mobile task")).toBeTruthy()
    expect(screen.queryByText("Reviewed task")).toBeNull()
  })

  it("shows the column whose chip is tapped, and remembers it", () => {
    const { unmount } = renderList()
    fireEvent.click(within(chips()).getByRole("tab", { name: "Review 1" }))
    expect(screen.getByText("Reviewed task")).toBeTruthy()
    expect(screen.queryByText("Mobile task")).toBeNull()
    unmount()
    renderList()
    expect(screen.getByText("Reviewed task")).toBeTruthy()
  })

  it("moves between columns with a sideways swipe", () => {
    renderList()
    const list = screen.getByRole("tabpanel")
    fireEvent.touchStart(list, { touches: [{ clientX: 300, clientY: 200 }] })
    fireEvent.touchEnd(list, { changedTouches: [{ clientX: 150, clientY: 210 }] })
    expect(screen.getByText("Reviewed task")).toBeTruthy()
    fireEvent.touchStart(list, { touches: [{ clientX: 100, clientY: 200 }] })
    fireEvent.touchEnd(list, { changedTouches: [{ clientX: 260, clientY: 190 }] })
    expect(screen.getByText("Mobile task")).toBeTruthy()
  })

  it("shows the column of a highlighted task", () => {
    renderList({ highlightedTaskId: "task-2" })
    expect(screen.getByText("Reviewed task")).toBeTruthy()
  })

  it("says when a column is empty, and opens a card's actions from its button", () => {
    const onCardContextMenu = vi.fn()
    renderList({ onCardContextMenu })
    fireEvent.click(screen.getByRole("button", { name: "Open actions for Mobile task" }))
    expect(onCardContextMenu).toHaveBeenCalledTimes(1)
    fireEvent.click(within(chips()).getByRole("tab", { name: "Backlog 0" }))
    expect(screen.getByText("Nothing in Backlog")).toBeTruthy()
  })
})
