import { describe, expect, it } from "vitest"
import { parseWorkflow, stepsOf } from "../../../lib/workflowDocument"
import { followRenamed, pastedIds, pasteSteps } from "../pasteSteps"
import { freeName, moveProblem, readersOf, stepListOf } from "../useOutlineEditing"
import type { StepValue } from "../stepForms"

const text = `schema_version: 2
name: demo
steps:
  - id: prep
    type: repo.ensure
  - id: run
    type: agent.run
    with:
      prompt: \${{ steps.prep.outputs.path }}
`
const list = stepListOf(stepsOf(parseWorkflow(text).values) as (StepValue | null)[])

describe("pastedIds", () => {
  it("keeps a pasted step's own ID, numbers one taken, and makes one from a title or type", () => {
    expect(pastedIds([{ id: "new" }, { id: "prep" }, { title: "Do it" }, { type: "wait" }, {}], ["prep", "run"])).toEqual(["new", "prep-2", "do-it", "wait", "step"])
  })
})

describe("followRenamed", () => {
  it("points references to renamed steps at their new IDs, in expressions only", () => {
    const renamed = new Map([["prep", "prep-2"]])
    expect(followRenamed({ a: ["${{ steps.prep.outputs.x }} steps.prep.outputs.y", 3] }, renamed)).toEqual({ a: ["${{ steps.prep-2.outputs.x }} steps.prep.outputs.y", 3] })
    expect(followRenamed("${{ steps.other.outputs.x }}", renamed)).toBe("${{ steps.other.outputs.x }}")
    expect(followRenamed("${{ steps.prep.outputs.x }}", new Map())).toBe("${{ steps.prep.outputs.x }}")
  })
})

describe("pasteSteps", () => {
  it("inserts after the step chosen, renaming and following its own references", () => {
    const { edit, added } = pasteSteps(text, list.placed, list.ids, "prep", [{ id: "prep", type: "repo.ensure" }, { id: "use", type: "task.note", with: { note: "${{ steps.prep.outputs.path }}" } }])
    expect(added).toEqual(["prep-2", "use"])
    expect(edit.undo).toEqual({ label: "Paste 2 steps", before: text, after: edit.text })
    const ids = stepsOf(parseWorkflow(edit.text).values).map(step => step.id)
    expect(ids).toEqual(["prep", "prep-2", "use", "run"])
    expect(edit.text).toContain("note: ${{ steps.prep-2.outputs.path }}")
  })

  it("inserts at the end when no step is chosen", () => {
    const { edit } = pasteSteps(text, list.placed, list.ids, null, [{ id: "last", type: "wait" }])
    expect(edit.undo.label).toBe("Paste step last")
    expect(stepsOf(parseWorkflow(edit.text).values).map(step => step.id)).toEqual(["prep", "run", "last"])
  })
})

describe("the step list", () => {
  it("names who reads a step, why a move would break a reference, and a free name", () => {
    expect(readersOf(list.steps, "prep").map(step => step.id)).toEqual(["run"])
    expect(moveProblem(list, "run", 0)).toMatch(/^That move would break a reference: /)
    expect(moveProblem(list, "run", 1)).toBe("")
    expect(freeName("input", ["input", "input-2"])).toBe("input-3")
    expect(freeName("task", [])).toBe("task")
  })
})
