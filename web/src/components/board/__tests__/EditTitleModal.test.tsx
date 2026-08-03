import { afterEach, describe, expect, it, vi } from "vitest"
import { cleanup, render, screen, fireEvent } from "@testing-library/react"
import { useState } from "react"
import { EditTitleModal } from "../EditTitleModal"

afterEach(cleanup)

describe("EditTitleModal", () => {
  it("submits with current title", () => {
    const onSave = vi.fn()
    render(<EditTitleModal open={true} currentTitle="Old title" onClose={() => {}} onSave={onSave} />)
    expect(screen.getByRole("dialog", { name: "Edit Title" })).toBeTruthy()
    const saveBtn = screen.getByRole("button", { name: /Save/i })
    fireEvent.click(saveBtn)
    expect(onSave).toHaveBeenCalledWith("Old title")
  })

  it("closes with Escape and restores focus to its opener", () => {
    function Harness() {
      const [open, setOpen] = useState(false)
      return <><button type="button" onClick={() => setOpen(true)}>Edit title</button><EditTitleModal open={open} currentTitle="Old title" onClose={() => setOpen(false)} onSave={() => {}} /></>
    }
    render(<Harness />)
    const opener = screen.getByRole("button", { name: "Edit title" })
    opener.focus()
    fireEvent.click(opener)

    fireEvent.keyDown(document, { key: "Escape" })

    expect(screen.queryByRole("dialog")).toBeNull()
    expect(document.activeElement).toBe(opener)
  })
})
