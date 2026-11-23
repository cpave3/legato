import { describe, expect, it } from "vitest"
import { addressOf, selectionOf, type Selection } from "../workflowAddress"

describe("a selection's address", () => {
  const cases: [Selection, string][] = [
    [{ kind: "about" }, "about"],
    [{ kind: "input", name: "pr" }, "inputs/pr"],
    [{ kind: "trigger", name: "task.entered_column" }, "on/task.entered_column"],
    [{ kind: "step", path: "review" }, "steps/review"],
    [{ kind: "step", path: "each/review" }, "steps/each/review"],
    [{ kind: "input", name: "a b/c" }, "inputs/a%20b%2Fc"],
  ]
  for (const [selection, address] of cases) {
    it(`is ${address}`, () => {
      expect(addressOf(selection)).toBe(address)
      expect(selectionOf(address)).toEqual(selection)
      expect(selectionOf("#" + address)).toEqual(selection)
    })
  }

  it("is nothing for no address or one it doesn't know", () => {
    expect(selectionOf("")).toBeNull()
    expect(selectionOf("#")).toBeNull()
    expect(selectionOf("steps/")).toBeNull()
    expect(selectionOf("elsewhere/x")).toBeNull()
    expect(selectionOf("inputs/%E0%A4%A")).toBeNull()
  })

  it("reads an about field's item as About", () => {
    expect(addressOf({ kind: "about", key: "title" })).toBe("about")
  })
})
