import { describe, expect, it } from "vitest"
import { picker } from "./picker"

describe("picker", () => {
  it("lists the Legatos saved in the Legato app, one line each, to open", () => {
    const page = picker([{ id: "a", name: "Desk" }, { id: "b", name: "Laptop" }])

    expect(page.items).toEqual([
      { text: "Desk", open: "a" },
      { text: "Laptop", open: "b" },
    ])
    expect(page.title).toBe("Legato")
    expect(page.status).toBe("")
  })

  it("cuts a long name to the list's width", () => {
    const page = picker([{ id: "a", name: "A Legato with a name far too long to fit on one line of the glasses' list" }])

    expect(page.items[0].text).toMatch(/…$/)
    expect(page.items[0].text.length).toBeLessThan(70)
  })

  it("says to add a Legato in the Legato app when it has none", () => {
    expect(picker([]).items).toEqual([{ text: "Add a Legato in the Legato app." }])
  })

  it("says it is reading before the first answer, and shows a problem in the header", () => {
    expect(picker(undefined).items).toEqual([{ text: "Reading the Legato app…" }])
    const page = picker(undefined, { problem: "Not paired with the Legato app", read: true })
    expect(page.status).toBe("Not paired with the Legato app")
    expect(page.items).toEqual([{ text: "No Legatos to show." }])
  })

  it("keeps the last list when a read fails", () => {
    const page = picker([{ id: "a", name: "Desk" }], { problem: "Can't reach the Legato app on this phone" })

    expect(page.items).toEqual([{ text: "Desk", open: "a" }])
    expect(page.status).toBe("Can't reach the Legato app on this phone")
  })

  it("ends a list too long for the glasses with how many more there are", () => {
    const many = Array.from({ length: 25 }, (_, i) => ({ id: `i${i}`, name: `Legato ${i}` }))

    const page = picker(many)

    expect(page.items).toHaveLength(20)
    expect(page.items[19]).toEqual({ text: "+6 more" })
  })
})
