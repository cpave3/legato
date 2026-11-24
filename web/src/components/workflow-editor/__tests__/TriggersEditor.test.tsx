import { readFileSync } from "node:fs"
import { join } from "node:path"
import { useState } from "react"
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { parse } from "yaml"
import { afterEach, expect, it } from "vitest"
import WorkflowBuilder from "../WorkflowBuilder"
import type { SchedulePreview, WorkflowDiagnostic } from "../../../lib/workflowFiles"
import { schemaDiagnostics } from "../../../lib/workflowSchema"
import { expressionDiagnostics } from "../../../lib/workflowExpressions"

afterEach(cleanup)

let latest = ""
function Builder({ initial, diagnostics, schedule }: { initial: string; diagnostics?: WorkflowDiagnostic[]; schedule?: SchedulePreview }) {
  const [text, setText] = useState(initial)
  return <WorkflowBuilder text={text} onEdit={edit => { latest = edit.text; setText(edit.text) }} onEditYaml={() => {}} diagnostics={diagnostics} schedule={schedule}
    options={{ macros: [], agents: [], tiers: {}, columns: ["Backlog", "Triage", "Ready"], bots: ["slack", "discord"] }} />
}
const renderBuilder = (initial: string, extra: { diagnostics?: WorkflowDiagnostic[]; schedule?: SchedulePreview } = {}) => { latest = initial; return render(<Builder initial={initial} {...extra} />) }
const commit = (element: HTMLElement, value: string) => { fireEvent.change(element, { target: { value } }); fireEvent.blur(element) }
const errors = (text: string) => [...schemaDiagnostics(text), ...expressionDiagnostics(text)].filter(d => d.severity === "error").map(d => d.message)

const template = readFileSync(join(__dirname, "../../../../../internal/engine/workflow/examples/triage.workflow.yaml"), "utf8")

it("builds the triage template's trigger", () => {
  const start = template.replace(/^on:\n(?: {2}.*\n)+/m, "")
  expect(parse(start).on).toBeUndefined()
  renderBuilder(start)
  fireEvent.click(screen.getByRole("button", { name: "Add trigger" }))
  fireEvent.click(within(screen.getByRole("group", { name: "Trigger kinds" })).getByRole("button", { name: /When a card enters a column/ }))
  const card = screen.getByRole("article", { name: "When a card enters a column" })
  commit(within(card).getByRole("combobox", { name: "Column of the task.entered_column trigger" }), "Triage")
  expect((within(card).getByRole("checkbox", { name: "The task.entered_column trigger starts runs by itself" }) as HTMLInputElement).checked).toBe(false)
  fireEvent.click(within(card).getByRole("button", { name: "Insert a reference into input task from the task.entered_column trigger" }))
  fireEvent.click(screen.getByRole("option", { name: "event.task" }))
  commit(within(card).getByRole("textbox", { name: "Input repository from the task.entered_column trigger" }), "${{ task.metadata.repository }}")
  fireEvent.click(within(card).getByRole("button", { name: "Add a condition" }))
  commit(screen.getByRole("combobox", { name: "Conditions, 1: field" }), "task.metadata.triaged")
  fireEvent.change(screen.getByRole("combobox", { name: "Conditions, 1: compare" }), { target: { value: "not_equals" } })
  fireEvent.change(screen.getByRole("combobox", { name: "Conditions, 1: value type" }), { target: { value: "boolean" } })
  commit(screen.getByRole("textbox", { name: "Conditions, 1: value" }), "true")
  expect(parse(latest).on).toEqual(parse(template).on)
  expect(parse(latest)).toEqual(parse(template))
  expect(errors(latest)).toEqual([])
})

it("shows a bad cron's server diagnostic on its field", () => {
  const text = "schema_version: 1\nname: s\ntitle: S\non:\n  schedule:\n    cron: \"61 * * * *\"\nsteps:\n  - {id: a, title: A, type: agent.run, with: {prompt: hi}}\n"
  renderBuilder(text, { diagnostics: [{ severity: "error", message: "schedule: minute 61 is out of range", path: "on.schedule.cron", line: 6, column: 11, end_line: 6, end_column: 23 }] })
  const cron = screen.getByRole("textbox", { name: "Cron" })
  expect(cron.getAttribute("aria-invalid")).toBe("true")
  expect(within(screen.getByRole("article", { name: "On a schedule" })).getByText("schedule: minute 61 is out of range")).toBeTruthy()
})

