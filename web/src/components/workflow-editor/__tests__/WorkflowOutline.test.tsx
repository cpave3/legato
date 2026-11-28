import { useState } from "react"
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { afterEach, expect, it, vi } from "vitest"
import WorkflowOutline, { type WorkflowOutlineProps } from "../WorkflowOutline"
import type { Selection } from "../../../lib/workflowAddress"
import type { WorkflowEdit } from "../../../lib/workflowDocument"

// The outline on its own, without the page: the parts the page tests do
// not reach (the add menus, the per-kind inspectors, the empty and broken
// files).

vi.mock("../LazyWorkflowYamlEditor", () => ({
  LazyWorkflowYamlEditor: ({ value, onChange, label = "Workflow YAML" }: { value: string; onChange: (v: string) => void; label?: string }) =>
    <textarea aria-label={label} value={value} onChange={event => onChange(event.target.value)} />,
}))
vi.mock("../../workflow-graph/LazyWorkflowGraph", () => ({ LazyWorkflowGraph: () => <p>the graph</p> }))

afterEach(cleanup)

const twoSteps = `schema_version: 1
name: demo
title: Demo
inputs:
  pr: {type: integer}
on:
  schedule:
    every: weekdays
    at: "09:00"
    enabled: false
steps:
  - id: prep
    title: Prepare
    type: repo.ensure
    with:
      repo: a/b
  - id: run
    title: Run
    type: agent.run
    with:
      prompt: Review \${{ steps.prep.outputs.path }}
`

type Harness = Partial<WorkflowOutlineProps> & { initial: string; start?: Selection | null }

function setup({ initial, start = null, ...rest }: Harness) {
  const edits: WorkflowEdit[] = []
  const onEditYaml = vi.fn()
  const selected: (Selection | null)[] = []
  function Host() {
    const [text, setText] = useState(initial)
    const [selection, setSelection] = useState<Selection | null>(start)
    return <WorkflowOutline text={text} onEdit={edit => { edits.push(edit); setText(edit.text) }} onEditYaml={onEditYaml}
      selection={selection} onSelect={next => { selected.push(next); setSelection(next) }} {...rest} />
  }
  render(<Host />)
  return { edits, onEditYaml, selected, last: () => edits[edits.length - 1]?.text ?? initial }
}

it("says a file that doesn't parse can't be shown, and offers its YAML", () => {
  const { onEditYaml } = setup({ initial: "name: [unclosed\n" })
  const alert = screen.getByRole("alert")
  expect(alert.textContent).toMatch(/^The YAML doesn't parse, so it can't be shown here: /)
  fireEvent.click(within(alert).getByRole("button", { name: "Edit as YAML" }))
  expect(onEditYaml).toHaveBeenCalledWith()
})

it("says a file that is not a workflow's mapping can't be shown as forms", () => {
  setup({ initial: "- a\n- b\n" })
  expect(screen.getByRole("alert").textContent).toContain("This file can't be shown as forms: the file is not a mapping of a workflow's fields.")
})

it("on the canvas, keeps the page's notices and cards beside the alert", () => {
  setup({ initial: "- a\n", layout: "canvas", notices: <p>a notice</p>, cards: <p>a card</p> })
  expect(screen.getByRole("alert")).toBeTruthy()
  expect(screen.getByText("a notice")).toBeTruthy()
  expect(screen.getByText("a card")).toBeTruthy()
})

it("an empty workflow says it takes no inputs and offers its first step", () => {
  setup({ initial: "schema_version: 1\nname: empty\n" })
  expect(screen.getByText("Nothing: it takes no inputs")).toBeTruthy()
  expect(screen.getByText("You can always run it by hand.")).toBeTruthy()
  expect(screen.queryByRole("button", { name: "Add step" })).toBeNull()
  fireEvent.click(screen.getByRole("button", { name: "Add the first step" }))
  expect(screen.queryByRole("button", { name: "Add the first step" })).toBeNull()
  expect(screen.getByText("Choose a row of the outline to edit it.")).toBeTruthy()
})

it("beside the YAML, an empty workflow says it needs a step and offers no adding", () => {
  setup({ initial: "schema_version: 1\nname: empty\n", mode: "navigate" })
  expect(screen.getByText("No steps yet: a workflow needs at least one.")).toBeTruthy()
  expect(screen.queryByRole("button", { name: "Add input" })).toBeNull()
  expect(screen.queryByRole("button", { name: "Add trigger" })).toBeNull()
})

it("adds a trigger from its menu, and closes the menu with Cancel or Escape", () => {
  const { last, selected } = setup({ initial: twoSteps })
  fireEvent.click(screen.getByRole("button", { name: "Add trigger" }))
  let menu = screen.getByRole("dialog", { name: "Add a trigger" })
  expect(within(menu).queryByText("Schedule")).toBeNull()
  fireEvent.click(within(menu).getByRole("button", { name: "Cancel" }))
  expect(screen.queryByRole("dialog", { name: "Add a trigger" })).toBeNull()
  fireEvent.click(screen.getByRole("button", { name: "Add trigger" }))
  fireEvent.keyDown(screen.getByRole("dialog", { name: "Add a trigger" }), { key: "Escape" })
  expect(screen.queryByRole("dialog", { name: "Add a trigger" })).toBeNull()
  fireEvent.click(screen.getByRole("button", { name: "Add trigger" }))
  menu = screen.getByRole("dialog", { name: "Add a trigger" })
  fireEvent.click(within(menu).getAllByRole("button")[0])
  expect(last()).toMatch(/on:\n {2}schedule:[\s\S]*\n {2}[a-z.]+:/)
  expect(selected[selected.length - 1]?.kind).toBe("trigger")
})

