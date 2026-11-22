import { readdirSync, readFileSync } from "node:fs"
import { join } from "node:path"
import { describe, expect, it } from "vitest"
import { itemKey, itemsOf, itemText, pathAt, problemsByItem, rangeOf, setItemText, stepYamlPath, type ItemRef } from "../workflowDocument"
import type { WorkflowDiagnostic } from "../workflowFiles"

// The parts of a file the outline shows (LEG-503): where each is in the
// text, the innermost one at a cursor, and whether each can be shown as a
// form.

const v1 = `# A v1 workflow.
schema_version: 1
name: paths
title: Paths # the title
description: What it does
inputs:
  task: {type: task, required: true}
  note:
    type: string # free text
on:
  schedule:
    cron: "0 9 * * *"
  task.entered_column:
    column: Doing
steps:
  # The first step.
  - id: first
    title: First
    type: agent.run
    with:
      prompt: Look at \${{ workflow.inputs.note }} # inline
  - id: second
    title: Second
    trigger: manual
    type: task.note
    with:
      task: \${{ workflow.inputs.task }}
      body: |
        Line one
        Line two
`

const v2 = `schema_version: 2
name: nested
title: Nested
steps:
  - id: prep
    title: Prep
    type: repo.ensure
    start: manual
  - id: each
    title: Each
    type: loop
    steps:
      - id: review
        title: Review
        type: agent.run
        with:
          prompt: Review it
      - id: note
        title: Note
        type: task.note
        start: agent_ended
`

const slice = (text: string, item: ItemRef, field: (string | number)[] = []) => {
  const range = rangeOf(text, item, field)
  return range ? text.slice(range.from, range.to) : null
}

describe("rangeOf", () => {
  it("finds a top-level field, an input and a trigger", () => {
    expect(slice(v1, { kind: "about", key: "title" })).toBe("title: Paths # the title")
    expect(slice(v1, { kind: "input", name: "task" })).toBe("task: {type: task, required: true}")
    expect(slice(v1, { kind: "input", name: "note" })).toBe("note:\n    type: string # free text")
    expect(slice(v1, { kind: "trigger", name: "task.entered_column" })).toBe("task.entered_column:\n    column: Doing")
  })

  it("finds a step by its path, from its dash, and a field of it", () => {
    expect(slice(v1, { kind: "step", path: "first" })).toBe("- id: first\n    title: First\n    type: agent.run\n    with:\n      prompt: Look at ${{ workflow.inputs.note }} # inline")
    expect(slice(v1, { kind: "step", path: "second" }, ["with", "body"])).toBe("body: |\n        Line one\n        Line two")
    expect(slice(v1, { kind: "step", path: "second" }, ["trigger"])).toBe("trigger: manual")
  })

  it("finds nested steps, and a run's path with its pass", () => {
    expect(slice(v2, { kind: "step", path: "each/review" }, ["with", "prompt"])).toBe("prompt: Review it")
    expect(slice(v2, { kind: "step", path: "each/2/review" }, ["with", "prompt"])).toBe("prompt: Review it")
    expect(slice(v2, { kind: "step", path: "each/note" }, ["start"])).toBe("start: agent_ended")
    expect(stepYamlPath(v2, "each/note")).toEqual(["steps", 1, "steps", 1])
  })

  it("is null for what is not there", () => {
    expect(rangeOf(v1, { kind: "step", path: "third" })).toBeNull()
    expect(rangeOf(v1, { kind: "step", path: "first" }, ["with", "macro"])).toBeNull()
    expect(rangeOf(v1, { kind: "trigger", name: "pr.changes_requested" })).toBeNull()
    expect(rangeOf("steps: [", { kind: "step", path: "first" })).toBeNull()
  })
})

