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

  it("keeps horizontal overflow below the sticky file header", () => {
    const { container } = render(<DiffView files={files} />)

    const header = container.querySelector("[data-sticky-file-header]")
    expect(header?.classList.contains("sticky")).toBe(true)
    expect(header?.classList.contains("top-[27px]")).toBe(true)
    expect(header?.classList.contains("z-10")).toBe(true)

    let ancestor = header?.parentElement
    while (ancestor && ancestor !== container) {
      expect(ancestor.classList.contains("overflow-x-auto")).toBe(false)
      ancestor = ancestor.parentElement
    }

    const lines = container.querySelector("[data-diff-lines]")
    expect(lines?.classList.contains("max-w-full")).toBe(true)
    expect(lines?.classList.contains("overflow-x-auto")).toBe(true)
    expect(lines?.contains(screen.getByText("const oldName = true"))).toBe(true)
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

    // Range block should be a single parent row, not per-line repeated notes.
    const rangeRows = container.querySelectorAll('[data-diff-block="range"]')
    expect(rangeRows).toHaveLength(1)
    // Each line inside should preserve the data-line-note-range marker.
    expect(rangeRows[0].querySelectorAll('[data-line-note-range="N-range"]')).toHaveLength(2)
    // Outer border only on the wrapper, not between internal rows.
    const firstRow = rangeRows[0].querySelector('[data-diff-line]')
    expect(firstRow?.className.includes('ring-amber')).toBe(false)
    expect(firstRow?.className.includes('border-amber')).toBe(false)
  })

  it("selects and extends a contiguous line range from the gutter", () => {
    const selections: unknown[] = []
    const { rerender, container } = render(<DiffView files={files} onSelectionChange={(selection) => selections.push(selection)} />)

    fireEvent.click(screen.getByRole("button", { name: "Select old line 1" }))
    expect(selections.at(-1)).toEqual({
      file_path: "src/new.ts", hunk_anchor: "auth-refresh-anchor", start: 0, end: 0,
    })

    const selection = selections.at(-1) as { file_path: string; hunk_anchor: string; start: number; end: number }
    rerender(<DiffView files={files} selection={selection} onSelectionChange={(next) => selections.push(next)} />)
    expect(container.querySelector('[data-diff-block="selection"]')?.className).toContain("bg-[var(--theme-selection,")
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

  describe("line rows", () => {
    const three = [{ ...files[0], hunks: [{ ...files[0].hunks[0], lines: [
      ...files[0].hunks[0].lines,
      { kind: "ctx" as const, old_no: 2, new_no: 2, text: "return oldName" },
    ] }] }]
    const row = (text: string) => screen.getByText(text).closest("[data-diff-line]") as HTMLElement
    const base = "grid grid-cols-[3rem_3rem_1fr] leading-5"

    it("tints each kind of line, marks it, and leaves no number where a side has none", () => {
      render(<DiffView files={three} />)
      expect(row("const oldName = true").className).toBe(`${base} bg-red-950/40 text-red-200`)
      expect(row("const newName = true").className).toBe(`${base} bg-emerald-950/40 text-emerald-200`)
      expect(row("return oldName").className).toBe(`${base} text-zinc-400`)
      expect(row("return oldName").textContent).toBe("22 return oldName")
      expect(row("const oldName = true").textContent).toBe("1-const oldName = true")
      expect(row("const newName = true").textContent).toBe("1+const newName = true")
      expect(row("const oldName = true").querySelectorAll("button")).toHaveLength(1)
      expect(row("const oldName = true").getAttribute("data-selected")).toBeNull()
    })

    it("tints a noted range more softly, and a selected line not at all", () => {
      const note = { id: "N", task_id: "T", step_id: "S", file_path: "src/new.ts", hunk_anchor: "auth-refresh-anchor", line_start: 1, line_end: 3, body: "All three", created_at: "" }
      const { rerender } = render(<DiffView files={three} hunkNotes={[note]} />)
      expect(row("const oldName = true").className).toBe(`${base} range-tinted-del bg-red-950/[0.25] text-red-100`)
      expect(row("const newName = true").className).toBe(`${base} range-tinted-add bg-emerald-950/[0.25] text-emerald-100`)
      expect(row("return oldName").className).toBe(`${base} bg-amber-950/20 text-amber-100`)
      rerender(<DiffView files={three} selection={{ file_path: "src/new.ts", hunk_anchor: "auth-refresh-anchor", start: 0, end: 2 }} />)
      for (const text of ["const oldName = true", "const newName = true", "return oldName"]) {
        expect(row(text).className).toBe(base)
        expect(row(text).getAttribute("data-selected")).toBe("true")
      }
    })

    it("starts a drag only with the main button, and selects nothing without a handler", () => {
      const selections: unknown[] = []
      const { rerender } = render(<DiffView files={three} onSelectionChange={(next) => selections.push(next)} />)
      fireEvent.pointerDown(screen.getByRole("button", { name: "Select old line 1" }), { button: 2 })
      fireEvent.pointerEnter(row("return oldName"))
      expect(selections).toHaveLength(0)
      rerender(<DiffView files={three} />)
      fireEvent.pointerDown(screen.getByRole("button", { name: "Select old line 1" }), { button: 0 })
      fireEvent.click(screen.getByRole("button", { name: "Select old line 2" }))
      expect(selections).toHaveLength(0)
    })

    it("extends a selection upwards from the gutter, and keeps a drag inside its hunk", () => {
      const selections: { start: number; end: number }[] = []
      const { rerender } = render(<DiffView files={three} selection={{ file_path: "src/new.ts", hunk_anchor: "auth-refresh-anchor", start: 2, end: 2 }} onSelectionChange={(next) => selections.push(next!)} />)
      fireEvent.click(screen.getByRole("button", { name: "Select old line 1" }))
      expect(selections.at(-1)).toMatchObject({ start: 0, end: 2 })
      rerender(<DiffView files={three} onSelectionChange={(next) => selections.push(next!)} />)
      fireEvent.pointerDown(screen.getByRole("button", { name: "Select new line 2" }), { button: 0 })
      fireEvent.pointerEnter(row("const oldName = true"))
      expect(selections.at(-1)).toMatchObject({ start: 0, end: 2 })
      fireEvent.pointerUp(window)
      fireEvent.click(screen.getByRole("button", { name: "Select new line 2" }))
      const count = selections.length
      fireEvent.pointerEnter(row("const newName = true"))
      expect(selections).toHaveLength(count)
    })
  })

  it("shows an empty state when a step has no diff", () => {
    render(<DiffView files={[]} />)
    expect(screen.getByText("No file changes in this step.")).toBeTruthy()
  })
})
