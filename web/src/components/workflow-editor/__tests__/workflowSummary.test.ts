import { readFileSync } from "node:fs"
import { join } from "node:path"
import { describe, expect, it } from "vitest"
import { blankWorkflow, copyOf, startLines, summaryLine } from "../workflowSummary"

const triage = readFileSync(join(__dirname, "../../../../../internal/engine/workflow/examples/triage.workflow.yaml"), "utf8")

describe("a workflow summed up", () => {
  it("is one line: what starts it, what it asks for, how many steps", () => {
    expect(summaryLine("schema_version: 1\nname: a\ntitle: A\non:\n  task.entered_column: {column: Review}\ninputs:\n  a: {type: string}\n  b: {type: string}\nsteps:\n  - {id: x, type: task.note}\n  - {id: y, type: task.note}\n  - {id: z, type: task.note}\n"))
      .toBe("Starts when a card enters Review · asks for 2 inputs · 3 steps")
    expect(summaryLine("schema_version: 1\nname: a\ntitle: A\nsteps:\n  - {id: x, type: task.note}\n")).toBe("Runs by hand · asks for nothing · 1 step")
    expect(summaryLine("name: [oops")).toBe("")
    expect(summaryLine("just text")).toBe("")
  })

  it("is a card's When, Asks for and Does lines", () => {
    const lines = startLines(triage)
    expect(lines.when).toMatch(/^A card enters/)
    expect(lines.does.split(" → ").length).toBeGreaterThan(0)
    expect(startLines("schema_version: 1\nname: a\ntitle: A\nsteps: []\n")).toEqual({ when: "By hand", asks: "Nothing", does: "Nothing yet" })
  })
})

describe("starting points", () => {
  it("a blank workflow has a free name and no steps", () => {
    expect(blankWorkflow(["new-workflow"])).toBe("schema_version: 1\nname: new-workflow-2\ntitle: New workflow\nsteps: []\n")
  })

  it("a copy takes a free name and says it is a copy, the rest kept byte for byte", () => {
    const copy = copyOf("# mine\nschema_version: 1\nname: review\ntitle: Review # it\nsteps: []\n", ["review", "review-copy"])
    expect(copy).toBe("# mine\nschema_version: 1\nname: review-copy-2\ntitle: Review (copy) # it\nsteps: []\n")
  })
})
