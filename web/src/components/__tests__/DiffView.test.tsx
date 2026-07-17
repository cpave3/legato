import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"
import { DiffView } from "../DiffView"

const files = [{
  old_path: "src/old.ts",
  new_path: "src/new.ts",
  status: "renamed" as const,
  hunks: [{
    anchor: "auth-refresh-anchor",
    header: "@@ -1,2 +1,2 @@",
    lines: [
      { kind: "del" as const, old_no: 1, new_no: 0, text: "const oldName = true" },
      { kind: "add" as const, old_no: 0, new_no: 1, text: "const newName = true" },
    ],
  }],
}]

afterEach(cleanup)

describe("DiffView", () => {
  it("renders file metadata, hunk headers, line numbers, and changed content", () => {
    render(<DiffView files={files} />)

    expect(screen.getByText("src/old.ts → src/new.ts")).toBeTruthy()
    expect(screen.getByText("renamed")).toBeTruthy()
    expect(screen.getByText("@@ -1,2 +1,2 @@")).toBeTruthy()
    expect(screen.getByText("const oldName = true")).toBeTruthy()
    expect(screen.getByText("const newName = true")).toBeTruthy()
    expect(screen.getByRole("button", { name: "Select old line 1" })).toBeTruthy()
    expect(screen.getByRole("button", { name: "Select new line 1" })).toBeTruthy()
  })

  it("keeps each file header sticky beneath the review action bar", () => {
    const { container } = render(<DiffView files={files} />)

    const header = container.querySelector("[data-sticky-file-header]")
    expect(header?.classList.contains("sticky")).toBe(true)
    expect(header?.classList.contains("top-[27px]")).toBe(true)
    expect(header?.classList.contains("z-10")).toBe(true)
    expect(header?.closest("section")?.classList.contains("overflow-hidden")).toBe(false)
  })

  it("renders matching hunk notes immediately above their hunk", () => {
    const { container } = render(<DiffView files={files} hunkNotes={[{
      id: "N-1",
      task_id: "T-1",
      step_id: "S-1",
      file_path: "src/new.ts",
      hunk_anchor: "auth-refresh-anchor",
      body: "Check the refresh race.",
      created_at: "2026-01-01",
    }]} />)

    const note = screen.getByText("Check the refresh race.")
    const header = screen.getByText("@@ -1,2 +1,2 @@")
    expect(note.compareDocumentPosition(header) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(container.querySelector('[data-hunk-anchor="auth-refresh-anchor"]')?.contains(note)).toBe(true)
  })

  it("places line-range notes before the first selected line and highlights the range", () => {
    const { container } = render(<DiffView files={files} hunkNotes={[{
      id: "N-range",
      task_id: "T-1",
      step_id: "S-1",
      file_path: "src/new.ts",
      hunk_anchor: "auth-refresh-anchor",
      line_start: 1,
      line_end: 2,
      line_anchor: "range-anchor",
      body: "These lines move together.",
      created_at: "2026-01-01",
    }]} />)

    const note = screen.getByText("These lines move together.")
    const firstLine = screen.getByText("const oldName = true").closest("[data-diff-line]")!
    expect(note.compareDocumentPosition(firstLine) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(container.querySelectorAll('[data-line-note-range="N-range"]')).toHaveLength(2)
  })

  it("selects and extends a contiguous line range from the gutter", () => {
    const selections: unknown[] = []
    const { rerender } = render(<DiffView files={files} onSelectionChange={(selection) => selections.push(selection)} />)

    fireEvent.click(screen.getByRole("button", { name: "Select old line 1" }))
    expect(selections.at(-1)).toEqual({
      file_path: "src/new.ts", hunk_anchor: "auth-refresh-anchor", start: 0, end: 0,
    })

    const selection = selections.at(-1) as { file_path: string; hunk_anchor: string; start: number; end: number }
    rerender(<DiffView files={files} selection={selection} onSelectionChange={(next) => selections.push(next)} />)
    fireEvent.click(screen.getByRole("button", { name: "Select new line 1" }))
    expect(selections.at(-1)).toEqual({
      file_path: "src/new.ts", hunk_anchor: "auth-refresh-anchor", start: 0, end: 1,
    })
  })

  it("selects a line range by dragging across diff rows", () => {
    const selections: unknown[] = []
    render(<DiffView files={files} onSelectionChange={(selection) => selections.push(selection)} />)

    const firstGutter = screen.getByRole("button", { name: "Select old line 1" })
    const secondRow = screen.getByText("const newName = true").closest("[data-diff-line]")!
    fireEvent.pointerDown(firstGutter, { pointerId: 1, button: 0 })
    fireEvent.pointerEnter(secondRow, { pointerId: 1, buttons: 1 })
    fireEvent.pointerUp(secondRow, { pointerId: 1 })
    fireEvent.click(firstGutter)

    expect(selections.at(-1)).toEqual({
      file_path: "src/new.ts", hunk_anchor: "auth-refresh-anchor", start: 0, end: 1,
    })
  })

  it("replaces the selection when a line in another hunk is clicked", () => {
    const twoHunks = [{ ...files[0], hunks: [...files[0].hunks, {
      anchor: "second-anchor",
      header: "@@ -10 +10 @@",
      lines: [{ kind: "ctx" as const, old_no: 10, new_no: 10, text: "return value" }],
    }] }]
    let selection = { file_path: "src/new.ts", hunk_anchor: "auth-refresh-anchor", start: 0, end: 1 }
    const { rerender } = render(<DiffView files={twoHunks} selection={selection} onSelectionChange={(next) => { selection = next! }} />)

    fireEvent.click(screen.getByRole("button", { name: "Select new line 10" }))
    rerender(<DiffView files={twoHunks} selection={selection} onSelectionChange={(next) => { selection = next! }} />)

    expect(selection).toEqual({ file_path: "src/new.ts", hunk_anchor: "second-anchor", start: 0, end: 0 })
    expect(screen.getByText("return value").closest("[data-diff-line]")?.getAttribute("data-selected")).toBe("true")
  })

  it("collapses and restores a hunk with its Viewed checkbox", () => {
    render(<DiffView files={files} />)

    const viewed = screen.getByRole("checkbox", { name: "Viewed @@ -1,2 +1,2 @@" })
    fireEvent.click(viewed)
    expect(screen.queryByText("const oldName = true")).toBeNull()
    expect(screen.getByText("@@ -1,2 +1,2 @@")).toBeTruthy()

    fireEvent.click(viewed)
    expect(screen.getByText("const oldName = true")).toBeTruthy()
  })

  it("clears a selected range when its hunk is collapsed", () => {
    let selection = { file_path: "src/new.ts", hunk_anchor: "auth-refresh-anchor", start: 0, end: 1 }
    render(<DiffView files={files} selection={selection} onSelectionChange={(next) => { selection = next! }} />)

    fireEvent.click(screen.getByRole("checkbox", { name: "Viewed @@ -1,2 +1,2 @@" }))

    expect(selection).toBeNull()
  })

  it("shows an empty state when a step has no diff", () => {
    render(<DiffView files={[]} />)
    expect(screen.getByText("No file changes in this step.")).toBeTruthy()
  })
})
