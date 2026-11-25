import { describe, expect, it } from "vitest"
import fixture from "../../../../internal/engine/workflow/testdata/expressions.json"
import { checkExpression, describeReference, locationOf, openExpression, references, type ExpressionLocation } from "../workflowExpressions"

// The table internal/engine/workflow/expressions_test.go runs through the
// engine's Validate: the verdicts here must be the same.
describe("the shared expression table", () => {
  for (const tc of fixture.cases) {
    it(`${tc.expression} in ${tc.location.kind}${"step" in tc.location ? ` of step ${tc.location.step}` : ""}${"whole" in tc && tc.whole ? " (whole)" : ""} is ${tc.valid ? "valid" : "invalid"}`, () => {
      const verdict = checkExpression(fixture.definition, tc.location as ExpressionLocation, tc.expression, "whole" in tc && tc.whole === true)
      expect(verdict === null).toBe(tc.valid)
    })
  }
})

const definition = fixture.definition
const expressions = (location: ExpressionLocation) => references(definition, location).map(r => r.expression)

describe("references", () => {
  it("offers a step's with the inputs, task fields and earlier steps' outputs", () => {
    const offered = expressions({ kind: "with", step: 1 })
    expect(offered).toEqual(expect.arrayContaining([
      "workflow.inputs.task", "workflow.inputs.note", "workflow.inputs.task.title", "workflow.inputs.task.metadata.",
      "workflow.inputs.repo.primary_dir", "workflow.task", "workflow.task.working_dir", "workflow.task.metadata.",
      "steps.first.outputs.summary", "steps.first.outputs.report.path", "outputs.log.path",
    ]))
    expect(offered.filter(e => e.startsWith("steps.second") || e.startsWith("steps.third"))).toEqual([])
    expect(offered).not.toContain("workflow.inputs.note.title")
  })

  it("offers run_title the inputs alone", () => {
    const offered = expressions({ kind: "run_title" })
    expect(offered).toContain("workflow.inputs.note")
    expect(offered.filter(e => e.startsWith("workflow.task") || e.startsWith("steps."))).toEqual([])
  })

  it("offers a trigger's inputs that trigger's context", () => {
    expect(expressions({ kind: "trigger", trigger: "schedule" })).toEqual(["schedule.time", "schedule.previous_time"])
    expect(expressions({ kind: "trigger", trigger: "pr.changes_requested" })).toEqual(["event.task", "event.repo", "event.pr_number", "event.pr_url", "event.reviewer"])
    expect(expressions({ kind: "trigger", trigger: "task.entered_column" })).toEqual(expect.arrayContaining(["event.task", "task.title", "task.metadata."]))
  })

  it("every offer but a metadata path's start is valid where it is offered", () => {
    for (const location of [{ kind: "with", step: 2 }, { kind: "run_title" }, { kind: "output_path", step: 1, output: "log" }] as ExpressionLocation[]) {
      for (const expression of expressions(location).filter(e => !e.endsWith("."))) {
        expect(checkExpression(definition, location, expression), `${expression} in ${location.kind}`).toBeNull()
      }
    }
  })
})

describe("locationOf", () => {
  it("knows each place that takes expressions", () => {
    expect(locationOf(["run_title"])).toEqual({ kind: "run_title" })
    expect(locationOf(["steps", 2, "with", "prompt"])).toEqual({ kind: "with", step: 2 })
    expect(locationOf(["steps", 2, "with", "values", "a"])).toEqual({ kind: "with", step: 2 })
    expect(locationOf(["steps", 1, "outputs", "log", "path"])).toEqual({ kind: "output_path", step: 1, output: "log" })
    expect(locationOf(["on", "schedule", "inputs", "since"])).toEqual({ kind: "trigger", trigger: "schedule" })
    expect(locationOf(["title"])).toBeNull()
  })
})

it("describes a reference", () => {
  expect(describeReference(definition, { kind: "with", step: 1 }, "workflow.inputs.note")).toBe("A workflow input: note (string)")
  expect(describeReference(definition, { kind: "with", step: 1 }, "steps.first.outputs.summary")).toContain("summary of step first (string)")
})

it("finds the open ${{ on a line", () => {
  expect(openExpression("      prompt: Look at ${{ workflow.in")).toEqual({ prefix: "workflow.in" })
  expect(openExpression("      prompt: ${{ workflow.task }} and ")).toBeNull()
})

describe("in a file", async () => {
  const { expressionAt, expressionCompletions, expressionDiagnostics } = await import("../workflowExpressions")
  const file = `schema_version: 1
name: x
title: X
run_title: Run for \${{ workflow.task.title }}
inputs:
  task: {type: task, required: true}
steps:
  - id: first
    title: First
    type: agent.run
    with:
      prompt: Look at \${{ workflow.inputs.task.title }}
    outputs:
      summary: {type: string}
  - id: second
    title: Second
    type: agent.run
    with:
      prompt: |
        Read \${{ steps.first.outputs.summary }}
        and \${{ steps.second.outputs.nope }}
`
  it("marks the expressions the engine refuses", () => {
    const problems = expressionDiagnostics(file)
    expect(problems.map(p => file.slice(p.from, p.to))).toEqual(["${{ workflow.task.title }}", "${{ steps.second.outputs.nope }}"])
  })

  it("completes inside ${{, in a block scalar too", () => {
    const at = file.indexOf("steps.first.outputs.summary") + "steps.fi".length
    const offered = expressionCompletions(file, at)!
    expect(file.slice(offered.from, at)).toBe("steps.fi")
    expect(offered.options.map(o => o.expression)).toContain("steps.first.outputs.summary")
    expect(offered.options.map(o => o.expression)).not.toContain("steps.second.outputs.nope")
    expect(expressionCompletions(file, file.indexOf("title: First") + 3)).toBeNull()
  })

  it("says what an expression reads", () => {
    expect(expressionAt(file, file.indexOf("workflow.inputs.task.title") + 3)?.description).toBe("A field of a task input: task (task)")
  })
})
