import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { MemoryRouter, Route, Routes } from "react-router-dom"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { parse } from "yaml"
import { WorkflowEditPage } from "../WorkflowEditPage"

// Triggers in When (LEG-509): sentence rows with their state, the trigger
// picker, the task input from the card, and Saved · paused · Turn on.

const { apiFetch, discovery } = vi.hoisted(() => ({ apiFetch: vi.fn(), discovery: { states: [] as { trigger: string; enabled: boolean; file_enabled: boolean; overridden: boolean }[] } }))
vi.mock("../../lib/api", () => ({ apiFetch }))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../hooks/useMediaQuery", () => ({ useMediaQuery: () => false }))
vi.mock("../../hooks/useWorkflow", () => ({ useWorkflows: () => ({ data: { definitions: [], errors: null, folders: [], trigger_states: { "triage": discovery.states } }, refresh: vi.fn(async () => {}) }) }))
vi.mock("../../components/workflow-editor/LazyWorkflowYamlEditor", () => ({
  LazyWorkflowYamlEditor: ({ value, onChange, label = "Workflow YAML" }: { value: string; onChange: (v: string) => void; label?: string }) =>
    <textarea aria-label={label} value={value} onChange={event => onChange(event.target.value)} />,
}))
vi.mock("../../components/workflow-graph/LazyWorkflowGraph", () => ({ LazyWorkflowGraph: () => null }))

const fileText = `schema_version: 1
name: triage
title: Triage
on:
  task.entered_column:
    column: Review
    enabled: false
    conditions:
      all:
        - field: task.priority
          equals: high
  schedule:
    every: weekdays
    at: "09:00"
  pr.changes_requested:
    enabled: false
steps:
  - id: look
    title: Look
    type: agent.run
    with:
      prompt: Look
`
const json = (body: unknown, status = 200) => Promise.resolve(new Response(JSON.stringify(body), { status }))
let puts: { path: string; body: unknown }[] = []
// served is the file the server gives; a test may serve another.
let served = fileText

beforeEach(() => {
  localStorage.clear()
  localStorage.setItem("legato:workflow-editor-mode", "visual")
  discovery.states = [
    { trigger: "task.entered_column", enabled: false, file_enabled: false, overridden: false },
    { trigger: "schedule", enabled: true, file_enabled: true, overridden: false },
    { trigger: "pr.changes_requested", enabled: false, file_enabled: false, overridden: false },
  ]
  puts = []
  served = fileText
  apiFetch.mockReset()
  apiFetch.mockImplementation((_: string, path: string, init?: RequestInit) => {
    const body = init?.body ? JSON.parse(String(init.body)) : {}
    if (init?.method === "PUT") { puts.push({ path, body }); return json(path.includes("/triggers/") ? { status: "ok" } : { name: "triage", path: "/g/triage.yaml", folder: "global", digest: "sha256:new", diagnostics: [] }) }
    if (path === "/api/workflow-files/check") return json({ definition: { name: "triage" }, graph: null, diagnostics: [] })
    if (path === "/api/workflows/triage/file") return json({ name: "triage", yaml: served, path: "/g/triage.yaml", folder: "global", digest: "sha256:old", editable: true })
    return json({}, 404)
  })
})
afterEach(cleanup)

async function renderAt(hash = "") {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(<QueryClientProvider client={client}><MemoryRouter initialEntries={[`/workflows/triage/edit${hash}`]}>
    <Routes><Route path="/workflows/:name/edit" element={<WorkflowEditPage mode="edit" />} /></Routes>
  </MemoryRouter></QueryClientProvider>)
  return screen.findByRole("region", { name: "Triggers" })
}

const fileNow = async () => {
  fireEvent.click(screen.getByRole("radio", { name: "YAML" }))
  const value = (await screen.findByRole("textbox", { name: "Workflow YAML" }) as HTMLTextAreaElement).value
  fireEvent.click(screen.getByRole("radio", { name: "Visual" }))
  return value
}

it("writes each trigger as a sentence, with its state here", async () => {
  const when = await renderAt()
  const rows = [...when.querySelectorAll("[data-address]")].map(row => row.textContent)
  expect(rows).toEqual([
    "A card enters Review · priority is highpaused",
    "Every weekday at 09:00on",
    "A PR gets changes requestedpaused",
  ])
  expect(within(when).getByText("Review").tagName).toBe("STRONG")
  expect(within(when).getByText("You can always run it by hand.")).toBeTruthy()
})