it("adds inputs under names not taken, and opens each", () => {
  const { last, selected } = setup({ initial: twoSteps.replace("pr: {type: integer}", "input: {type: string}") })
  fireEvent.click(screen.getByRole("button", { name: "Add input" }))
  expect(last()).toContain("input-2:")
  expect(selected[selected.length - 1]).toEqual({ kind: "input", name: "input-2" })
})

it("marks a paused trigger, or one on, as this Legato's switch has it", () => {
  setup({ initial: twoSteps })
  const triggers = screen.getByRole("region", { name: "Triggers" })
  expect(within(triggers).getByText("paused")).toBeTruthy()
  cleanup()
  setup({ initial: twoSteps, pausedHere: { schedule: false } })
  expect(within(screen.getByRole("region", { name: "Triggers" })).getByText("on")).toBeTruthy()
})

it("counts a row's errors on it", () => {
  const at = (line: number) => ({ severity: "error" as const, message: "bad", line, column: 5, end_line: line, end_column: 6 })
  setup({ initial: twoSteps, diagnostics: [at(13), at(14), { ...at(19), severity: "warning" }] })
  expect(screen.getByLabelText("2 problems")).toBeTruthy()
  cleanup()
  setup({ initial: twoSteps, diagnostics: [at(13)] })
  expect(screen.getByLabelText("1 problem")).toBeTruthy()
})

it("shows About with the workflow's title and its form alone", () => {
  setup({ initial: twoSteps, start: { kind: "about" } })
  const inspector = screen.getByRole("region", { name: "Inspector" })
  expect(within(inspector).getByText("About this workflow")).toBeTruthy()
  expect(within(inspector).getByRole("heading", { name: "Demo" })).toBeTruthy()
  expect(within(inspector).queryByRole("tablist")).toBeNull()
  cleanup()
  setup({ initial: "schema_version: 1\nname: x\n", start: { kind: "about" } })
  expect(within(screen.getByRole("region", { name: "Inspector" })).getByRole("heading", { name: "Untitled" })).toBeTruthy()
})

it("shows an input under its name, with Setup and YAML", () => {
  setup({ initial: twoSteps, start: { kind: "input", name: "pr" } })
  const inspector = screen.getByRole("region", { name: "Inspector" })
  expect(within(inspector).getByText("Input")).toBeTruthy()
  expect(within(inspector).getByRole("heading", { name: "pr" })).toBeTruthy()
  expect(within(inspector).getAllByRole("tab").map(t => t.textContent)).toEqual(["Setup", "YAML"])
  fireEvent.click(within(inspector).getByRole("tab", { name: "YAML" }))
  expect((within(inspector).getByLabelText("YAML of inputs/pr") as HTMLTextAreaElement).value).toContain("pr:")
})

it("shows a trigger under its kind's title", () => {
  setup({ initial: twoSteps, start: { kind: "trigger", name: "schedule" } })
  const inspector = screen.getByRole("region", { name: "Inspector" })
  expect(within(inspector).getByText("Trigger")).toBeTruthy()
  expect(within(inspector).getAllByRole("tab").map(t => t.textContent)).toEqual(["Setup", "YAML"])
})

it("offers a column trigger a task input from its card, until it has one", () => {
  const column = twoSteps.replace(/on:\n[\s\S]*?steps:/, "on:\n  task.entered_column:\n    column: Doing\nsteps:")
  const { last } = setup({ initial: column, start: { kind: "trigger", name: "task.entered_column" } })
  fireEvent.click(screen.getByRole("button", { name: "Add a task input from the card" }))
  expect(last()).toContain("task: ${{ event.task }}")
  expect(screen.queryByRole("button", { name: "Add a task input from the card" })).toBeNull()
  expect(within(screen.getByRole("region", { name: "Inputs" })).getByText("from the card")).toBeTruthy()
})

it("moves a step from the inspector, but not past a step it reads", () => {
  const { last } = setup({ initial: twoSteps, start: { kind: "step", path: "run" } })
  const inspector = screen.getByRole("region", { name: "Inspector" })
  expect(within(inspector).getByText("Run an agent")).toBeTruthy()
  expect(within(inspector).getByRole("button", { name: "Move step run down" }).hasAttribute("disabled")).toBe(true)
  fireEvent.click(within(inspector).getByRole("button", { name: "Move step run up" }))
  expect(screen.getByRole("alert").textContent).toContain("That move would break a reference")
  expect(last()).toBe(twoSteps)
})

