import { describe, expect, it, afterEach, vi } from "vitest"
import { render, screen, cleanup, fireEvent, within } from "@testing-library/react"
import { BoardGrid } from "../BoardGrid"
import type { BoardCard, BoardColumn as BoardColumnType } from "../../../lib/board-types"

afterEach(() => {
  cleanup()
})

function card(id: string, title: string, status: string): BoardCard {
  return { id, title, priority: "", issue_type: "", status, provider: "", has_warning: false, agent_active: false, agent_state: "", working_seconds: 0, waiting_seconds: 0, error_seconds: 0, workspace_id: null, workspace_name: "", workspace_color: "" }
}

const columns: BoardColumnType[] = [
  { name: "Backlog", cards: [card("T-1", "A", "Backlog")] },
  { name: "Ready", cards: [] },
  { name: "Doing", cards: [card("T-2", "B", "Doing")] },
]

// A card dragged from its column and dropped on target.
function drop(target: HTMLElement, cardId: string) {
  const dataTransfer = { getData: () => cardId, setData: () => {}, dropEffect: "", effectAllowed: "" }
  fireEvent.dragOver(target, { dataTransfer })
  fireEvent.drop(target, { dataTransfer })
}

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

  it("draws the Away lane after the columns, grouped by worker", () => {
    const awayCard = { ...card("T-9", "Away task", "Doing"), away: { worker_instance_id: "srv-1", worker: "vm-1", handoff_id: "h", worker_task_id: "w", purpose: "make-change", kind: "new-branch", repo: "a/b", branch: "legato/t-9", left_column: "Doing", handed_off_at: "2026-11-05T09:00:00Z" } }
    const onCardClick = vi.fn()
    renderGrid({ showEmpty: false, onCardClick, workers: [{ instance_id: "srv-1", name: "vm-1", address: "https://vm-1" }], columns: [...columns, { name: "Away", away: true, cards: [awayCard] }] })
    const lane = screen.getByRole("region", { name: "Away" })
    fireEvent.click(screen.getByRole("button", { name: /Away task/ }))
    expect(onCardClick).toHaveBeenCalledWith(3, 0)
    expect(lane).toBeTruthy()
  })

  it("offers Return on a done away row and Return all done on its worker's group", () => {
    const awayCard = { ...card("T-9", "Away task", "Doing"), away: { worker_instance_id: "srv-1", worker: "vm-1", handoff_id: "h", worker_task_id: "w", purpose: "make-change", kind: "new-branch", repo: "a/b", branch: "legato/t-9", left_column: "Doing", handed_off_at: "2026-11-05T09:00:00Z", state: "done", view: "done" } }
    renderGrid({ baseUrl: "", workers: [{ instance_id: "srv-1", name: "vm-1", address: "https://vm-1" }], columns: [...columns, { name: "Away", away: true, cards: [awayCard] }] })
    const group = screen.getByRole("group", { name: "vm-1" })
    expect(within(group).getByRole("button", { name: "Return" })).toBeTruthy()
    expect(within(group).getByRole("button", { name: "Return all done (1)" })).toBeTruthy()
    expect(within(group).queryByRole("button", { name: "Recall" })).toBeNull()
  })

  it("folds an empty Away lane to a rail like an empty column, which opens it again", () => {
    const onToggleEmpty = vi.fn()
    renderGrid({ showEmpty: false, onToggleEmpty, workers: [{ instance_id: "srv-1", name: "vm-1", address: "https://vm-1" }], columns: [...columns, { name: "Away", away: true, cards: [] }] })
    expect(screen.queryByRole("region", { name: "Away" })).toBeNull()
    fireEvent.click(screen.getByRole("button", { name: "Away, 0 cards" }))
    expect(onToggleEmpty).toHaveBeenCalledTimes(1)
  })

  it("opens an empty Away lane with the empty columns", () => {
    renderGrid({ showEmpty: true, workers: [{ instance_id: "srv-1", name: "vm-1", address: "https://vm-1" }], columns: [...columns, { name: "Away", away: true, cards: [] }] })
    expect(screen.getByRole("group", { name: "vm-1" })).toBeTruthy()
  })

  it("hands off a card dropped on the Away lane's rail, and does not move it", () => {
    const onCardHandOff = vi.fn()
    const onCardMove = vi.fn()
    renderGrid({ showEmpty: false, onCardHandOff, onCardMove, workers: [{ instance_id: "srv-1", name: "vm-1", address: "https://vm-1" }], columns: [...columns, { name: "Away", away: true, cards: [] }] })
    drop(screen.getByRole("button", { name: "Away, 0 cards" }), "T-1")
    expect(onCardHandOff).toHaveBeenCalledWith("T-1")
    expect(onCardMove).not.toHaveBeenCalled()
  })

  it("hands off a card dropped on the open Away lane", () => {
    const awayCard = { ...card("T-9", "Away task", "Doing"), away: { worker_instance_id: "srv-1", worker: "vm-1", handoff_id: "h", worker_task_id: "w", purpose: "make-change", kind: "new-branch", repo: "a/b", branch: "legato/t-9", left_column: "Doing", handed_off_at: "2026-11-05T09:00:00Z" } }
    const onCardHandOff = vi.fn()
    renderGrid({ showEmpty: false, onCardHandOff, workers: [{ instance_id: "srv-1", name: "vm-1", address: "https://vm-1" }], columns: [...columns, { name: "Away", away: true, cards: [awayCard] }] })
    drop(screen.getByRole("group", { name: "vm-1" }), "T-2")
    expect(onCardHandOff).toHaveBeenCalledWith("T-2")
  })
})