describe("pathAt", () => {
  const at = (text: string, needle: string, shift = 0) => pathAt(text, text.indexOf(needle) + shift)

  it("is the innermost item and field at a position", () => {
    expect(at(v1, "workflow.inputs.note")).toEqual({ item: { kind: "step", path: "first" }, field: ["with", "prompt"] })
    expect(at(v1, "Line two")).toEqual({ item: { kind: "step", path: "second" }, field: ["with", "body"] })
    expect(at(v1, "free text")).toEqual({ item: { kind: "input", name: "note" }, field: ["type"] })
    expect(at(v1, "Doing")).toEqual({ item: { kind: "trigger", name: "task.entered_column" }, field: ["column"] })
    expect(at(v1, "Paths")).toEqual({ item: { kind: "about", key: "title" }, field: [] })
  })

  it("is the step itself on its dash, and nested steps by path", () => {
    expect(at(v1, "- id: second")).toEqual({ item: { kind: "step", path: "second" }, field: [] })
    expect(at(v1, "id: second")).toEqual({ item: { kind: "step", path: "second" }, field: ["id"] })
    expect(at(v2, "Review it")).toEqual({ item: { kind: "step", path: "each/review" }, field: ["with", "prompt"] })
    expect(at(v2, "title: Each")).toEqual({ item: { kind: "step", path: "each" }, field: ["title"] })
  })

  it("is null between items, in a comment above them, and in a file that does not parse", () => {
    expect(at(v1, "# The first step.")).toBeNull()
    expect(at(v1, "# A v1")).toBeNull()
    expect(pathAt("steps: [", 3)).toBeNull()
  })

  it("round-trips with rangeOf", () => {
    for (const needle of ["Look at", "Line one", "free text", "Doing", "Review it"]) {
      const text = needle === "Review it" ? v2 : v1
      const found = at(text, needle)!
      const range = rangeOf(text, found.item, found.field)!
      expect(range.from).toBeLessThanOrEqual(text.indexOf(needle))
      expect(range.to).toBeGreaterThan(text.indexOf(needle))
    }
  })
})

describe("itemsOf", () => {
  it("lists every item in the file's order, each with no problem", () => {
    const { file, items } = itemsOf(v1)
    expect(file).toBeNull()
    expect(items.map(i => itemKey(i.item))).toEqual([
      "about:schema_version", "about:name", "about:title", "about:description",
      "input:task", "input:note", "trigger:schedule", "trigger:task.entered_column", "step:first", "step:second",
    ])
    expect(items.every(i => i.problem === null)).toBe(true)
  })

  it("lists nested steps after their parent", () => {
    expect(itemsOf(v2).items.filter(i => i.item.kind === "step").map(i => itemKey(i.item))).toEqual(["step:prep", "step:each", "step:each/review", "step:each/note"])
  })

  it("reports an anchor or an odd shape on its item alone", () => {
    const text = `schema_version: 1
name: odd
title: Odd
on:
  schedule: daily
  task.entered_column:
    column: Doing
steps:
  - &base
    id: first
    title: First
    type: agent.run
    with: {prompt: hi}
  - id: second
    title: Second
    type: task.note
    with: [not, a, map]
  - id: third
    title: Third
    type: task.move
    with:
      <<: {column: Done}
  - id: fourth
    title: Fourth
    type: task.move
    with:
      column: Done
`
    const { file, items } = itemsOf(text)
    expect(file).toBeNull()
    const problem = (key: string) => items.find(i => itemKey(i.item) === key)?.problem
    expect(problem("step:first")).toMatch(/anchor/)
    expect(problem("step:second")).toMatch(/with .*not a mapping/)
    expect(problem("step:third")).toMatch(/merge key/)
    expect(problem("step:fourth")).toBeNull()
    expect(problem("trigger:schedule")).toMatch(/not a mapping/)
    expect(problem("trigger:task.entered_column")).toBeNull()
  })

  it("reports an alias on the item that uses it", () => {
    const text = `schema_version: 1
name: alias
title: Alias
inputs:
  note: &note {type: string}
steps:
  - id: first
    title: First
    type: agent.run
    with:
      prompt: *note
`
    const { items } = itemsOf(text)
    expect(items.find(i => itemKey(i.item) === "input:note")?.problem).toMatch(/anchor/)
    expect(items.find(i => itemKey(i.item) === "step:first")?.problem).toMatch(/alias/)
  })

  it("stops only at a file that does not parse, or is not a mapping", () => {
    expect(itemsOf("steps: [").file).toMatch(/does not parse/)
    expect(itemsOf("steps: [").items).toEqual([])
    expect(itemsOf("- a\n- b\n").file).toMatch(/not a mapping/)
    expect(itemsOf("schema_version: 1\nsteps: oops\n").items.find(i => i.item.kind === "about" && i.item.key === "steps")?.problem).toMatch(/not a list/)
  })

  it("shows every example Legato ships, and finds each item where it lists it", () => {
    const dir = join(__dirname, "../../../../internal/engine/workflow/examples")
    for (const name of readdirSync(dir).filter(n => n.endsWith(".yaml"))) {
      const text = readFileSync(join(dir, name), "utf8")
      const { file, items } = itemsOf(text)
      expect(file, name).toBeNull()
      for (const { item, range, problem } of items) {
        expect(problem, `${name} ${itemKey(item)}`).toBeNull()
        expect(rangeOf(text, item), `${name} ${itemKey(item)}`).toEqual(range)
        expect(pathAt(text, range.from + 1)?.item, `${name} ${itemKey(item)}`).toEqual(item)
      }
    }
  })

  it("gives each item its range", () => {
    const second = itemsOf(v1).items.find(i => itemKey(i.item) === "step:second")!
    expect(v1.slice(second.range.from, second.range.to)).toMatch(/^- id: second[\s\S]*Line two$/)
  })
})

