import { describe, expect, it } from "vitest"
import { connectorOf, duplicateOf, idFromTitle, startOf, stepSummary } from "../stepRows"

describe("a step row's summary", () => {
  it("is the first of the type's summary fields with a value, on one line", () => {
    expect(stepSummary({ type: "agent.run", with: { prompt: "Review the pull request\nand say what you find" } })).toBe("Review the pull request")
    expect(stepSummary({ type: "agent.run", with: { macro: "Review", prompt: "unused" } })).toBe("Review")
    expect(stepSummary({ type: "task.move", with: { task: "${{ workflow.task }}", column: "Done" } })).toBe("Done")
    expect(stepSummary({ type: "task.metadata.set", with: { values: { triaged: true } } })).toBe('{"triaged":true}')
  })

  it("is empty when there is nothing to say", () => {
    expect(stepSummary({ type: "agent.run", with: {} })).toBe("")
    expect(stepSummary({ type: "no.such.type", with: { prompt: "x" } })).toBe("")
    expect(stepSummary({ type: "agent.run", with: null })).toBe("")
  })

  it("says what a wait waits for", () => {
    expect(stepSummary({ type: "wait", with: { for: "1h" } })).toBe("1h")
  })
})

describe("how a step starts", () => {
  it("reads v1 trigger: and v2 start:", () => {
    expect(startOf({})).toBe("automatic")
    expect(startOf({ trigger: "manual" })).toBe("manual")
    expect(startOf({ start: "agent_ended" })).toBe("agent_ended")
    expect(startOf({ start: "manual", trigger: "automatic" })).toBe("manual")
  })

  const steps = [
    { id: "prep", title: "Prepare", type: "repo.ensure" },
    { id: "review", title: "Review", type: "agent.run" },
    { id: "note", title: "Note", type: "task.note", trigger: "agent_ended" },
    { id: "ok", title: "Approve", type: "task.move", start: "manual" },
    { id: "done", title: "Done", type: "task.move" },
  ]

  it("is said on the connector before it", () => {
    expect(connectorOf(steps, 0)).toEqual({ kind: "first", text: "" })
    expect(connectorOf(steps, 1)).toEqual({ kind: "automatic", text: "then" })
    expect(connectorOf(steps, 2)).toEqual({ kind: "agent_ended", text: "when Review's agent ends" })
    expect(connectorOf(steps, 3)).toEqual({ kind: "manual", text: "waits for you" })
    expect(connectorOf(steps, 4)).toEqual({ kind: "automatic", text: "then" })
  })

  it("names no step when no agent ran before", () => {
    expect(connectorOf([{ id: "a", type: "task.note", trigger: "agent_ended" }], 0)).toEqual({ kind: "agent_ended", text: "when the agent ends" })
    expect(connectorOf([{ id: "a", type: "task.note", start: "manual" }], 0)).toEqual({ kind: "manual", text: "waits for you" })
  })
})

describe("step IDs", () => {
  it("are made from a title", () => {
    expect(idFromTitle("Review the PR!", [])).toBe("review-the-pr")
    expect(idFromTitle("Review", ["review"])).toBe("review-2")
    expect(idFromTitle("  ", ["step"])).toBe("step-2")
  })

  it("are new for a duplicate, which reads what the original read", () => {
    expect(duplicateOf({ id: "review", title: "Review", type: "agent.run", with: { prompt: "${{ workflow.inputs.pr }}" } }, ["review"])).toEqual({ id: "review-2", title: "Review (copy)", type: "agent.run", with: { prompt: "${{ workflow.inputs.pr }}" } })
  })
})