it("asks before removing a step another reads, from the inspector", () => {
  const { last, selected } = setup({ initial: twoSteps, start: { kind: "step", path: "prep" } })
  const inspector = screen.getByRole("region", { name: "Inspector" })
  fireEvent.click(within(inspector).getByRole("button", { name: "Remove step prep" }))
  const ask = within(inspector).getByRole("alertdialog", { name: "Remove step prep" })
  expect(ask.textContent).toContain("run reads this step's outputs.")
  fireEvent.click(within(ask).getByRole("button", { name: "Keep it" }))
  fireEvent.click(within(inspector).getByRole("button", { name: "Remove step prep" }))
  fireEvent.click(within(inspector).getByRole("button", { name: "Remove anyway" }))
  expect(last()).not.toContain("id: prep")
  expect(selected[selected.length - 1]).toBeNull()
})

it("warns in the list when a step's menu would move it past a step it reads", () => {
  const { last } = setup({ initial: twoSteps })
  fireEvent.click(screen.getByRole("button", { name: "Actions for step run" }))
  fireEvent.click(within(screen.getByRole("menu", { name: "Step run" })).getByRole("menuitem", { name: "Move up" }))
  expect(screen.getByRole("alert").textContent).toMatch(/^That move would break a reference: .*prep/)
  expect(last()).toBe(twoSteps)
})

it("says a step inside another is edited as YAML", () => {
  const nested = `schema_version: 2
name: n
steps:
  - id: each
    title: Each
    type: loop
    over: \${{ workflow.inputs.items }}
    steps:
      - id: inner
        title: Inner
        type: task.note
        with:
          note: hi
`
  setup({ initial: nested, start: { kind: "step", path: "each/inner" } })
  const inspector = screen.getByRole("region", { name: "Inspector" })
  expect(within(inspector).getByText("A step inside another is edited as YAML for now.")).toBeTruthy()
  expect(within(inspector).getByRole("heading", { name: "Inner" })).toBeTruthy()
})

it("gives a row the forms can't show its YAML alone, and marks it in the outline", () => {
  const aliased = twoSteps.replace("with:\n      repo: a/b", "with: &w\n      repo: a/b")
  setup({ initial: aliased, start: { kind: "step", path: "prep" } })
  expect(screen.getByText("Edited as YAML")).toBeTruthy()
  const inspector = screen.getByRole("region", { name: "Inspector" })
  expect(within(inspector).getByText(/This can't be shown as a form: it uses a YAML anchor/)).toBeTruthy()
  expect(within(inspector).queryByRole("tablist")).toBeNull()
  expect(within(inspector).getByLabelText("YAML of steps/prep")).toBeTruthy()
})

it("moves a step by dragging its row onto another's place", () => {
  const three = twoSteps + "  - id: note\n    title: Note\n    type: task.note\n    with:\n      note: hi\n"
  const { last } = setup({ initial: three })
  const steps = screen.getByRole("list", { name: "Steps" })
  const rowOf = (name: RegExp) => within(steps).getByRole("button", { name }).closest("li")!
  fireEvent.dragStart(rowOf(/^3\s*Note/))
  fireEvent.drop(rowOf(/^1\s*Prepare/))
  expect(last().indexOf("id: note")).toBeLessThan(last().indexOf("id: prep"))
  fireEvent.dragStart(rowOf(/^2\s*Prepare/))
  fireEvent.drop(rowOf(/^3\s*Run/))
  expect(screen.getByRole("alert").textContent).toMatch(/^That move would break a reference/)
})

it("offers Graph only once there is a graph", () => {
  setup({ initial: twoSteps })
  const graph = screen.getByRole("radio", { name: "Graph" })
  expect(graph.hasAttribute("disabled")).toBe(true)
  expect(graph.getAttribute("title")).toBe("The graph shows once Legato has read the file")
  expect(screen.getByRole("radio", { name: "List" }).getAttribute("aria-checked")).toBe("true")
})

it("beside the YAML, says the YAML can't be read and dims the last outline", () => {
  const shown = setup({ initial: twoSteps + "  - id: [\n", mode: "navigate", lastGood: twoSteps })
  const note = screen.getByText(/can't read the YAML/).closest("p")!
  expect(note.textContent).toMatch(/^Line \d+: can't read the YAML/)
  fireEvent.click(within(note).getByRole("button", { name: "Show" }))
  expect(shown.onEditYaml).toHaveBeenCalledWith(expect.any(Number))
  expect(screen.getByRole("navigation", { name: "Outline" }).closest("[inert]")).toBeTruthy()
})

it("while the YAML can't be read, edits nothing and keeps the outline it last had", () => {
  setup({ initial: twoSteps + "  - id: [\n", lastGood: twoSteps })
  expect(screen.getByText(/can't read the YAML/)).toBeTruthy()
  expect(screen.getByRole("button", { name: /Prepare/ })).toBeTruthy()
  expect(screen.queryByRole("button", { name: "Add input" })).toBeNull()
  expect(screen.getByRole("navigation", { name: "Outline" }).closest("[inert]")).toBeTruthy()
})
