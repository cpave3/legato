import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { RenameAgentDialog } from "../RenameAgentDialog"

const { patchTask } = vi.hoisted(() => ({ patchTask: vi.fn() }))
vi.mock("../../lib/board", () => ({ patchTask }))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "http://x" }) }))

afterEach(() => { cleanup(); patchTask.mockReset() })

describe("RenameAgentDialog", () => {
  it("saves the trimmed title and reports it", async () => {
    patchTask.mockResolvedValue({ status: "ok" })
    const onRenamed = vi.fn()
    const onClose = vi.fn()
    render(<RenameAgentDialog taskId="T-1" title="Old" onRenamed={onRenamed} onClose={onClose} />)
    const input = screen.getByLabelText("Name") as HTMLInputElement
    expect(input.value).toBe("Old")
    fireEvent.change(input, { target: { value: "  New name " } })
    fireEvent.click(screen.getByRole("button", { name: "Rename" }))
    await waitFor(() => expect(onClose).toHaveBeenCalled())
    expect(patchTask).toHaveBeenCalledWith("http://x", "T-1", { title: "New name" })
    expect(onRenamed).toHaveBeenCalledWith("New name")
  })

  it("will not save an empty or unchanged name", () => {
    render(<RenameAgentDialog taskId="T-1" title="Old" onRenamed={() => {}} onClose={() => {}} />)
    const save = screen.getByRole("button", { name: "Rename" }) as HTMLButtonElement
    expect(save.disabled).toBe(true)
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "  " } })
    expect(save.disabled).toBe(true)
  })

  it("shows the failure and stays open", async () => {
    patchTask.mockRejectedValue(new Error("provider-backed titles cannot change"))
    const onClose = vi.fn()
    render(<RenameAgentDialog taskId="T-1" title="Old" onRenamed={() => {}} onClose={onClose} />)
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "New" } })
    fireEvent.click(screen.getByRole("button", { name: "Rename" }))
    expect((await screen.findByRole("alert")).textContent).toContain("provider-backed")
    expect(onClose).not.toHaveBeenCalled()
  })
})
