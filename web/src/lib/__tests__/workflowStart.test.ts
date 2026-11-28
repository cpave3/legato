import { describe, expect, it } from "vitest"
import type { WorkflowDefinition, WorkflowRunView } from "../workflow"
import { afterStartPath, declaredValues, firstValues, missingInput, presetValues, refusalOf, shownInputs, shownValue } from "../workflowStart"

const definition = { schema_version: 2, name: "w", title: "W", source: "w.yaml", steps: [], inputs: {
  task: { type: "task", required: true },
  count: { type: "integer", default: 2 },
  mode: { type: "string", choices: ["fast", "safe"] },
  notify: { type: "boolean" },
  note: { type: "string" },
  hidden: { type: "string", default: "h" },
} } as unknown as WorkflowDefinition

describe("the form's inputs and values", () => {
  const inputs = shownInputs(definition, ["hidden"])

  it("leaves hidden inputs out", () => {
    expect(Object.keys(inputs)).toEqual(["task", "count", "mode", "notify", "note"])
  })

  it("starts each input at the value given, its default, its first choice or false", () => {
    expect(firstValues(inputs, { task: "T-1", count: "5" })).toEqual({ task: "T-1", count: "5", mode: "fast", notify: "false" })
    expect(firstValues(inputs, {})).toEqual({ count: "2", mode: "fast", notify: "false" })
  })

  it("finds a required input with no value, sends only declared values, and shows a default", () => {
    expect(missingInput(inputs, {})).toBe("task")
    expect(missingInput(inputs, { task: "T" })).toBeUndefined()
    expect(declaredValues({ task: "T", stray: "x" }, inputs)).toEqual({ task: "T" })
    expect(shownValue({}, "count", inputs.count)).toBe("2")
    expect(shownValue({ count: "" }, "count", inputs.count)).toBe("")
  })

  it("fills the repository inputs from a preset", () => {
    expect(presetValues({ name: "L", repo: "a/b", primary_dir: "/l" }, ["repo", "base_branch"])).toEqual({ repo: "a/b", base_branch: "" })
    expect(presetValues({ name: "L", repo: "a/b", primary_dir: "/l", base_branch: "dev" }, ["primary_dir", "base_branch"])).toEqual({ primary_dir: "/l", base_branch: "dev" })
  })
})

describe("a start", () => {
  const view = (status: string, steps: unknown[] | null, error?: string) => ({ run: { id: "wr-1", status, error }, steps }) as unknown as WorkflowRunView

  it("is refused with its link, naming a held review loop", () => {
    expect(refusalOf(view("failed", [{ status: "failed", error: "held", error_link: "/l", error_code: "author_loop_active" }]))).toEqual({ error: "held", link: { to: "/l", label: "Open the review loop" } })
    expect(refusalOf(view("failed", [{ status: "failed", error_link: "/x" }], "run error"))).toEqual({ error: "run error", link: { to: "/x", label: "Open" } })
    expect(refusalOf(view("failed", [{ status: "failed", error_link: "/x" }]))?.error).toBe("The workflow could not start")
    expect(refusalOf(view("failed", [{ status: "failed", error: "no link" }]))).toBeNull()
    expect(refusalOf(view("failed", null))).toBeNull()
    expect(refusalOf(view("running", [{ status: "failed", error_link: "/x" }]))).toBeNull()
  })

  it("leads to the run, the task's agent, or nowhere", () => {
    const inputs = shownInputs(definition, [])
    expect(afterStartPath(definition, inputs, {}, "wr-1")).toBe("/workflows/runs/wr-1")
    expect(afterStartPath({ ...definition, after_start: "agent" }, inputs, { task: "T 1" }, "wr-1")).toBe("/agents?task=T%201")
    expect(afterStartPath({ ...definition, after_start: "agent" }, {}, {}, "wr-1")).toBe("/agents?task=")
    expect(afterStartPath({ ...definition, after_start: "stay" }, inputs, {}, "wr-1")).toBeNull()
  })
})