describe("problemsByItem", () => {
  const d = (path: string | undefined, line = 1, column = 1): WorkflowDiagnostic => ({ severity: "error", message: path ?? "somewhere", path, line, column, end_line: line, end_column: column + 1 })

  it("matches the server's diagnostics to items by path", () => {
    const byItem = problemsByItem(v1, [d("steps[1].with.body"), d("steps[0]"), d("on.schedule.cron"), d("inputs.note.type"), d("title"), d("steps")])
    expect(byItem.get("step:second")?.map(x => x.message)).toEqual(["steps[1].with.body"])
    expect(byItem.get("step:first")?.map(x => x.message)).toEqual(["steps[0]"])
    expect(byItem.get("trigger:schedule")?.map(x => x.message)).toEqual(["on.schedule.cron"])
    expect(byItem.get("input:note")?.map(x => x.message)).toEqual(["inputs.note.type"])
    expect(byItem.get("about:title")?.map(x => x.message)).toEqual(["title"])
    expect(byItem.get("file")?.map(x => x.message)).toEqual(["steps"])
  })

  it("matches nested steps, and a diagnostic with no path by its place", () => {
    const line = v2.split("\n").findIndex(l => l.includes("Review it")) + 1
    const byItem = problemsByItem(v2, [d("steps[1].steps[0].with.prompt"), d(undefined, line, 20)])
    expect(byItem.get("step:each/review")?.length).toBe(2)
  })
})

describe("an item's own YAML", () => {
  it("is the item's text, out of its place, and writes back to that place alone", () => {
    expect(itemText(v1, { kind: "step", path: "second" })).toBe("id: second\ntitle: Second\ntrigger: manual\ntype: task.note\nwith:\n  task: ${{ workflow.inputs.task }}\n  body: |\n    Line one\n    Line two")
    expect(itemText(v1, { kind: "input", name: "note" })).toBe("note:\n  type: string # free text")
    const edited = setItemText(v1, { kind: "step", path: "second" }, "id: second\ntitle: Renamed\ntype: task.note\nwith:\n  body: one line")
    const range = rangeOf(v1, { kind: "step", path: "second" })!
    expect(edited.text.slice(0, range.from)).toBe(v1.slice(0, range.from))
    expect(edited.text.slice(range.from)).toBe("- id: second\n    title: Renamed\n    type: task.note\n    with:\n      body: one line\n")
    expect(edited.undo.label).toBe("Edit step second as YAML")
  })

  it("leaves the file as it was when written back unchanged", () => {
    for (const item of itemsOf(v2).items.map(i => i.item)) expect(setItemText(v2, item, itemText(v2, item)!).text, itemKey(item)).toBe(v2)
    for (const item of itemsOf(v1).items.map(i => i.item)) expect(setItemText(v1, item, itemText(v1, item)!).text, itemKey(item)).toBe(v1)
  })

  it("edits a nested step at its depth, the steps around it byte for byte", () => {
    const edited = setItemText(v2, { kind: "step", path: "each/review" }, "id: review\ntitle: Look\ntype: agent.run")
    expect(edited.text).toBe(v2.replace("      - id: review\n        title: Review\n        type: agent.run\n        with:\n          prompt: Review it\n", "      - id: review\n        title: Look\n        type: agent.run\n"))
  })
})
