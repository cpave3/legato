import { describe, expect, it, vi, afterEach } from "vitest"
import { render, screen, fireEvent, cleanup } from "@testing-library/react"
import { MoveTaskModal } from "../MoveTaskModal"

const workspaces = [
  { id: 1, name: "Platform", color: "#123456" },
  { id: 2, name: "Product", color: "#654321" },
]

function renderModal(overrides: Partial<React.ComponentProps<typeof MoveTaskModal>> = {}) {
  const props: React.ComponentProps<typeof MoveTaskModal> = {
    open: true,
    taskId: "T-1",
    taskTitle: "Test",
    columns: ["Backlog", "Doing", "Done"],
    currentColumn: "Backlog",
    workspaces,
    currentWorkspaceId: 1,
    onClose: vi.fn(),
    onMoveColumn: vi.fn(),
    onMoveWorkspace: vi.fn(),
    ...overrides,
  }
  render(<MoveTaskModal {...props} />)
  return props
}

describe("MoveTaskModal", () => {
  afterEach(cleanup)

  it("renders separate column and workspace destinations with current choices marked", () => {
    renderModal()

    expect(screen.getByRole("heading", { name: "Column" })).toBeTruthy()
    expect(screen.getByRole("heading", { name: "Workspace" })).toBeTruthy()
    expect((screen.getByRole("button", { name: /Backlog.*current/ }) as HTMLButtonElement).disabled).toBe(true)
    expect((screen.getByRole("button", { name: /Platform.*current/ }) as HTMLButtonElement).disabled).toBe(true)
    expect(screen.getByRole("button", { name: "Unassigned" })).toBeTruthy()
    expect(screen.getByRole("button", { name: "Product" })).toBeTruthy()
  })

  it("emits distinct column and workspace moves", () => {
    const props = renderModal()

    fireEvent.click(screen.getByRole("button", { name: "Done" }))
    fireEvent.click(screen.getByRole("button", { name: "Product" }))
    fireEvent.click(screen.getByRole("button", { name: "Unassigned" }))

    expect(props.onMoveColumn).toHaveBeenCalledWith("Done")
    expect(props.onMoveWorkspace).toHaveBeenNthCalledWith(1, 2)
    expect(props.onMoveWorkspace).toHaveBeenNthCalledWith(2, null)
  })

  it("disables every destination while loading", () => {
    renderModal({ loading: true })

    for (const button of screen.getAllByRole("button")) {
      expect((button as HTMLButtonElement).disabled).toBe(true)
    }
  })
})
