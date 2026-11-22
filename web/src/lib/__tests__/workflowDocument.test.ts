import { readdirSync, readFileSync } from "node:fs"
import { join } from "node:path"
import { describe, expect, it } from "vitest"
import { addInput, insertStep, moveStep, parseWorkflow, removeInput, removeStep, renameInput, renameStep, setField, setTrigger, stepsOf, WorkflowDocumentError } from "../workflowDocument"
import { schemaDiagnostics } from "../workflowSchema"
import { expressionDiagnostics } from "../workflowExpressions"

const examplesDir = join(__dirname, "../../../../internal/engine/workflow/examples")
const examples = readdirSync(examplesDir).filter(name => name.endsWith(".yaml")).map(name => [name, readFileSync(join(examplesDir, name), "utf8")] as const)

const commented = `# A workflow with comments everywhere.
schema_version: 1
name: commented
title: Commented # the title
inputs:
  task: {type: task, required: true}
  note:
    type: string # free text
on:
  schedule:
    cron: "0 9 * * *" # every morning
    enabled: false
steps:
  # The first step.
  - id: first
    title: First
    type: agent.run
    with:
      # What to ask.
      prompt: Look at \${{ workflow.inputs.note }} # inline
    outputs:
      summary: {type: string}

  # The second step reads the first.
  - id: second
    title: Second
    type: task.note
    with:
      task: \${{ workflow.inputs.task }}
      body: \${{ steps.first.outputs.summary }}
# The end.
`

/** clean is the editor's check of a file: the schema's and the references'. */
const clean = (text: string) => [...schemaDiagnostics(text), ...expressionDiagnostics(text)].filter(d => d.severity === "error")

// changedLines are the lines of after that before lacks, and of before that
// after lacks.
function changedLines(before: string, after: string): { removed: string[]; added: string[] } {
  const a = before.split("\n"), b = after.split("\n")
  return { removed: a.filter(line => !b.includes(line)), added: b.filter(line => !a.includes(line)) }
}

