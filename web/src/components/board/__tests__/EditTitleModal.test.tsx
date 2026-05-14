import { describe, expect, it, vi } from "vitest"
import { render, screen, fireEvent } from "@testing-library/react"
import { EditTitleModal } from "../EditTitleModal"

describe("EditTitleModal", () => {
  it("submits with current title", () => {
    const onSave = vi.fn()
    render(<EditTitleModal open={true} currentTitle="Old title" onClose={() => {}} onSave={onSave} />)
    const saveBtn = screen.getByRole("button", { name: /Save/i })
    fireEvent.click(saveBtn)
    expect(onSave).toHaveBeenCalledWith("Old title")
  })
})
