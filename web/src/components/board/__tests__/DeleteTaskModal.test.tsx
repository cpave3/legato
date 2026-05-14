import { describe, expect, it, afterEach } from "vitest"
import { render, screen, cleanup } from "@testing-library/react"
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
})
