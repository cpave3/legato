import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { useState } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"
import userEvent from "@testing-library/user-event"
import { AgentMoreMenu } from "../../AgentChatHeader"

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
  it("skips closed disclosure fields while keeping the summary in keyboard order", async () => {
    const user = userEvent.setup()
    render(<Dialog open onClose={() => {}}><DialogPanel><DialogTitle>Model options</DialogTitle>
      <details><summary>Advanced</summary><input aria-label="Additional option" /></details>
      <Button>Apply</Button>
    </DialogPanel></Dialog>)
    const summary = screen.getByText("Advanced")
    expect(document.activeElement).toBe(summary)
    await user.tab({ shift: true })
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "Apply" }))
    await user.tab()
    expect(document.activeElement).toBe(summary)
    await user.click(summary)
    await user.tab()
    expect(document.activeElement).toBe(screen.getByRole("textbox", { name: "Additional option" }))
  })

  it("allows a portaled menu owned by the dialog and closes that menu first", async () => {
    const user = userEvent.setup()
    const onClose = vi.fn()
    render(<Dialog open onClose={onClose}><DialogPanel><DialogTitle>Agent</DialogTitle>
      <AgentMoreMenu entries={[{ label: "Rename", onSelect: vi.fn() }]} />
    </DialogPanel></Dialog>)
    await user.click(screen.getByRole("button", { name: "More actions" }))
    expect(screen.getByRole("menuitem", { name: "Rename" })).toBe(document.activeElement)
    expect(document.activeElement?.closest("[inert]")).toBeNull()
    await user.keyboard("{Escape}")
    expect(screen.queryByRole("menu")).toBeNull()
    expect(onClose).not.toHaveBeenCalled()
    expect(screen.getByRole("button", { name: "More actions" })).toBe(document.activeElement)
    await user.keyboard("{Escape}")
    expect(onClose).toHaveBeenCalledOnce()
  })

  it("isolates background content and dismisses only the topmost dialog", () => {
    function NestedDialogs() {
      const [parent, setParent] = useState(false)
      const [child, setChild] = useState(false)
      return <>
        <button onClick={() => setParent(true)}>Open parent</button>
        <Dialog open={parent} onClose={() => setParent(false)}>
          <DialogPanel><DialogTitle>Parent</DialogTitle><button onClick={() => setChild(true)}>Open child</button></DialogPanel>
        </Dialog>
        <Dialog open={child} onClose={() => setChild(false)}>
          <DialogPanel><DialogTitle>Child</DialogTitle><button onClick={() => setChild(false)}>Cancel child</button></DialogPanel>
        </Dialog>
      </>
    }
    render(<NestedDialogs />)
    const opener = screen.getByRole("button", { name: "Open parent" })
    opener.focus()
    fireEvent.click(opener)
    expect(opener.closest("[inert]")).not.toBeNull()
    expect(document.body.style.overflow).toBe("hidden")
    fireEvent.click(screen.getByRole("button", { name: "Open child" }))
    fireEvent.keyDown(document, { key: "Escape" })
    expect(screen.queryByRole("dialog", { name: "Child" })).toBeNull()
    expect(screen.getByRole("dialog", { name: "Parent" })).toBeTruthy()
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "Open child" }))
    fireEvent.keyDown(document, { key: "Escape" })
    expect(document.activeElement).toBe(opener)
    expect(opener.closest("[inert]")).toBeNull()
    expect(document.body.style.overflow).toBe("")
  })

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
