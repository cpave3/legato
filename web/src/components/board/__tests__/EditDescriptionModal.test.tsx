import { describe, expect, it, vi } from "vitest"
import { render, screen, fireEvent } from "@testing-library/react"
import { EditDescriptionModal } from "../EditDescriptionModal"

describe("EditDescriptionModal", () => {
  it("submits with current content", () => {
    const onSave = vi.fn()
    render(<EditDescriptionModal open={true} currentDescription="Old desc" onClose={() => {}} onSave={onSave} />)
    const saveBtn = screen.getByRole("button", { name: /Save/i })
    fireEvent.click(saveBtn)
    expect(onSave).toHaveBeenCalledWith("Old desc")
  })
})
