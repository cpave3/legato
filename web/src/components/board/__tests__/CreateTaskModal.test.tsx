import { afterEach, describe, expect, it, vi } from "vitest"
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { CreateTaskModal } from "../CreateTaskModal"

afterEach(() => {
  cleanup()
  vi.useRealTimers()
})

describe("CreateTaskModal", () => {
  it("submits with correct payload", () => {
    const onSubmit = vi.fn()
    render(
      <CreateTaskModal
        open={true}
        columns={["Backlog", "Doing"]}
        currentColumn="Doing"
        workspaces={[{ id: 1, name: "W1", color: "#fff" }]}
        onClose={() => {}}
        onSubmit={onSubmit}
      />
    )

    expect(screen.getByRole("dialog", { name: "New Task" })).toBeTruthy()
    expect(screen.getByRole("button", { name: "Close dialog" })).toBeTruthy()
    const dialog = screen.getByRole("dialog", { name: "New Task" })
    expect(dialog.className).toContain("max-w-2xl")
    expect(screen.getByRole("group", { name: "Task details" })).toBeTruthy()
    expect(screen.getByRole("group", { name: "Placement" })).toBeTruthy()
    expect(dialog.querySelector("[data-create-task-layout]")?.className).toContain("md:grid-cols-[minmax(0,1fr)_15rem]")
    expect((screen.getByLabelText("Description") as HTMLTextAreaElement).rows).toBe(10)
    expect(screen.getByText("Markdown supported")).toBeTruthy()

    const titleInput = screen.getByLabelText(/Title/i)
    fireEvent.change(titleInput, { target: { value: "New task title" } })

    const descInput = screen.getByLabelText(/Description/i)
    fireEvent.change(descInput, { target: { value: "Description text" } })
    expect((screen.getByRole("combobox", { name: "Column" }) as HTMLSelectElement).value).toBe("Doing")
    expect((screen.getByRole("combobox", { name: "Priority" }) as HTMLSelectElement).value).toBe("")
    expect((screen.getByRole("combobox", { name: "Workspace" }) as HTMLSelectElement).value).toBe("")

    const createBtn = screen.getByRole("button", { name: /Create/i })
    fireEvent.click(createBtn)

    expect(onSubmit).toHaveBeenCalledWith({
      title: "New task title",
      description: "Description text",
      column: "Doing",
      priority: "",
      workspace_id: null,
    })
  })

  it("keeps the title focused and editable across open rerenders", () => {
    vi.useFakeTimers()
    const onSubmit = vi.fn()
    const firstOnClose = vi.fn()
    const latestOnClose = vi.fn()
    const { rerender } = render(
      <CreateTaskModal
        open={true}
        columns={["Backlog", "Doing"]}
        currentColumn="Backlog"
        workspaces={[]}
        onClose={firstOnClose}
        onSubmit={onSubmit}
      />
    )

    act(() => vi.advanceTimersByTime(50))
    const titleInput = screen.getByLabelText(/Title/i)
    expect(titleInput).toBe(document.activeElement)
    fireEvent.change(titleInput, { target: { value: "Keep" } })

    rerender(
      <CreateTaskModal
        open={true}
        columns={["Backlog", "Doing"]}
        currentColumn="Backlog"
        workspaces={[]}
        onClose={latestOnClose}
        onSubmit={onSubmit}
        loading={true}
      />
    )

    expect(titleInput).toBe(document.activeElement)
    fireEvent.change(titleInput, { target: { value: "Keep typing" } })
    expect((titleInput as HTMLInputElement).value).toBe("Keep typing")
  })

  it("preserves form data while an open request is loading", () => {
    const onSubmit = vi.fn()
    const { rerender } = render(
      <CreateTaskModal
        open={true}
        columns={["Backlog", "Doing"]}
        currentColumn="Backlog"
        workspaces={[]}
        onClose={() => {}}
        onSubmit={onSubmit}
      />
    )

    fireEvent.change(screen.getByLabelText(/Title/i), { target: { value: "Keep this title" } })
    fireEvent.change(screen.getByLabelText(/Description/i), { target: { value: "Keep this description" } })

    rerender(
      <CreateTaskModal
        open={true}
        columns={[...["Backlog", "Doing"]]}
        currentColumn="Backlog"
        workspaces={[]}
        onClose={() => {}}
        onSubmit={onSubmit}
        loading={true}
      />
    )

    expect((screen.getByLabelText(/Title/i) as HTMLInputElement).value).toBe("Keep this title")
    expect((screen.getByLabelText(/Description/i) as HTMLTextAreaElement).value).toBe("Keep this description")
    expect((screen.getByRole("button", { name: "Creating…" }) as HTMLButtonElement).disabled).toBe(true)
  })
})
