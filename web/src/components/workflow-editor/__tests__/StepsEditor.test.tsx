import { readFileSync } from "node:fs"
import { join } from "node:path"
import { useState } from "react"
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { parse } from "yaml"
import { afterEach, describe, expect, it } from "vitest"
import WorkflowBuilder from "../WorkflowBuilder"
import { FIELD_KINDS, brokenReferences, newStep, stepForm } from "../stepForms"
import { executors, schemaDiagnostics } from "../../../lib/workflowSchema"
import { expressionDiagnostics } from "../../../lib/workflowExpressions"

afterEach(cleanup)

let latest = ""
function Builder({ initial }: { initial: string }) {
  const [text, setText] = useState(initial)
  return <WorkflowBuilder text={text} onEdit={edit => { latest = edit.text; setText(edit.text) }} onEditYaml={() => {}} options={{ macros: ["Review", "Plan"], agents: ["claude-code", "codex"], tiers: { codex: ["fast"] }, columns: ["Backlog", "Doing"], bots: ["slack", "discord"] }} />
}

// renderBuilder draws the builder over its own text; latest is that text.
const renderBuilder = (initial: string) => { latest = initial; return render(<Builder initial={initial} />) }

const head = "schema_version: 1\nname: build\ntitle: Build\n"
const errors = (text: string) => [...schemaDiagnostics(text), ...expressionDiagnostics(text)].filter(d => d.severity === "error").map(d => d.message)
const commit = (element: HTMLElement, value: string) => { fireEvent.change(element, { target: { value } }); fireEvent.blur(element) }
const stepCard = (n: number) => screen.getAllByRole("listitem").filter(li => li.getAttribute("aria-label")?.startsWith(`Step ${n}:`))[0]

describe("the step types", () => {
  const types = executors

  it("have forms of kinds the builder draws", () => {
    for (const executor of executors) {
      const form = stepForm(executor.type)!
      for (const field of form.fields) expect(FIELD_KINDS, `${executor.type}.${field.name}`).toContain(field.kind)
    }
  })

  for (const executor of types) {
    it(`${executor.type} gets a form of every with field`, () => {
      const step = newStep(executor.type, [])
      renderBuilder(head + `steps:\n  - id: ${step.id}\n    title: T\n    type: ${executor.type}\n`)
      const form = stepForm(executor.type)!
      const card = stepCard(1)
      const exclusive = form.groups.filter(g => g.kind === "exactly_one").flatMap(g => g.fields.slice(1))
      for (const field of form.fields.filter(f => !exclusive.includes(f.name))) {
        expect(within(card).getByLabelText(`${field.name} of step ${step.id}`), `${executor.type}.${field.name}`).toBeTruthy()
      }
      cleanup()
    })
  }
})

