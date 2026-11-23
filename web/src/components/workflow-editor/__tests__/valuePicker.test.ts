import { describe, expect, it } from "vitest"
import { valueGroups } from "../valuePicker"

const values = {
  inputs: { pr: { type: "integer", description: "The pull request" }, target: { type: "task" } },
  steps: [
    { id: "prep", title: "Prepare", type: "repo.ensure", outputs: { working_dir: { type: "string", description: "Where it is" } } },
    { id: "review", title: "Review", type: "agent.run", outputs: { summary: { type: "string" }, approved: { type: "boolean" } } },
    { id: "note", title: "Note", type: "task.note" },
    { id: "later", title: "Later", type: "agent.run", outputs: { x: { type: "string" } } },
  ],
}

describe("the values a field can read", () => {
  it("are grouped as This run, What started it and each earlier step", () => {
    const groups = valueGroups(values, { kind: "with", step: 2 }, "text")
    expect(groups.map(g => g.group)).toEqual(["This run", "Prepare", "Review"])
    expect(groups[1].entries.map(e => [e.expression, e.name, e.type, e.description])).toContainEqual(["steps.prep.outputs.working_dir", "working_dir", "string", "Where it is"])
  })

  it("are only those reachable from here: earlier steps, never this one or later", () => {
    const expressions = valueGroups(values, { kind: "with", step: 1 }, "text").flatMap(g => g.entries.map(e => e.expression))
    expect(expressions).toContain("steps.prep.outputs.working_dir")
    expect(expressions.some(e => e.startsWith("steps.review.") || e.startsWith("steps.later."))).toBe(false)
  })

  it("names this run's values plainly", () => {
    const run = valueGroups(values, { kind: "with", step: 2 }, "text")[0]
    expect(run.entries.find(e => e.expression === "workflow.inputs.pr")).toMatchObject({ name: "pr", type: "integer", description: "The pull request" })
    expect(run.entries.find(e => e.expression === "workflow.task")).toMatchObject({ name: "The workflow's task" })
    expect(run.entries.find(e => e.expression === "workflow.inputs.target.title")).toMatchObject({ name: "target · title" })
  })

  it("puts a trigger's own values under What started it", () => {
    const groups = valueGroups(values, { kind: "trigger", trigger: "task.entered_column" }, "text")
    expect(groups.map(g => g.group)).toContain("What started it")
    expect(groups.find(g => g.group === "What started it")!.entries.map(e => e.expression)).toContain("event.task")
  })

  it("dims a value of the wrong type, with the reason", () => {
    const groups = valueGroups(values, { kind: "with", step: 2 }, "integer")
    const all = groups.flatMap(g => g.entries)
    expect(all.find(e => e.expression === "workflow.inputs.pr")!.wrongType).toBeUndefined()
    expect(all.find(e => e.expression === "steps.review.outputs.approved")!.wrongType).toBe("a boolean, not a whole number")
    expect(all.find(e => e.expression === "steps.prep.outputs.working_dir")!.wrongType).toBe("text, not a whole number")
    const forBoolean = valueGroups(values, { kind: "with", step: 2 }, "boolean").flatMap(g => g.entries)
    expect(forBoolean.find(e => e.expression === "steps.review.outputs.approved")!.wrongType).toBeUndefined()
  })
})
