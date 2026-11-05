import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { MemoryRouter } from "react-router-dom"
import { MobileBoardList } from "../MobileBoardList"
import type { AwayOn, BoardCard, BoardColumn, BoardWorker } from "../../../lib/board-types"

function card(id: string, title: string, status: string): BoardCard {
  return { id, title, status, priority: "High", issue_type: "Task", provider: "", has_warning: false, agent_active: false, agent_state: "", working_seconds: 0, waiting_seconds: 0, error_seconds: 0, workspace_id: null, workspace_name: "", workspace_color: "" }
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

  describe("the Away section", () => {
    function away(id: string, title: string, worker: BoardWorker, more: Partial<AwayOn> = {}): BoardCard {
      return {
        ...card(id, title, "Doing"),
        away: {
          worker_instance_id: worker.instance_id, worker: worker.name, handoff_id: "ho-" + id, worker_task_id: "w-" + id, purpose: "make-change", kind: "new-branch",
          repo: "acme/widget", branch: "legato/" + id, left_column: "Doing", handed_off_at: "2026-11-05T09:00:00Z", state: "in_progress", view: "working", ...more,
        },
      }
    }
    const vm1: BoardWorker = { instance_id: "srv-1", name: "vm-1", address: "https://vm-1.lan:3080" }
    const vm2: BoardWorker = { instance_id: "srv-2", name: "vm-2", address: "https://vm-2.lan:3080" }
    const vm3: BoardWorker = { instance_id: "srv-3", name: "vm-3", address: "https://vm-3.lan:3080" }
    // As the board orders the lane: by worker.
    const awayCards = [
      away("T-1", "Retry webhooks", vm1, { state: "done", view: "done" }),
      away("T-2", "Find the leak", vm1),
      away("T-3", "Bump deps", vm1, { state: "done", view: "done" }),
      away("T-4", "Tidy logs", vm2),
    ]
    const withAway: BoardColumn[] = [...columns, { name: "Away", away: true, cards: awayCards }]

    function section() {
      return screen.getByRole("region", { name: "Away on workers" })
    }

    it("puts away tasks in a section above the columns, one collapsed line per worker", () => {
      renderList({ columns: withAway, workers: [vm1, vm2, vm3], baseUrl: "" })
      expect(within(chips()).getAllByRole("tab").map((tab) => tab.textContent)).toEqual(["Backlog 0", "Doing 1", "Review 1"])
      const vm1Line = within(section()).getByRole("button", { name: "vm-1 · 3 away · 2 done" })
      expect(vm1Line.getAttribute("aria-expanded")).toBe("false")
      expect(within(section()).getByRole("button", { name: "vm-2 · 1 away" })).toBeTruthy()
      // A worker with nothing away has no line.
      expect(within(section()).queryByRole("button", { name: /vm-3/ })).toBeNull()
      expect(within(section()).queryByText("Retry webhooks")).toBeNull()
      expect(within(section()).getByRole("button", { name: "Return all done (2)" })).toBeTruthy()
    })

    it("expands a worker's line into its rows with their actions, and opens a tapped row", () => {
      const onCardClick = vi.fn()
      renderList({ columns: withAway, workers: [vm1, vm2], baseUrl: "", onCardClick })
      fireEvent.click(within(section()).getByRole("button", { name: "vm-1 · 3 away · 2 done" }))
      const group = within(section()).getByRole("group", { name: "vm-1" })
      const done = within(group).getByRole("button", { name: /Retry webhooks/ })
      expect(done.textContent).toContain("from Doing")
      expect(within(group).getAllByRole("button", { name: "Return" })).toHaveLength(2)
      expect(within(group).getByRole("button", { name: "Recall" })).toBeTruthy()
      expect(within(section()).queryByText("Tidy logs")).toBeNull()
      fireEvent.click(within(group).getByRole("button", { name: /Bump deps/ }))
      expect(onCardClick).toHaveBeenCalledWith(3, 2)
      fireEvent.click(within(section()).getByRole("button", { name: "vm-1 · 3 away · 2 done" }))
      expect(within(section()).queryByText("Retry webhooks")).toBeNull()
    })

    it("opens the line of a highlighted away task", () => {
      renderList({ columns: withAway, workers: [vm1, vm2], baseUrl: "", highlightedTaskId: "T-4" })
      expect(within(section()).getByRole("button", { name: "vm-2 · 1 away" }).getAttribute("aria-expanded")).toBe("true")
      expect(within(section()).getByText("Tidy logs")).toBeTruthy()
      expect(screen.getByText("Mobile task")).toBeTruthy()
    })

    it("has no section while nothing is away", () => {
      renderList({ columns: [...columns, { name: "Away", away: true, cards: [] }], workers: [vm1], baseUrl: "" })
      expect(screen.queryByRole("region", { name: "Away on workers" })).toBeNull()
      expect(within(chips()).getAllByRole("tab")).toHaveLength(3)
    })
  })
})