it("removes a trigger with its comments, and nothing else", () => {
  const text = "# Top.\nschema_version: 1\nname: s\ntitle: S\non:\n  # Every morning.\n  schedule:\n    cron: \"0 9 * * *\" # nine\n  # On changes.\n  pr.changes_requested: {enabled: false}\nsteps:\n  # The step.\n  - {id: a, title: A, type: agent.run, with: {prompt: hi}}\n"
  renderBuilder(text)
  fireEvent.click(screen.getByRole("button", { name: "Remove the schedule trigger" }))
  expect(latest).toBe("# Top.\nschema_version: 1\nname: s\ntitle: S\non:\n  # On changes.\n  pr.changes_requested: {enabled: false}\nsteps:\n  # The step.\n  - {id: a, title: A, type: agent.run, with: {prompt: hi}}\n")
})

it("switches a schedule between every and cron in one edit, and shows the next runs", () => {
  const text = "schema_version: 1\nname: s\ntitle: S\non:\n  schedule: {every: weekdays, at: \"09:00\", enabled: false}\nsteps:\n  - {id: a, title: A, type: agent.run, with: {prompt: hi}}\n"
  renderBuilder(text, { schedule: { description: "Every weekday at 09:00", timezone: "UTC", next: ["2026-11-23T09:00:00Z", "2026-11-24T09:00:00Z", "2026-11-25T09:00:00Z"] } })
  expect(screen.getByLabelText("Next runs").textContent).toContain("Every weekday at 09:00")
  fireEvent.click(screen.getByRole("radio", { name: "Cron" }))
  expect(parse(latest).on.schedule).toEqual({ cron: "0 9 * * 1-5", enabled: false })
  fireEvent.click(screen.getByRole("radio", { name: "Every" }))
  expect(parse(latest).on.schedule).toEqual({ every: "weekdays", at: "09:00", enabled: false })
})

it("edits groups of conditions", () => {
  const text = "schema_version: 1\nname: s\ntitle: S\non:\n  task.entered_column:\n    column: Doing\n    conditions: {all: [{field: task.priority, equals: high}]}\nsteps:\n  - {id: a, title: A, type: agent.run, with: {prompt: hi}}\n"
  renderBuilder(text)
  const group = screen.getByRole("group", { name: "Conditions" })
  fireEvent.click(within(group).getByRole("button", { name: "Not" }))
  commit(screen.getByRole("combobox", { name: "Conditions, 2, not: field" }), "task.has_active_agent")
  fireEvent.change(screen.getByRole("combobox", { name: "Conditions, 2, not: compare" }), { target: { value: "equals" } })
  fireEvent.change(screen.getByRole("combobox", { name: "Conditions, 2, not: value type" }), { target: { value: "boolean" } })
  commit(screen.getByRole("textbox", { name: "Conditions, 2, not: value" }), "true")
  fireEvent.change(screen.getByRole("combobox", { name: "Conditions: all or any" }), { target: { value: "any" } })
  expect(parse(latest).on["task.entered_column"].conditions).toEqual({ any: [{ field: "task.priority", equals: "high" }, { not: { field: "task.has_active_agent", equals: true } }] })
  expect(errors(latest)).toEqual([])
})

it("takes on out when a weekly schedule becomes daily", () => {
  const text = "schema_version: 1\nname: s\ntitle: S\non:\n  schedule: {every: weekly, at: \"09:00\", on: monday}\nsteps:\n  - {id: a, title: A, type: agent.run, with: {prompt: hi}}\n"
  renderBuilder(text)
  fireEvent.change(screen.getByRole("combobox", { name: "Every" }), { target: { value: "daily" } })
  expect(parse(latest).on.schedule).toEqual({ every: "daily", at: "09:00" })
  expect(errors(latest)).toEqual([])
})

it("keeps a condition value's type when it is edited", () => {
  const text = "schema_version: 1\nname: s\ntitle: S\non:\n  task.entered_column:\n    column: Doing\n    conditions: {all: [{field: task.metadata.code, equals: \"123\"}]}\nsteps:\n  - {id: a, title: A, type: agent.run, with: {prompt: hi}}\n"
  renderBuilder(text)
  expect((screen.getByRole("combobox", { name: "Conditions, 1: value type" }) as HTMLSelectElement).value).toBe("text")
  commit(screen.getByRole("textbox", { name: "Conditions, 1: value" }), "124")
  expect(parse(latest).on["task.entered_column"].conditions.all[0].equals).toBe("124")
  fireEvent.change(screen.getByRole("combobox", { name: "Conditions, 1: value type" }), { target: { value: "number" } })
  expect(parse(latest).on["task.entered_column"].conditions.all[0].equals).toBe(124)
})

it("sends conditions it can't draw to YAML", () => {
  renderBuilder("schema_version: 1\nname: s\ntitle: S\non:\n  task.entered_column:\n    column: Doing\n    conditions: {all: [3]}\nsteps:\n  - {id: a, title: A, type: agent.run, with: {prompt: hi}}\n")
  expect(screen.getByRole("alert").textContent).toContain("conditions: a condition is not a mapping")
})