describe("round trips", () => {
  for (const [name, text] of [...examples, ["commented.yaml", commented] as const]) {
    it(`${name} is unchanged by a write of what it says`, () => {
      const parsed = parseWorkflow(text)
      expect(parsed.errors).toEqual([])
      expect(parsed.readOnly).toBeNull()
      expect(setField(text, ["title"], parsed.values.title).text).toBe(text)
      expect(setField(text, ["steps", 0, "id"], stepsOf(parsed.values)[0].id).text).toBe(text)
    })

    it(`${name}: a new title changes its line alone`, () => {
      const after = setField(text, ["title"], "A new title").text
      expect(parseWorkflow(after).values.title).toBe("A new title")
      const { removed, added } = changedLines(text, after)
      expect(removed.length).toBe(1)
      expect(added).toEqual([removed[0].replace(/title: .*?( #.*)?$/, "title: A new title$1")])
    })
  }
})

describe("setField", () => {
  it("keeps the inline comment of a value it changes", () => {
    const after = setField(commented, ["on", "schedule", "cron"], "0 10 * * *").text
    expect(after).toContain(`cron: 0 10 * * * # every morning`)
    expect(parseWorkflow(after).values.on?.schedule?.cron).toBe("0 10 * * *")
  })

  it("adds a key to a block map, after its last key", () => {
    const after = setField(commented, ["steps", 0, "with", "agent"], "codex").text
    expect(changedLines(commented, after)).toEqual({ removed: [], added: ["      agent: codex"] })
  })

  it("rewrites a flow map around a new key in block style", () => {
    const after = setField(commented, ["inputs", "task", "description"], "The task").text
    expect(after).toContain("  task:\n    type: task\n    required: true\n    description: The task\n")
    expect(after).toContain("    type: string # free text")
  })

  it("makes the maps on the way", () => {
    const after = setField(commented, ["steps", 1, "outputs", "note_id", "type"], "string").text
    expect(parseWorkflow(after).values.steps?.[1].outputs).toEqual({ note_id: { type: "string" } })
    expect(clean(after)).toEqual([])
  })

  it("writes multi-line text as a literal block", () => {
    const after = setField(commented, ["steps", 0, "with", "prompt"], "Line one\nLine two").text
    // Its inline comment moves to the key's line, out of the text.
    expect(after).toContain("      prompt: |- # inline\n        Line one\n        Line two\n")
    expect(after).toContain("      # What to ask.\n")
    expect(parseWorkflow(after).values.steps?.[0].with).toEqual({ prompt: "Line one\nLine two" })
  })

  it("removes a key with its line", () => {
    const after = setField(commented, ["on", "schedule", "enabled"], undefined).text
    expect(changedLines(commented, after)).toEqual({ removed: ["    enabled: false"], added: [] })
  })

  it("quotes what would not read back as the same string", () => {
    const after = setField(commented, ["title"], "yes: no").text
    expect(parseWorkflow(after).values.title).toBe("yes: no")
  })
})

describe("edge cases the review found", () => {
  const last = "schema_version: 1\nname: e\ntitle: E\nsteps:\n  - id: a\n    title: A\n    type: agent.run\n    with: {prompt: hi}"
  it("edits the last value of a file with no final newline", () => {
    expect(parseWorkflow(setField(last, ["steps", 0, "with", "prompt"], "changed").text).values.steps?.[0].with).toEqual({ prompt: "changed" })
    const block = "schema_version: 1\nname: e\ntitle: E\nsteps:\n  - id: a\n    title: A\n    type: agent.run\n    with:\n      prompt: |\n        old"
    expect(parseWorkflow(setField(block, ["steps", 0, "with", "prompt"], "changed").text).values.steps?.[0].with).toEqual({ prompt: "changed" })
    expect(parseWorkflow(setField(block, ["steps", 0, "with"], { macro: "m" }).text).values.steps?.[0].with).toEqual({ macro: "m" })
  })

  it("moves a step last before the fields after the steps", () => {
    const text = "schema_version: 1\nname: e\ntitle: E\nsteps:\n  - id: a\n    title: A\n    type: agent.run\n    with: {prompt: a}\n  - id: b\n    title: B\n    type: agent.run\n    with: {prompt: b}\ndescription: After the steps.\n"
    const after = moveStep(text, "a", 1).text
    const parsed = parseWorkflow(after)
    expect(parsed.errors).toEqual([])
    expect(stepsOf(parsed.values).map(s => s.id)).toEqual(["b", "a"])
    expect(parsed.values.description).toBe("After the steps.")
  })

  it("keeps the comments of a value it rewrites", () => {
    const flow = setField(commented.replace("  task: {type: task, required: true}", "  task: {type: task, required: true} # the task"), ["inputs", "task", "required"], false).text
    expect(flow).toContain("  task: # the task\n    type: task\n    required: false\n")
    const header = "schema_version: 1\nname: e\ntitle: E\nsteps:\n  - id: a\n    title: A\n    type: agent.run\n    with:\n      prompt: |- # rationale\n        old\n"
    expect(setField(header, ["steps", 0, "with", "prompt"], "new").text).toContain("      prompt: new # rationale\n")
    expect(setField(header, ["steps", 0, "with", "prompt"], "two\nlines").text).toContain("      prompt: |- # rationale\n        two\n        lines\n")
    const quoted = "schema_version: 1\nname: e\ntitle: E\ndescription: \"first\n  second\" # keep me\nsteps:\n  - {id: a, title: A, type: agent.run, with: {prompt: hi}}\n"
    const replaced = setField(quoted, ["description"], "new").text
    expect(replaced).toContain("description: new # keep me\n")
    expect(parseWorkflow(replaced).values.description).toBe("new")
    const flowLines = "schema_version: 1\nname: e\ntitle: E\ninputs:\n  pr: {type: integer,\n    required: true} # ticket\nsteps:\n  - {id: a, title: A, type: agent.run, with: {prompt: hi}}\n"
    expect(setField(flowLines, ["inputs", "pr", "required"], false).text).toContain("  pr: # ticket\n    type: integer\n    required: false\n")
    const keyLine = "schema_version: 1\nname: e\ntitle: E\nsteps:\n  - id: a\n    title: A\n    type: agent.run\n    with: # what to ask\n      prompt: x\n"
    expect(setField(keyLine, ["steps", 0, "with"], { macro: "m" }).text).toContain("    with: # what to ask\n      macro: m\n")
  })
})

describe("steps", () => {
  it("inserts a step in the builder's style, comments elsewhere kept", () => {
    const after = insertStep(commented, 1, { id: "middle", title: "Middle", type: "task.context", with: { task: "${{ workflow.inputs.task }}" } }).text
    expect(stepsOf(parseWorkflow(after).values).map(s => s.id)).toEqual(["first", "middle", "second"])
    expect(after).toContain("  - id: middle\n    title: Middle\n    type: task.context\n    with:\n      task: ${{ workflow.inputs.task }}\n")
    for (const comment of ["# The first step.", "# What to ask.", "# inline", "# The second step reads the first.", "# The end."]) expect(after).toContain(comment)
    expect(clean(after)).toEqual([])
  })

  it("moves a step with its own comment", () => {
    const after = moveStep(commented, "second", 0).text
    expect(stepsOf(parseWorkflow(after).values).map(s => s.id)).toEqual(["second", "first"])
    expect(after.indexOf("# The second step reads the first.")).toBeLessThan(after.indexOf("- id: second"))
    expect(after.indexOf("- id: second")).toBeLessThan(after.indexOf("# The first step."))
    expect(after.indexOf("# The first step.")).toBeLessThan(after.indexOf("- id: first"))
    // And back again.
    expect(stepsOf(parseWorkflow(moveStep(after, "second", 1).text).values).map(s => s.id)).toEqual(["first", "second"])
  })

  it("removes a step and its comment, keeping the others", () => {
    const after = removeStep(commented, "first").text
    expect(after).not.toContain("# The first step.")
    expect(after).not.toContain("# What to ask.")
    expect(after).toContain("# The second step reads the first.")
    expect(stepsOf(parseWorkflow(after).values).map(s => s.id)).toEqual(["second"])
    const last = removeStep(commented, "second").text
    expect(stepsOf(parseWorkflow(last).values).map(s => s.id)).toEqual(["first"])
    expect(last).toContain("# The end.")
  })

  it("renames a step and every reference to its outputs", () => {
    const after = renameStep(commented, "first", "look").text
    expect(after).toContain("- id: look")
    expect(after).toContain("body: ${{ steps.look.outputs.summary }}")
    expect(after).not.toContain("steps.first")
    expect(clean(after)).toEqual([])
    expect(() => renameStep(commented, "first", "second")).toThrow(WorkflowDocumentError)
  })

  it("edits steps written as a flow list", () => {
    const flow = "schema_version: 1\nname: f\ntitle: F\nsteps: [{id: a, title: A, type: agent.run, with: {prompt: hi}}]\n"
    const after = insertStep(flow, 1, { id: "b", title: "B", type: agent(), with: { prompt: "there" } }).text
    expect(stepsOf(parseWorkflow(after).values).map(s => s.id)).toEqual(["a", "b"])
    expect(clean(after)).toEqual([])
  })
})

function agent() { return "agent.run" }

describe("inputs and triggers", () => {
  it("renames an input and every reference to it", () => {
    const withTrigger = setField(commented, ["on", "schedule", "inputs"], { note: "${{ schedule.time }}" }).text
    const after = renameInput(withTrigger, "note", "topic").text
    expect(after).toContain("    type: string # free text")
    expect(after).toContain("prompt: Look at ${{ workflow.inputs.topic }} # inline")
    expect(parseWorkflow(after).values.on?.schedule?.inputs).toEqual({ topic: "${{ schedule.time }}" })
    expect(Object.keys(parseWorkflow(after).values.inputs ?? {})).toEqual(["task", "topic"])
    expect(clean(after)).toEqual([])
  })

  it("adds and removes inputs", () => {
    const added = addInput(commented, "repo", { type: "repository" }).text
    expect(parseWorkflow(added).values.inputs?.repo).toEqual({ type: "repository" })
    const removed = removeInput(added, "repo").text
    expect(removed).toBe(commented)
  })

  it("adds a trigger and removes one, on: going with the last", () => {
    const added = setTrigger(commented, "task.entered_column", { column: "Triage", enabled: false }).text
    expect(Object.keys(parseWorkflow(added).values.on ?? {})).toEqual(["schedule", "task.entered_column"])
    const one = setTrigger(added, "schedule", null).text
    expect(one).not.toContain("every morning")
    expect(Object.keys(parseWorkflow(one).values.on ?? {})).toEqual(["task.entered_column"])
    const none = setTrigger(one, "task.entered_column", null).text
    expect(parseWorkflow(none).values.on).toBeUndefined()
    expect(none).toContain("# The first step.")
  })

  it("gives an undo entry with the text before and after", () => {
    const { text, undo } = setTrigger(commented, "schedule", null)
    expect(undo).toEqual({ label: "Remove the schedule trigger", before: commented, after: text })
  })
})

describe("files the builder leaves alone", () => {
  it("refuses anchors, aliases and merge keys", () => {
    for (const text of [
      commented.replace("inputs:\n", "inputs: &inputs\n"),
      commented + "extra: *inputs\n",
      "schema_version: 1\nname: m\ntitle: M\nbase: &b {prompt: hi}\nsteps:\n  - id: a\n    title: A\n    type: agent.run\n    with:\n      <<: *b\n",
    ]) {
      const parsed = parseWorkflow(text)
      expect(parsed.readOnly).toBeTruthy()
      expect(() => setField(text, ["title"], "x")).toThrow(WorkflowDocumentError)
    }
  })

  it("refuses YAML that does not parse", () => {
    expect(parseWorkflow("name: [oops\n").errors.length).toBeGreaterThan(0)
    expect(() => setField("name: [oops\n", ["title"], "x")).toThrow(/does not parse/)
  })
})

// Properties over every example and the commented fixture: an operation
// changes the values it should and nothing else, and keeps every comment.
describe("properties", () => {
  const comments = (text: string) => text.split("\n").map(line => line.slice(line.indexOf("#"))).filter((part, i, all) => part.startsWith("#") && all.indexOf(part) === i).sort()
  const cases = [...examples, ["commented.yaml", commented] as const]
  for (const [name, text] of cases) {
    const values = parseWorkflow(text).values
    const steps = stepsOf(values)

    it(`${name}: setting any step's title changes that value alone`, () => {
      steps.forEach((_, i) => {
        const after = setField(text, ["steps", i, "title"], `Title ${i}`).text
        const expected = structuredClone(values)
        expected.steps![i].title = `Title ${i}`
        expect(parseWorkflow(after).values).toEqual(expected)
        expect(comments(after)).toEqual(comments(text))
      })
    })

    it(`${name}: every move keeps the steps and the comments`, () => {
      for (let from = 0; from < steps.length; from++) for (let to = 0; to < steps.length; to++) {
        const after = moveStep(text, steps[from].id!, to).text
        const order = steps.map(s => s.id)
        order.splice(to, 0, order.splice(from, 1)[0])
        const moved = stepsOf(parseWorkflow(after).values)
        expect(moved.map(s => s.id)).toEqual(order)
        expect(moved).toEqual(order.map(id => steps.find(s => s.id === id)))
        expect(comments(after)).toEqual(comments(text))
      }
    })

    it(`${name}: inserting then removing a step gives the file back`, () => {
      for (let at = 0; at <= steps.length; at++) {
        const inserted = insertStep(text, at, { id: "added", title: "Added", type: "task.context", with: { task: "${{ workflow.task }}" } }).text
        expect(stepsOf(parseWorkflow(inserted).values).map(s => s.id).indexOf("added")).toBe(at)
        expect(parseWorkflow(removeStep(inserted, "added").text).values).toEqual(values)
      }
    })
  }
})