it("offers no trigger kind once every kind is used", async () => {
  discovery.states = []
  served = fileText.replace("steps:", "  chat.message:\n    bot: slack\n    enabled: false\n  chat.mention:\n    bot: discord\n    enabled: false\n  chat.button:\n    bot: slack\n    user: U1\n    enabled: false\nsteps:")
  const when = await renderAt()
  expect(within(when).queryByRole("button", { name: "Add trigger" })).toBeNull()
})

it("offers a column trigger's workflow a task input filled from the card", async () => {
  await renderAt("#on/task.entered_column")
  fireEvent.click(await screen.findByRole("button", { name: "Add a task input from the card" }))
  const file = parse(await fileNow())
  expect(file.inputs.task).toEqual({ type: "task", required: true })
  expect(file.on["task.entered_column"].inputs.task).toBe("${{ event.task }}")
  const asks = screen.getByRole("region", { name: "Inputs" })
  expect(within(asks).getByRole("button", { name: /task.*from the card/ })).toBeTruthy()
  expect(screen.queryByRole("button", { name: "Add a task input from the card" })).toBeNull()
})

it("after a save, says a trigger is paused and turns it on when asked", async () => {
  await renderAt("#about")
  fireEvent.change(await screen.findByRole("textbox", { name: "Description" }), { target: { value: "Sorts cards" } })
  fireEvent.blur(screen.getByRole("textbox", { name: "Description" }))
  const save = screen.getByRole("button", { name: "Save" })
  await waitFor(() => expect(save).toHaveProperty("disabled", false))
  fireEvent.click(save)
  const saved = await screen.findByRole("status", { name: "Saved" })
  expect(saved.textContent).toContain("Saved · paused")
  expect(saved.textContent).toContain("When a card enters a column, When changes are requested on a pull request")
  fireEvent.click(within(saved).getByRole("button", { name: "Turn on" }))
  // It asks first: the trigger will start runs by itself.
  const ask = within(saved).getByRole("alertdialog", { name: "Turn on" })
  expect(puts.filter(p => p.path.includes("/triggers/"))).toEqual([])
  fireEvent.click(within(ask).getByRole("button", { name: "Turn on" }))
  await waitFor(() => expect(puts.filter(p => p.path.includes("/triggers/"))).toEqual([
    { path: "/api/workflows/triage/triggers/task.entered_column", body: { enabled: true } },
    { path: "/api/workflows/triage/triggers/pr.changes_requested", body: { enabled: true } },
  ]))
  expect((await screen.findByRole("status", { name: "Saved" })).textContent).toContain("Saved · on")
})

it("says Saved · paused in the editor a new workflow with a paused trigger lands in", async () => {
  apiFetch.mockImplementation((_: string, path: string, init?: RequestInit) => {
    if (path === "/api/workflow-files" && init?.method === "POST") return json({ name: "triage", path: "/g/triage.yaml", folder: "global", digest: "sha256:x", diagnostics: [] }, 201)
    if (path === "/api/workflow-files/check") return json({ definition: { name: "triage" }, graph: null, diagnostics: [] })
    if (path === "/api/workflows/triage/file") return json({ name: "triage", yaml: fileText, path: "/g/triage.yaml", folder: "global", digest: "sha256:x", editable: true })
    return json({}, 404)
  })
  localStorage.setItem("legato:workflow-editor-mode", "yaml")
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(<QueryClientProvider client={client}><MemoryRouter initialEntries={["/workflows/new"]}>
    <Routes>
      <Route path="/workflows/new" element={<WorkflowEditPage mode="new" />} />
      <Route path="/workflows/:name/edit" element={<WorkflowEditPage mode="edit" />} />
    </Routes>
  </MemoryRouter></QueryClientProvider>)
  // A new workflow starts on the start page: Blank opens the editor.
  fireEvent.click(await screen.findByRole("button", { name: /^Blank/ }))
  const editor = await screen.findByRole("textbox", { name: "Workflow YAML" })
  fireEvent.change(editor, { target: { value: fileText } })
  const create = screen.getByRole("button", { name: "Create" })
  await waitFor(() => expect(create).toHaveProperty("disabled", false))
  fireEvent.click(create)
  expect((await screen.findByRole("status", { name: "Saved" })).textContent).toContain("Saved · paused")
})
