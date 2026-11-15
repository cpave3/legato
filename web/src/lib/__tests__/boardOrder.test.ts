import { describe, expect, it } from "vitest"
import { dropPlacement, dropSlot } from "../boardOrder"

// Three cards 80px tall with 8px between them: midpoints at 40, 128, 216.
const rects = [
  { top: 0, height: 80 },
  { top: 88, height: 80 },
  { top: 176, height: 80 },
]

describe("dropSlot", () => {
  it("is the slot above the first card whose middle is below the pointer", () => {
    expect(dropSlot(rects, -20)).toBe(0)
    expect(dropSlot(rects, 39)).toBe(0)
    expect(dropSlot(rects, 41)).toBe(1)
    expect(dropSlot(rects, 127)).toBe(1)
    expect(dropSlot(rects, 200)).toBe(2)
  })

  it("is the end of the column below the last card's middle", () => {
    expect(dropSlot(rects, 217)).toBe(3)
    expect(dropSlot(rects, 900)).toBe(3)
  })

  it("is 0 in an empty column", () => {
    expect(dropSlot([], 50)).toBe(0)
  })
})

describe("dropPlacement", () => {
  const ids = ["A", "B", "C"]

  it("names the card the dragged one goes before", () => {
    expect(dropPlacement(ids, "C", 0)).toEqual({ before: "A", unchanged: false })
    expect(dropPlacement(ids, "A", 2)).toEqual({ before: "C", unchanged: false })
  })

  it("is null for the end of the column", () => {
    expect(dropPlacement(ids, "A", 3)).toEqual({ before: null, unchanged: false })
  })

  it("is unchanged just above or just below the dragged card itself", () => {
    expect(dropPlacement(ids, "B", 1)).toEqual({ before: "C", unchanged: true })
    expect(dropPlacement(ids, "B", 2)).toEqual({ before: "C", unchanged: true })
    expect(dropPlacement(ids, "C", 3)).toEqual({ before: null, unchanged: true })
  })

  it("places a card from another column among this one's", () => {
    expect(dropPlacement(ids, "X", 1)).toEqual({ before: "B", unchanged: false })
    expect(dropPlacement(ids, "X", 3)).toEqual({ before: null, unchanged: false })
  })
})
