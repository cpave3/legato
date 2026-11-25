import { describe, expect, it } from "vitest"
import { completionsAt, contextAt, executors, schemaDiagnostics, describe as describeAt } from "../workflowSchema"

// at splits text at the | the test marks the cursor with.
function at(text: string): [string, number] {
  const offset = text.indexOf("|")
  return [text.slice(0, offset) + text.slice(offset + 1), offset]
}

const head = "schema_version: 1\nname: x\ntitle: X\n"

describe("contextAt", () => {
  const cases: [string, string, (string | number)[], "key" | "value"][] = [
    ["a top-level key", head + "|", [], "key"],
    ["a step's with", head + "steps:\n  - id: a\n    type: agent.run\n    with:\n      pr|", ["steps", 0, "with"], "key"],
    ["a second step", head + "steps:\n  - id: a\n    title: A\n  - id: b\n    ty|", ["steps", 1], "key"],
    ["a new item", head + "steps:\n  - id: a\n    title: A\n  - |", ["steps", 1], "key"],
    ["a list at the key's column", head + "steps:\n- id: a\n  type: agent.run\n- id: b\n  with:\n    |", ["steps", 1, "with"], "key"],
    ["a step's type", head + "steps:\n  - id: a\n    type: ag|", ["steps", 0, "type"], "value"],
    ["a trigger", head + "on:\n  schedule:\n    catch_up: |", ["on", "schedule", "catch_up"], "value"],
    ["an input", head + "inputs:\n  repo:\n    |", ["inputs", "repo"], "key"],
  ]
  for (const [name, marked, path, mode] of cases) {
    it(name, () => {
      const [text, offset] = at(marked)
      expect(contextAt(text, offset)).toMatchObject({ path, mode })
    })
  }
})

describe("completion", () => {
  it("offers every step type, the Learn ones included, with titles", () => {
    const [text, offset] = at(head + "steps:\n  - id: a\n    type: |\n")
    const labels = completionsAt(text, offset)!.options.map(o => o.label)
    expect(labels.sort()).toEqual(executors.map(e => e.type).sort())
    expect(labels).toContain("learning.packet.prepare")
    expect(completionsAt(text, offset)!.options.find(o => o.label === "agent.run")?.detail).toBe("Run an agent")
  })

  it("offers an agent.run step's with fields", () => {
    const [text, offset] = at(head + "steps:\n  - id: a\n    title: A\n    type: agent.run\n    with:\n      |\n")
    const labels = completionsAt(text, offset)!.options.map(o => o.label)
    for (const field of ["prompt", "macro", "agent", "tier", "keep_agent", "engram", "repo", "ref", "branch", "working_dir", "name"]) expect(labels).toContain(field)
  })

  it("leaves out keys the object has", () => {
    const [text, offset] = at(head + "steps:\n  - id: a\n    type: agent.run\n    with:\n      prompt: hi\n      |\n")
    expect(completionsAt(text, offset)!.options.map(o => o.label)).not.toContain("prompt")
  })

  it("offers the top-level keys, trigger kinds and enums", () => {
    let [text, offset] = at("schema_version: 1\n|")
    expect(completionsAt(text, offset)!.options.map(o => o.label)).toEqual(expect.arrayContaining(["name", "title", "steps", "on", "inputs", "run_title"]))
    ;[text, offset] = at(head + "on:\n  |")
    expect(completionsAt(text, offset)!.options.map(o => o.label).sort()).toEqual(["chat.button", "chat.mention", "chat.message", "pr.changes_requested", "schedule", "task.entered_column"])
    ;[text, offset] = at(head + "on:\n  schedule:\n    catch_up: |")
    expect(completionsAt(text, offset)!.options.map(o => o.label)).toEqual(["once", "skip"])
    ;[text, offset] = at(head + "steps:\n  - id: a\n    type: task.note\n    with:\n      optional: |")
    expect(completionsAt(text, offset)!.options.map(o => o.label)).toEqual(["true", "false"])
  })

  it("describes a key", () => {
    expect(describeAt(["steps", 0, "with", "keep_agent"], { steps: [{ type: "agent.run" }] })).toContain("Leave the agent running")
  })
})

