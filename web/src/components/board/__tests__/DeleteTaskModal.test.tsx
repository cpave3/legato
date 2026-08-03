import { describe, expect, it, afterEach, vi } from "vitest"
import { render, screen, cleanup, fireEvent } from "@testing-library/react"
import { DeleteTaskModal } from "../DeleteTaskModal"

afterEach(() => {
  cleanup()
})

describe("DeleteTaskModal", () => {
  it("shows remote warning when isRemote is true", () => {
    render(
      <DeleteTaskModal
        open={true}
        taskId="T-1"
        taskTitle="A task"
        isRemote={true}
        onClose={() => {}}
        onConfirm={() => {}}
      />
    )
    expect(screen.getByRole("dialog", { name: "Delete T-1" })).toBeTruthy()
    expect(screen.getByRole("button", { name: "Close dialog" })).toBeTruthy()
    expect(screen.getByText(/remove the local reference only/)).toBeTruthy()
  })

  it("does not show remote warning when isRemote is false", () => {
    render(
      <DeleteTaskModal
        open={true}
        taskId="T-1"
        taskTitle="A task"
        isRemote={false}
        onClose={() => {}}
        onConfirm={() => {}}
      />
    )
    expect(screen.queryByText(/remove the local reference only/)).toBeNull()
  })

  it("confirms with the y shortcut", () => {
    const onConfirm = vi.fn()
    render(
      <DeleteTaskModal
        open={true}
        taskId="T-1"
        taskTitle="A task"
        isRemote={false}
        onClose={() => {}}
        onConfirm={onConfirm}
      />
    )

    fireEvent.keyDown(window, { key: "y" })
    expect(onConfirm).toHaveBeenCalledOnce()
  })

  it("disables duplicate confirmation while deleting", () => {
    render(
      <DeleteTaskModal
        open={true}
        taskId="T-1"
        taskTitle="A task"
        isRemote={false}
        loading={true}
        onClose={() => {}}
        onConfirm={() => {}}
      />
    )
    expect((screen.getByRole("button", { name: "Deleting…" }) as HTMLButtonElement).disabled).toBe(true)
  })
})
