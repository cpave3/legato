import { describe, expect, it, vi } from "vitest"
import { render, screen, fireEvent } from "@testing-library/react"
import { EditDescriptionModal } from "../EditDescriptionModal"

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
})
