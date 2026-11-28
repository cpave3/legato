import { describe, expect, it } from "vitest"
import { itemsOf, parseWorkflow, problemsByItem } from "../../../lib/workflowDocument"
import type { WorkflowGraphModel } from "../../../lib/workflow"
import { fromTheCard, nodeOf, orderOf, placeOf, rowsOf, selectionOfNode } from "../outlineRows"

const text = `schema_version: 2
name: demo
title: Demo
inputs:
  pr: {type: integer}
  task: {type: task}
on:
  task.entered_column:
    column: Doing
    inputs:
      task: "\${{ event.task }}"
  schedule:
    every: weekdays
    enabled: false
steps:
  - id: each
    title: Each
    type: loop
    steps:
      - id: inner
        type: task.note
  - type: wait
`

const rowsFor = (source: string, pausedHere: Record<string, boolean> = {}, diagnostics = [] as Parameters<typeof problemsByItem>[1]) =>
  rowsOf(itemsOf(source).items, parseWorkflow(source).values, problemsByItem(source, diagnostics), pausedHere)

describe("rowsOf", () => {
  it("makes a row of each trigger, input and step, nested steps under their parent", () => {
    const rows = rowsFor(text)
    expect(rows.when.map(r => [r.label, r.paused, r.on])).toEqual([["A card enters Doing", false, true], ["Every weekday", true, false]])
    expect(rows.asks.map(r => [r.label, r.detail])).toEqual([["pr", "integer"], ["task", "from the card"]])
    expect(rows.then.map(r => [r.selection, r.label, r.depth, r.type])).toEqual([
      [{ kind: "step", path: "each" }, "Each", 0, "loop"],
      [{ kind: "step", path: "each/inner" }, "inner", 1, "task.note"],
      [{ kind: "step", path: "#1" }, "#1", 0, "wait"],
    ])
    expect(rows.about).toEqual({ problems: 0, problem: null, containers: [] })
  })

  it("takes a trigger's paused state from this Legato's switch when it has one", () => {
    expect(rowsFor(text, { schedule: false }).when[1].paused).toBe(false)
  })

  it("counts errors on their row and on About, not warnings", () => {
    const at = (line: number, severity: "error" | "warning" = "error") => ({ severity, message: "x", line, column: 3, end_line: line, end_column: 4 })
    const rows = rowsFor(text, {}, [at(3), at(5), at(5, "warning")])
    expect(rows.about.problems).toBe(1)
    expect(rows.asks[0].problems).toBe(1)
  })

  it("keeps a container the forms can't show for its section, and a field's problem for About", () => {
    const rows = rowsFor("schema_version: 2\nname: x\ninputs: &a {}\ntitle: &t T\n")
    expect(rows.about.containers.map(c => c.item)).toEqual([{ kind: "about", key: "inputs" }])
    expect(rows.about.problem).toMatch(/anchor/)
  })
})

describe("orderOf and placeOf", () => {
  it("orders every row with About last, and names a row's place among its kind", () => {
    const order = orderOf(rowsFor(text))
    expect(order.map(r => r.selection.kind)).toEqual(["trigger", "trigger", "input", "input", "step", "step", "step", "about"])
    expect(placeOf(order, { kind: "step", path: "each/inner" })).toBe("step:1")
    expect(placeOf(order, { kind: "about" })).toBe("about:0")
  })
})

describe("fromTheCard", () => {
  it("lists the inputs a column trigger fills from its card", () => {
    expect(fromTheCard(parseWorkflow(text).values)).toEqual(["task"])
    expect(fromTheCard({})).toEqual([])
  })
})

describe("nodeOf and selectionOfNode", () => {
  const graph = { nodes: [
    { id: "step:each", kind: "step", step: { id: "each", path: "each" } },
    { id: "inner:1", kind: "inner", parent: "step:each" },
    { id: "trigger:schedule", kind: "trigger", trigger: { kind: "schedule" } },
    { id: "trigger:manual", kind: "trigger", trigger: { kind: "manual" } },
  ] } as unknown as WorkflowGraphModel

  it("names a row's node, and a node's row", () => {
    expect(nodeOf({ kind: "step", path: "each" })).toBe("step:each")
    expect(nodeOf({ kind: "trigger", name: "schedule" })).toBe("trigger:schedule")
    expect(nodeOf({ kind: "about" })).toBeNull()
    expect(nodeOf(null)).toBeNull()
    expect(selectionOfNode(graph, "inner:1")).toEqual({ kind: "step", path: "each" })
    expect(selectionOfNode(graph, "trigger:schedule")).toEqual({ kind: "trigger", name: "schedule" })
    expect(selectionOfNode(graph, "trigger:manual")).toBeNull()
    expect(selectionOfNode(graph, null)).toBeNull()
  })
})