it("keeps each value of an in list its own type", () => {
  const text = "schema_version: 1\nname: s\ntitle: S\non:\n  task.entered_column:\n    column: Doing\n    conditions: {all: [{field: task.metadata.flag, in: [foo, true, \"a, b\"]}]}\nsteps:\n  - {id: a, title: A, type: agent.run, with: {prompt: hi}}\n"
  renderBuilder(text)
  commit(screen.getByRole("textbox", { name: "Conditions, 1: value 1" }), "bar")
  expect(parse(latest).on["task.entered_column"].conditions.all[0].in).toEqual(["bar", true, "a, b"])
  fireEvent.change(screen.getByRole("textbox", { name: "Conditions, 1: new value" }), { target: { value: "3" } })
  fireEvent.change(screen.getByRole("combobox", { name: "Conditions, 1: new value type" }), { target: { value: "number" } })
  fireEvent.click(screen.getByRole("button", { name: "Add a value to Conditions, 1" }))
  expect(parse(latest).on["task.entered_column"].conditions.all[0].in).toEqual(["bar", true, "a, b", 3])
})

it("adds a chat trigger paused, on the first bot", () => {
  renderBuilder("schema_version: 1\nname: s\ntitle: S\nsteps:\n  - {id: a, title: A, type: agent.run, with: {prompt: hi}}\n")
  fireEvent.click(screen.getByRole("button", { name: "Add trigger" }))
  fireEvent.click(within(screen.getByRole("group", { name: "Trigger kinds" })).getByRole("button", { name: /When the bot is mentioned/ }))
  expect(parse(latest).on).toEqual({ "chat.mention": { bot: "slack", enabled: false } })
  const card = screen.getByRole("article", { name: "When the bot is mentioned" })
  expect((within(card).getByRole("combobox", { name: "Bot of the chat.mention trigger" }) as HTMLInputElement).value).toBe("slack")
  expect(errors(latest)).toEqual([])
})

it("edits a chat message trigger's bot, channel, user, text match, loop item and bots", () => {
  const text = "schema_version: 1\nname: s\ntitle: S\non:\n  chat.message:\n    bot: slack\n    channel: C123\n    text: {contains: deploy}\nsteps:\n  - {id: a, title: A, type: agent.run, with: {prompt: hi}}\n"
  renderBuilder(text)
  const card = within(screen.getByRole("article", { name: "When a chat message arrives" }))
  const bot = card.getByRole("combobox", { name: "Bot of the chat.message trigger" }) as HTMLInputElement
  expect(bot.value).toBe("slack")
  expect([...document.querySelectorAll(`#${bot.getAttribute("list")} option`)].map(o => o.getAttribute("value"))).toEqual(["slack", "discord"])
  expect((card.getByRole("textbox", { name: "Channel of the chat.message trigger" }) as HTMLInputElement).value).toBe("C123")
  expect((card.getByRole("combobox", { name: "Text match of the chat.message trigger" }) as HTMLSelectElement).value).toBe("contains")
  expect((card.getByRole("textbox", { name: "Text of the chat.message trigger" }) as HTMLInputElement).value).toBe("deploy")

  commit(bot, "discord")
  commit(card.getByRole("textbox", { name: "Channel of the chat.message trigger" }), "")
  commit(card.getByRole("textbox", { name: "User of the chat.message trigger" }), "U42")
  fireEvent.change(card.getByRole("combobox", { name: "Text match of the chat.message trigger" }), { target: { value: "regex" } })
  expect(parse(latest).on["chat.message"].text).toEqual({ regex: "deploy" })
  commit(card.getByRole("textbox", { name: "Text of the chat.message trigger" }), "^deploy (\\w+)$")
  fireEvent.change(card.getByRole("combobox", { name: "Loop stop of the chat.message trigger counts runs by" }), { target: { value: "thread" } })
  fireEvent.click(card.getByRole("checkbox", { name: "The chat.message trigger also runs on other bots' messages" }))
  expect(parse(latest).on["chat.message"]).toEqual({ bot: "discord", user: "U42", text: { regex: "^deploy (\\w+)$" }, loop_item: "thread", include_bots: true })
  expect(errors(latest)).toEqual([])

  fireEvent.change(card.getByRole("combobox", { name: "Text match of the chat.message trigger" }), { target: { value: "" } })
  expect(parse(latest).on["chat.message"].text).toBeUndefined()
  expect(card.queryByRole("textbox", { name: "Text of the chat.message trigger" })).toBeNull()
})
