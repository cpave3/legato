import { describe, expect, it, vi, afterEach } from "vitest"
import { render, screen, fireEvent, cleanup } from "@testing-library/react"
import { MoveTaskModal } from "../MoveTaskModal"

describe("MoveTaskModal", () => {
  afterEach(() => {
    cleanup()
  })

  it("clicking column button triggers move", () => {
    const onMove = vi.fn()
    render(
      <MoveTaskModal
        open={true}
        taskId="T-1"
        taskTitle="Test"
        columns={["Backlog", "Doing", "Done"]}
        currentColumn="Backlog"
        onClose={() => {}}
        onMove={onMove}
      />
    )

    const doneButton = screen.getByText("Done")
    fireEvent.click(doneButton)
    expect(onMove).toHaveBeenCalledWith("Done")
  })

  it("does not allow selecting current column", () => {
    render(
      <MoveTaskModal
        open={true}
        taskId="T-1"
        taskTitle=""
        columns={["Backlog"]}
        currentColumn="Backlog"
        onClose={() => {}}
        onMove={() => {}}
      />
    )
    expect(screen.getByTestId("current-col")).toBeTruthy()
  })
})
