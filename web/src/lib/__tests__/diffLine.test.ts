import { describe, expect, it } from "vitest"
import { clickedSelection, draggedSelection, isLineSelected, lineMark, lineTint, type DragOrigin } from "../diffLine"

const place = (index: number, anchor = "h1") => ({ path: "a.ts", anchor, index })
const selection = { file_path: "a.ts", hunk_anchor: "h1", start: 2, end: 4 }

describe("diffLine", () => {
  it("says whether a line is in the selection of its hunk", () => {
    expect(isLineSelected(selection, place(3))).toBe(true)
    expect(isLineSelected(selection, place(5))).toBe(false)
    expect(isLineSelected(selection, place(3, "h2"))).toBe(false)
    expect(isLineSelected(null, place(3))).toBe(false)
  })

  it("grows a selection in the same hunk, and starts afresh elsewhere", () => {
    expect(clickedSelection(selection, place(0))).toEqual({ ...selection, start: 0 })
    expect(clickedSelection(selection, place(7))).toEqual({ ...selection, end: 7 })
    expect(clickedSelection(selection, place(1, "h2"))).toEqual({ file_path: "a.ts", hunk_anchor: "h2", start: 1, end: 1 })
    expect(clickedSelection(undefined, place(1))).toEqual({ file_path: "a.ts", hunk_anchor: "h1", start: 1, end: 1 })
  })

  it("selects across a drag in its own hunk, and marks it moved once it leaves its line", () => {
    const origin: DragOrigin = { filePath: "a.ts", hunkAnchor: "h1", lineIndex: 4, moved: false }
    expect(draggedSelection(origin, place(4))).toEqual({ file_path: "a.ts", hunk_anchor: "h1", start: 4, end: 4 })
    expect(origin.moved).toBe(false)
    expect(draggedSelection(origin, place(1))).toEqual({ file_path: "a.ts", hunk_anchor: "h1", start: 1, end: 4 })
    expect(origin.moved).toBe(true)
    expect(draggedSelection(origin, place(4))?.start).toBe(4)
    expect(origin.moved).toBe(true)
    expect(draggedSelection(origin, place(1, "h2"))).toBeNull()
    expect(draggedSelection(null, place(1))).toBeNull()
  })

  it("tints a line by kind, softly in a range, and not at all when selected in one", () => {
    expect(lineTint("add", false, false)).toEqual(["bg-emerald-950/40 text-emerald-200", false])
    expect(lineTint("ctx", false, true)[1]).toContain("--theme-selection")
    expect(lineTint("del", true, false)).toEqual(["range-tinted-del bg-red-950/[0.25] text-red-100"])
    expect(lineTint("del", true, true)).toEqual([])
    expect([lineMark("add"), lineMark("del"), lineMark("ctx")]).toEqual(["+", "-", " "])
  })
})
