import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { useState } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { Button } from "../Button"
import { Dialog, DialogBody, DialogClose, DialogFooter, DialogHeader, DialogPanel, DialogTitle } from "../Dialog"

function DialogHarness({ onClose = () => undefined }: { onClose?: () => void }) {
  const [open, setOpen] = useState(false)
  const close = () => {
    setOpen(false)
    onClose()
  }

  return (
    <>
      <button type="button" onClick={() => setOpen(true)}>Open task editor</button>
      <Dialog open={open} onClose={close}>
        <DialogPanel>
          <DialogHeader>
            <DialogTitle>Edit task</DialogTitle>
            <DialogClose />
          </DialogHeader>
          <DialogBody><input aria-label="Task title" /></DialogBody>
          <DialogFooter><Button onClick={close}>Save</Button></DialogFooter>
        </DialogPanel>
      </Dialog>
    </>
  )
}

afterEach(cleanup)

describe("Dialog", () => {
  it("is named, contains focus, dismisses with Escape, and restores focus", () => {
    const onClose = vi.fn()
    render(<DialogHarness onClose={onClose} />)

    const opener = screen.getByRole("button", { name: "Open task editor" })
    opener.focus()
    fireEvent.click(opener)

    expect(screen.getByRole("dialog", { name: "Edit task" })).toBeTruthy()
    const close = screen.getByRole("button", { name: "Close dialog" })
    expect(close).toBe(document.activeElement)

    close.focus()
    fireEvent.keyDown(document, { key: "Tab", shiftKey: true })
    expect(screen.getByRole("button", { name: "Save" })).toBe(document.activeElement)

    fireEvent.keyDown(document, { key: "Escape" })
    expect(screen.queryByRole("dialog")).toBeNull()
    expect(opener).toBe(document.activeElement)
    expect(onClose).toHaveBeenCalledOnce()
  })

  it("keeps focus across open rerenders and uses the latest dismissal props", () => {
    const firstOnClose = vi.fn()
    const latestOnClose = vi.fn()

    const renderDialog = (onClose: () => void, dismissible = true, open = true) => (
      <>
        <button type="button">Original opener</button>
        <Dialog open={open} onClose={onClose} dismissible={dismissible}>
          <DialogPanel>
            <DialogHeader>
              <DialogTitle>Edit task</DialogTitle>
              <DialogClose />
            </DialogHeader>
            <DialogBody><input aria-label="Task title" /></DialogBody>
            <DialogFooter><Button>Save</Button></DialogFooter>
          </DialogPanel>
        </Dialog>
      </>
    )

    const { rerender } = render(renderDialog(firstOnClose, true, false))
    const opener = screen.getByRole("button", { name: "Original opener" })
    opener.focus()
    rerender(renderDialog(firstOnClose))
    const input = screen.getByRole("textbox", { name: "Task title" })
    input.focus()
    fireEvent.change(input, { target: { value: "Keep typing" } })

    rerender(renderDialog(latestOnClose))

    expect(input).toBe(document.activeElement)
    fireEvent.change(input, { target: { value: "Keep typing here" } })
    expect((input as HTMLInputElement).value).toBe("Keep typing here")

    fireEvent.keyDown(document, { key: "Escape" })
    expect(firstOnClose).not.toHaveBeenCalled()
    expect(latestOnClose).toHaveBeenCalledOnce()

    rerender(renderDialog(latestOnClose, false))
    expect(input).toBe(document.activeElement)
    fireEvent.keyDown(document, { key: "Escape" })
    fireEvent.mouseDown(screen.getByTestId("dialog-backdrop"))
    expect(latestOnClose).toHaveBeenCalledOnce()

    rerender(renderDialog(latestOnClose, true))
    expect(input).toBe(document.activeElement)
    fireEvent.keyDown(document, { key: "Escape" })
    expect(latestOnClose).toHaveBeenCalledTimes(2)

    rerender(renderDialog(latestOnClose, true, false))
    expect(opener).toBe(document.activeElement)
  })

  it("dismisses from the backdrop but not from the panel", () => {
    const onClose = vi.fn()
    render(<DialogHarness onClose={onClose} />)
    fireEvent.click(screen.getByRole("button", { name: "Open task editor" }))

    fireEvent.click(screen.getByRole("dialog"))
    expect(onClose).not.toHaveBeenCalled()

    fireEvent.mouseDown(screen.getByTestId("dialog-backdrop"))
    expect(onClose).toHaveBeenCalledOnce()
  })
})
