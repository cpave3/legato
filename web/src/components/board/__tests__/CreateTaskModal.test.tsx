import { describe, expect, it, vi } from "vitest"
import { render, screen, fireEvent } from "@testing-library/react"
import { CreateTaskModal } from "../CreateTaskModal"

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

    const titleInput = screen.getByLabelText(/Title/i)
    fireEvent.change(titleInput, { target: { value: "New task title" } })

    const descInput = screen.getByLabelText(/Description/i)
    fireEvent.change(descInput, { target: { value: "Description text" } })

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