describe("schemaDiagnostics", () => {
  const valid = head + "steps:\n  - id: a\n    title: A\n    type: agent.run\n    with: {prompt: hi}\n"
  const lineOf = (text: string, from: number) => text.slice(0, from).split("\n").length

  it("passes a valid file", () => {
    expect(schemaDiagnostics(valid)).toEqual([])
  })

  it("puts a bad step type on its line", () => {
    const text = valid.replace("agent.run", "agent.nope")
    const [d] = schemaDiagnostics(text)
    expect(d).toMatchObject({ severity: "error", path: "steps[0].type" })
    expect(lineOf(text, d.from)).toBe(7)
  })

  it("warns of an unknown with key, and refuses one elsewhere", () => {
    const text = valid.replace("{prompt: hi}", "{prompt: hi, promt: hi}") + "colour: blue\n"
    const diagnostics = schemaDiagnostics(text)
    expect(diagnostics.map(d => [d.severity, d.message.split(":")[0]])).toEqual([
      ["warning", "agent.run takes no with field promt"],
      ["error", "a workflow has no field colour"],
    ])
  })

  it("checks types, required keys and exclusive fields", () => {
    expect(schemaDiagnostics(valid.replace("{prompt: hi}", "{prompt: hi, macro: m}"))[0].message).toContain("not both")
    expect(schemaDiagnostics(valid.replace("{prompt: hi}", "{}"))[0].message).toContain("needs prompt or macro")
    expect(schemaDiagnostics(valid.replace("{prompt: hi}", "{prompt: hi, keep_agent: yes}"))[0].message).toContain("must be a boolean")
    expect(schemaDiagnostics(valid.replace("title: X\n", ""))[0].message).toBe("a workflow needs title")
  })

  it("needs a step's with when its type does", () => {
    expect(schemaDiagnostics(valid.replace("    with: {prompt: hi}\n", ""))[0].message).toBe("steps[0] needs with")
    expect(schemaDiagnostics(valid.replace("schema_version: 1\n", ""))[0].message).toBe("a workflow needs schema_version")
  })

  it("accepts an expression where a number goes", () => {
    const text = head + "steps:\n  - id: a\n    title: A\n    type: github.pr.review_passes\n    with: {repo: a/b, pr_number: \"${{ workflow.inputs.n }}\"}\n"
    expect(schemaDiagnostics(text)).toEqual([])
  })

  it("reports a syntax error", () => {
    const [d] = schemaDiagnostics("name: [oops\n")
    expect(d.severity).toBe("error")
  })
})

// A file is read against its schema_version's schema: version 2's unless
// it says 1 (LEG-484).
describe("schema versions", () => {
  const v2 = "schema_version: 2\nname: x\ntitle: X\nsteps:\n  - id: a\n    type: agent.run\n    start: manual\n    wait_for: [complete]\n    with: {prompt: hi, keep: true}\n  - id: each\n    loop: {repeat: true, max: 3}\n    steps:\n      - id: b\n        type: task.note\n        if: {field: steps.a.outputs.x, exists: true}\n        with: {task: x, body: hi}\n"

  it("passes a v2 file with start, wait_for, if and a loop", () => {
    expect(schemaDiagnostics(v2)).toEqual([])
  })

  it("refuses each version's keys in the other", () => {
    expect(schemaDiagnostics(v2.replace("start: manual", "trigger: manual")).map(d => d.message)).toContain("steps[0] has no field trigger")
    expect(schemaDiagnostics(head + "steps:\n  - id: a\n    type: agent.run\n    start: manual\n    with: {prompt: hi}\n").map(d => d.message)).toContain("steps[0] has no field start")
  })

  it("says a v2 step needs a type or a loop, not both", () => {
    expect(schemaDiagnostics(v2.replace("    type: agent.run\n", "")).map(d => d.message)).toContain("steps[0] needs type or loop")
    expect(schemaDiagnostics(v2.replace("    loop: {repeat: true, max: 3}\n", "    type: task.note\n    loop: {repeat: true, max: 3}\n")).map(d => d.message)).toContain("steps[1] takes loop or type, not both")
  })

  it("completes each version's step keys", () => {
    let [text, offset] = at("schema_version: 2\nname: x\ntitle: X\nsteps:\n  - id: a\n    type: agent.run\n    |\n")
    expect(completionsAt(text, offset)!.options.map(o => o.label)).toEqual(expect.arrayContaining(["start", "wait_for", "if", "on_failure", "loop", "timeout"]))
    ;[text, offset] = at(head + "steps:\n  - id: a\n    type: agent.run\n    |\n")
    const v1 = completionsAt(text, offset)!.options.map(o => o.label)
    expect(v1).toContain("trigger")
    expect(v1).not.toContain("start")
  })
})

describe("the embedded examples", async () => {
  const { readdirSync, readFileSync } = await import("node:fs")
  const { join } = await import("node:path")
  const dir = join(__dirname, "../../../../internal/engine/workflow/examples")
  for (const file of readdirSync(dir).filter(name => name.endsWith(".yaml"))) {
    it(`${file} has no schema problems`, () => {
      expect(schemaDiagnostics(readFileSync(join(dir, file), "utf8"))).toEqual([])
    })
  }
})