describe("the catalog's metadata", () => {
  it("orders a form's fields, with plain labels and the essential ones marked", () => {
    const form = stepForm("agent.run")!
    expect(form.fields.slice(0, 4).map(f => f.name)).toEqual(["prompt", "macro", "working_dir", "repo"])
    expect(form.fields.map(f => f.order)).toEqual([...form.fields.map(f => f.order)].sort((a, b) => a - b))
    expect(form.fields[0]).toMatchObject({ label: "What it should do", essential: true })
    expect(form.fields.find(f => f.name === "name")).toMatchObject({ essential: false })
    expect(form.summary).toEqual(["macro", "prompt"])
  })

  it("gives every offered step type a summary and an essential field", () => {
    for (const executor of executors) {
      const form = stepForm(executor.type)!
      expect(form.summary.length, executor.type).toBeGreaterThan(0)
      expect(form.fields.some(f => f.essential), executor.type).toBe(true)
    }
  })

  it("draws the fields in that order, under their labels", () => {
    renderBuilder(head + "steps:\n  - id: s\n    title: S\n    type: task.move\n    with:\n      column: Doing\n      task: ${{ workflow.task }}\n")
    const card = stepCard(1)
    const task = within(card).getByLabelText("task of step s")
    const column = within(card).getByLabelText("column of step s")
    expect(task.compareDocumentPosition(column) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(within(card).getByText(/^To column/)).toBeTruthy()
  })
})

describe("a chat message step", () => {
  it("asks for the bot, the channel, the message and what it replies to, in that order", () => {
    expect(stepForm("chat.message.send")!.fields.map(f => [f.name, f.kind])).toEqual([["bot", "chat_bot"], ["channel", "string"], ["text", "text"], ["thread_id", "string"]])
    expect(FIELD_KINDS).toContain("chat_bot")
  })

  it("offers the bots and writes the step", () => {
    const step = newStep("chat.message.send", [])
    expect(step.with).toEqual({ bot: "", text: "" })
    renderBuilder(head + "steps:\n  - id: say\n    title: Say\n    type: chat.message.send\n    with: {bot: \"\", text: \"\"}\n")
    const card = stepCard(1)
    const labels = ["bot", "channel", "text", "thread_id"].map(name => within(card).getByLabelText(`${name} of step say`))
    for (let i = 1; i < labels.length; i++) expect(labels[i - 1].compareDocumentPosition(labels[i]) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    const bot = labels[0]
    expect([...document.querySelectorAll(`#${bot.getAttribute("list")} option`)].map(o => o.getAttribute("value"))).toEqual(["slack", "discord"])
    commit(bot, "discord")
    commit(labels[1], "C42")
    commit(labels[2], "Deployed.")
    commit(labels[3], "${{ event.thread_id }}")
    expect(parse(latest).steps[0].with).toEqual({ bot: "discord", channel: "C42", text: "Deployed.", thread_id: "${{ event.thread_id }}" })
  })
})

describe("fields by kind", () => {
  const base = head + "inputs:\n  target: {type: task, required: true}\nsteps:\n  - id: s\n    title: S\n    type: agent.run\n    with:\n      prompt: hi\n"
  it("writes text, booleans, numbers, task references and picks", () => {
    renderBuilder(base)
    commit(screen.getByLabelText("prompt of step s"), "Line one\nLine two")
    fireEvent.click(screen.getByLabelText("keep_agent of step s"))
    commit(screen.getByLabelText("pr_number of step s"), "12")
    commit(screen.getByLabelText("agent of step s"), "codex")
    commit(screen.getByLabelText("tier of step s"), "fast")
    const values = parse(latest).steps[0].with
    expect(values).toEqual({ prompt: "Line one\nLine two", keep_agent: true, pr_number: 12, agent: "codex", tier: "fast" })
    expect(screen.getByLabelText("agent of step s").getAttribute("list")).toBeTruthy()
    expect(errors(latest)).toEqual([])
  })

  it("chooses between prompt and macro", () => {
    renderBuilder(base)
    fireEvent.click(screen.getByRole("radio", { name: "macro" }))
    commit(screen.getByLabelText("macro of step s"), "Review")
    expect(parse(latest).steps[0].with).toEqual({ macro: "Review" })
  })

  it("offers whole-task references and enums as choices", () => {
    renderBuilder(head + "inputs:\n  target: {type: task, required: true}\nsteps:\n  - id: s\n    title: S\n    type: task.move\n    with:\n      task: ${{ workflow.task }}\n      column: Doing\n  - id: r\n    title: R\n    type: github.pr.review_passes\n    with: {repo: a/b, pr_number: 1}\n")
    fireEvent.change(screen.getByLabelText("task of step s"), { target: { value: "${{ workflow.inputs.target }}" } })
    fireEvent.change(screen.getByLabelText("scope of step r"), { target: { value: "pr" } })
    const doc = parse(latest)
    expect(doc.steps[0].with.task).toBe("${{ workflow.inputs.target }}")
    expect(doc.steps[1].with.scope).toBe("pr")
  })

  it("inserts a reference with the picker", () => {
    renderBuilder(base)
    fireEvent.click(screen.getByRole("button", { name: "Insert a reference into name of step s" }))
    fireEvent.click(screen.getByRole("option", { name: "workflow.inputs.target.title" }))
    expect(parse(latest).steps[0].with.name).toBe("${{ workflow.inputs.target.title }}")
  })

  it("closes the reference picker when a reference is picked", () => {
    renderBuilder(base)
    fireEvent.click(screen.getByRole("button", { name: "Insert a reference into name of step s" }))
    fireEvent.click(screen.getByRole("option", { name: "workflow.task" }))
    expect(screen.queryByRole("listbox")).toBeNull()
  })

  it("says when a number field holds something else", () => {
    renderBuilder(base)
    commit(screen.getByLabelText("pr_number of step s"), "abc")
    expect(screen.getByLabelText("pr_number of step s").getAttribute("aria-invalid")).toBe("true")
    expect(screen.getByText("pr_number must be a whole number or a ${{ }} expression")).toBeTruthy()
  })

  it("keeps fields the schema doesn't know, and names them", () => {
    renderBuilder(base.replace("prompt: hi\n", "prompt: hi\n      promt: hello\n"))
    expect(screen.getByText(/Other fields \(edit in YAML\)/).textContent).toContain("with.promt")
  })
})

describe("steps", () => {
  const commented = head + "steps:\n  # Look first.\n  - id: look\n    title: Look\n    type: agent.run\n    with: {prompt: look}\n    outputs:\n      summary: {type: string}\n  # Then note it.\n  - id: note\n    title: Note\n    type: task.note\n    with:\n      task: ${{ workflow.task }}\n      body: ${{ steps.look.outputs.summary }}\n"

  it("refuses a move that would break a reference, naming it", () => {
    renderBuilder(commented)
    fireEvent.click(screen.getByRole("button", { name: "Move step note up" }))
    expect(screen.getByRole("alert").textContent).toContain("step note reads steps.look.outputs.summary")
    expect(latest).toBe(commented)
    expect(brokenReferences(parse(commented).steps, ["note", "look"])).toEqual(["step note reads steps.look.outputs.summary"])
  })

  it("moves a step with its comment", () => {
    const free = commented.replace("${{ steps.look.outputs.summary }}", "done")
    renderBuilder(free)
    fireEvent.click(screen.getByRole("button", { name: "Move step note up" }))
    expect(latest.indexOf("# Then note it.")).toBeLessThan(latest.indexOf("- id: note"))
    expect(latest.indexOf("- id: note")).toBeLessThan(latest.indexOf("# Look first."))
  })

  it("warns before removing a step others read", () => {
    renderBuilder(commented)
    fireEvent.click(screen.getByRole("button", { name: "Remove step look" }))
    const ask = screen.getByRole("alertdialog", { name: "Remove step look" })
    expect(ask.textContent).toContain("note reads this step's outputs")
    expect(latest).toBe(commented)
    fireEvent.click(within(ask).getByRole("button", { name: "Remove anyway" }))
    expect(parse(latest).steps.map((s: { id: string }) => s.id)).toEqual(["note"])
  })

  it("adds, moves and removes steps from the keyboard", async () => {
    const user = userEvent.setup()
    renderBuilder(head + "steps:\n  - id: first\n    title: First\n    type: agent.run\n    with: {prompt: hi}\n")
    screen.getByRole("button", { name: "Add step" }).focus()
    await user.keyboard("{Enter}")
    within(screen.getByRole("group", { name: "Step types" })).getByRole("button", { name: /Add a note to a task/ }).focus()
    await user.keyboard("{Enter}")
    expect(parse(latest).steps.map((s: { id: string }) => s.id)).toEqual(["first", "task-note"])
    screen.getByRole("button", { name: "Move step task-note up" }).focus()
    await user.keyboard("{Enter}")
    expect(parse(latest).steps.map((s: { id: string }) => s.id)).toEqual(["task-note", "first"])
    screen.getByRole("button", { name: "Remove step task-note" }).focus()
    await user.keyboard("{Enter}")
    expect(parse(latest).steps.map((s: { id: string }) => s.id)).toEqual(["first"])
  })

  it("sees references to an ID with a slash in it", () => {
    const slash = commented.replaceAll("look", "read/pr")
    renderBuilder(slash)
    fireEvent.click(screen.getByRole("button", { name: "Move step note up" }))
    expect(screen.getByRole("alert").textContent).toContain("step note reads steps.read/pr.outputs.summary")
    fireEvent.click(screen.getByRole("button", { name: "Remove step read/pr" }))
    expect(screen.getByRole("alertdialog", { name: "Remove step read/pr" })).toBeTruthy()
  })

  it("edits outputs", () => {
    renderBuilder(head + "steps:\n  - id: s\n    title: S\n    type: agent.run\n    with: {prompt: hi}\n")
    fireEvent.change(screen.getByLabelText("New output of step s"), { target: { value: "report" } })
    fireEvent.click(screen.getByRole("button", { name: "Add output" }))
    fireEvent.change(screen.getByLabelText("Type of output report"), { target: { value: "file" } })
    commit(screen.getByLabelText("Path of output report"), "report.md")
    expect(parse(latest).steps[0].outputs).toEqual({ report: { type: "file", path: "report.md" } })
    expect(errors(latest)).toEqual([])
  })
})

it("builds the review-pr template from an empty workflow", () => {
  const template = readFileSync(join(__dirname, "../../../../../internal/engine/workflow/examples/review-pr.workflow.yaml"), "utf8")
  renderBuilder("schema_version: 1\nname: review-pr\ntitle: Review pull request\nsteps: []\n")
  commit(screen.getByRole("textbox", { name: "Run title" }), "Review ${{ workflow.inputs.repo }}#${{ workflow.inputs.pr_number }}")
  commit(screen.getByRole("textbox", { name: "Description" }), parse(template).description)
  for (const [name, type, description] of [["repo", "string", "GitHub owner/repository"], ["pr_number", "integer", "Pull request number"]]) {
    fireEvent.change(screen.getByRole("textbox", { name: "New input name" }), { target: { value: name } })
    fireEvent.click(screen.getByRole("button", { name: "Add input" }))
    fireEvent.change(screen.getByLabelText(`Type of input ${name}`), { target: { value: type } })
    fireEvent.click(screen.getByLabelText(`Input ${name} is required`))
    commit(screen.getByLabelText(`Description of input ${name}`), description)
  }
  const addStep = (title: RegExp) => {
    fireEvent.click(screen.getByRole("button", { name: "Add step" }))
    fireEvent.click(within(screen.getByRole("group", { name: "Step types" })).getByRole("button", { name: title }))
  }
  addStep(/Run an agent/)
  commit(screen.getByLabelText("ID of step agent-run"), "review")
  commit(screen.getByLabelText("Title of step review"), "Review pull request")
  fireEvent.click(screen.getByRole("radio", { name: "macro" }))
  commit(screen.getByLabelText("macro of step review"), "Review")
  commit(screen.getByLabelText("name of step review"), "Review ${{ workflow.inputs.repo }}#${{ workflow.inputs.pr_number }}")
  commit(screen.getByLabelText("repo of step review"), "${{ workflow.inputs.repo }}")
  commit(screen.getByLabelText("pr_number of step review"), "${{ workflow.inputs.pr_number }}")
  fireEvent.click(screen.getByLabelText("keep_agent of step review"))
  addStep(/Stop the task's agent/)
  commit(screen.getByLabelText("ID of step agent-stop"), "end")
  commit(screen.getByLabelText("Title of step end"), "End the review")
  fireEvent.change(screen.getByLabelText("How step end starts"), { target: { value: "agent_ended" } })
  commit(screen.getByLabelText("task of step end"), "${{ workflow.task }}")
  expect(errors(latest)).toEqual([])
  expect(parse(latest)).toEqual(parse(template))
})
