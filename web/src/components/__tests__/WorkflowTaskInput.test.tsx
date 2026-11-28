import { useState } from "react"
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { WorkflowTaskInput } from "../WorkflowTaskInput"

// The task picker on its own: grouping and order, the keys, choosing, and
// loading archived tasks.

const { fetchArchivedTasks, board } = vi.hoisted(() => ({ fetchArchivedTasks: vi.fn(), board: { columns: [] as { name: string; cards: Record<string, unknown>[] }[] } }))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "https://legato" }) }))
vi.mock("../../hooks/useBoard", () => ({ useBoard: () => board }))
vi.mock("../../lib/board", () => ({ fetchArchivedTasks: (...args: unknown[]) => fetchArchivedTasks(...args) }))

const card = (id: string, title: string, workspace_name: string, status = "Backlog") => ({ id, title, status, workspace_name })

beforeEach(() => {
  board.columns = [
    { name: "Backlog", cards: [card("LEG-2", "Zebra", "Platform"), card("LEG-1", "Apple", "Platform"), card("LEG-3", "Loose", ""), card("LEG-4", "Mobile thing", "Apps")] },
    { name: "Done", cards: [card("LEG-5", "Finished", "Platform", "Done"), card("LEG-1", "Apple", "Platform")] },
  ]
  fetchArchivedTasks.mockReset()
  fetchArchivedTasks.mockResolvedValue([card("LEG-9", "Old one", "Platform", "Done")])
  vi.spyOn(window, "requestAnimationFrame").mockImplementation(callback => { callback(0); return 0 })
})
afterEach(() => { cleanup(); vi.restoreAllMocks() })

function draw(initial = "", required = false) {
  const onChange = vi.fn()
  function Host() {
    const [value, setValue] = useState(initial)
    return <WorkflowTaskInput label="Task" required={required} value={value} onChange={next => { onChange(next); setValue(next) }} />
  }
  render(<Host />)
  return { onChange, combobox: () => screen.getByRole("combobox") }
}
const optionNames = () => screen.getAllByRole("option").map(option => option.textContent)

describe("the picker", () => {
  it("names the task chosen, or asks for one", () => {
    const { combobox } = draw("LEG-4", true)
    expect(combobox().getAttribute("aria-label")).toBe("Task: LEG-4 · Mobile thing")
    expect(combobox().getAttribute("aria-required")).toBe("true")
    cleanup()
    draw()
    expect(screen.getByRole("combobox", { name: "Task: Select a task" }).textContent).toBe("Select a task")
  })

  it("groups the open tasks by workspace, A to Z with Unassigned last, each once", () => {
    const { combobox } = draw()
    fireEvent.click(combobox())
    const list = screen.getByRole("listbox", { name: "Task" })
    expect(within(list).getAllByRole("group").map(group => group.getAttribute("aria-label"))).toEqual(["Apps", "Platform", "Unassigned"])
    expect(optionNames()).toEqual(["LEG-4 · Mobile thing", "LEG-1 · Apple", "LEG-2 · Zebra", "LEG-3 · Loose"])
    expect(fetchArchivedTasks).not.toHaveBeenCalled()
  })

  it("chooses with the arrow keys and Enter, opening on the task chosen", () => {
    const { onChange, combobox } = draw("LEG-1")
    fireEvent.keyDown(combobox(), { key: "ArrowDown" })
    const list = screen.getByRole("listbox")
    expect(list.getAttribute("aria-activedescendant")).toMatch(/-LEG-1$/)
    fireEvent.keyDown(list, { key: "ArrowDown" })
    fireEvent.keyDown(list, { key: "ArrowDown" })
    fireEvent.keyDown(list, { key: "ArrowDown" })
    expect(list.getAttribute("aria-activedescendant")).toMatch(/-LEG-3$/)
    fireEvent.keyDown(list, { key: "ArrowUp" })
    fireEvent.keyDown(list, { key: "Enter" })
    expect(onChange).toHaveBeenLastCalledWith("LEG-2")
    expect(screen.queryByRole("listbox")).toBeNull()
    expect(document.activeElement).toBe(combobox())
  })

  it("chooses with Space, by click, and closes with Escape", () => {
    const { onChange, combobox } = draw()
    fireEvent.click(combobox())
    fireEvent.keyDown(screen.getByRole("listbox"), { key: " " })
    expect(onChange).toHaveBeenLastCalledWith("LEG-4")
    fireEvent.click(combobox())
    expect(screen.getByRole("option", { name: /LEG-4/ }).getAttribute("aria-selected")).toBe("true")
    fireEvent.mouseEnter(screen.getByRole("option", { name: /LEG-3/ }))
    expect(screen.getByRole("listbox").getAttribute("aria-activedescendant")).toMatch(/-LEG-3$/)
    fireEvent.click(screen.getByRole("option", { name: /LEG-2/ }))
    expect(onChange).toHaveBeenLastCalledWith("LEG-2")
    fireEvent.click(combobox())
    fireEvent.keyDown(screen.getByRole("listbox"), { key: "Escape" })
    expect(screen.queryByRole("listbox")).toBeNull()
    fireEvent.click(combobox())
    fireEvent.keyDown(screen.getByRole("checkbox"), { key: "Escape" })
    expect(screen.queryByRole("listbox")).toBeNull()
  })

  it("shows done and archived tasks on request, marked as such", async () => {
    const { combobox } = draw()
    fireEvent.click(combobox())
    fireEvent.click(screen.getByRole("checkbox", { name: "Show done and archived tasks" }))
    await waitFor(() => expect(screen.getByRole("option", { name: /LEG-9/ })).toBeTruthy())
    expect(fetchArchivedTasks).toHaveBeenCalledWith("https://legato")
    expect(screen.getByRole("option", { name: /LEG-5/ }).textContent).toContain("Done")
    expect(screen.getByRole("option", { name: /LEG-9/ }).textContent).toContain("Archived")
  })

  it("says when archived tasks could not be loaded", async () => {
    fetchArchivedTasks.mockRejectedValue(new Error("offline"))
    const { combobox } = draw()
    fireEvent.click(combobox())
    await act(async () => { fireEvent.click(screen.getByRole("checkbox")) })
    expect((await screen.findByRole("alert")).textContent).toBe("Could not load archived tasks: offline")
  })

  it("says when there is no task to choose", () => {
    board.columns = []
    const { combobox, onChange } = draw()
    fireEvent.click(combobox())
    expect(screen.getByText("No matching tasks")).toBeTruthy()
    fireEvent.keyDown(screen.getByRole("listbox"), { key: "Enter" })
    expect(onChange).not.toHaveBeenCalled()
    expect(screen.getByRole("listbox").hasAttribute("aria-activedescendant")).toBe(false)
  })
})
