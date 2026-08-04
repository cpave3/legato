import { afterEach, describe, expect, it, vi } from "vitest"
import { cleanup, render, screen, fireEvent } from "@testing-library/react"
import { EditDescriptionModal } from "../EditDescriptionModal"

afterEach(cleanup)

describe("EditDescriptionModal", () => {
  it("submits with current content", () => {
    const onSave = vi.fn()
    render(<EditDescriptionModal open={true} currentDescription="Old desc" onClose={() => {}} onSave={onSave} />)
    expect(screen.getByRole("dialog", { name: "Edit description" })).toBeTruthy()
    expect(screen.getByRole("button", { name: "Close dialog" })).toBeTruthy()
    const description = screen.getByLabelText("Description") as HTMLTextAreaElement
    expect(description.rows).toBe(14)
    expect(description.className).toContain("resize-y")
    expect(screen.getByText("Markdown supported")).toBeTruthy()
    const panel = screen.getByRole("dialog", { name: "Edit description" })
    expect(panel.className).toContain("max-w-2xl")
    expect(panel.className).toContain("max-h-[calc(100dvh-2rem)]")
    expect(panel.className).not.toContain("h-[min(80vh")
    const saveBtn = screen.getByRole("button", { name: /Save/i })
    fireEvent.click(saveBtn)
    expect(onSave).toHaveBeenCalledWith("Old desc")
  })

  it("explains provider ownership and edits only the supplied Legato details", () => {
    const onSave = vi.fn()
    render(<EditDescriptionModal open currentDescription="Local only" providerBacked onClose={() => {}} onSave={onSave} />)
    expect(screen.getByRole("dialog", { name: "Edit Legato details" })).toBeTruthy()
    expect(screen.getByText(/provider description is read-only/i)).toBeTruthy()
    expect((screen.getByLabelText("Description") as HTMLTextAreaElement).value).toBe("Local only")
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    expect(onSave).toHaveBeenCalledWith("Local only")
  })
})
