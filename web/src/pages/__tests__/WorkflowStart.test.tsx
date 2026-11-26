import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { MemoryRouter, Route, Routes } from "react-router-dom"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { WorkflowEditPage } from "../WorkflowEditPage"
import { WorkflowsPage } from "../WorkflowsPage"

// The start page of a new workflow (LEG-514): every way into the editor.

const { apiFetch, discovery } = vi.hoisted(() => ({ apiFetch: vi.fn(), discovery: { definitions: [] as { name: string; title: string }[] } }))
vi.mock("../../lib/api", () => ({ apiFetch }))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../hooks/useMediaQuery", () => ({ useMediaQuery: () => false }))
vi.mock("../../hooks/useWorkflow", () => ({
  useWorkflows: () => ({ data: { definitions: discovery.definitions, errors: null, folders: [] }, refresh: vi.fn(), loading: false, error: null }),
  useWorkflowRuns: () => ({ data: [], refresh: vi.fn(), loading: false, error: null }),
}))
vi.mock("../../components/workflow-editor/LazyWorkflowYamlEditor", () => ({
  LazyWorkflowYamlEditor: ({ value, onChange, label = "Workflow YAML" }: { value: string; onChange: (v: string) => void; label?: string }) =>
    <textarea aria-label={label} value={value} onChange={event => onChange(event.target.value)} />,
}))
vi.mock("../../components/workflow-graph/LazyWorkflowGraph", () => ({ LazyWorkflowGraph: () => null }))

const triage = `schema_version: 1
name: triage
title: Triage new cards
description: Sort a card when it reaches Review.
inputs:
  task: {type: task, required: true}
on:
  task.entered_column:
    column: Review
    inputs:
      task: \${{ event.task }}
steps:
  - id: look
    title: Look at it
    type: agent.run
    with:
      prompt: Sort \${{ workflow.inputs.task.title }}
  - id: move
    title: Move it
    type: task.move
    with:
      task: \${{ workflow.inputs.task }}
      column: Backlog
`
const mine = "# my own\nschema_version: 1\nname: review\ntitle: Review\nsteps:\n  - {id: a, title: A, type: task.note, with: {body: hi}}\n"
const json = (body: unknown, status = 200) => Promise.resolve(new Response(JSON.stringify(body), { status }))

beforeEach(() => {
  localStorage.clear()
  localStorage.setItem("legato:workflow-editor-mode", "yaml")
  discovery.definitions = []
  apiFetch.mockReset()
  apiFetch.mockImplementation((_: string, path: string) => {
    if (path === "/api/workflow-templates") return json({ templates: [{ name: "triage", title: "Triage new cards", description: "Sort a card.", yaml: triage }], folders: [] })
    if (path === "/api/workflow-files/check") return json({ definition: null, graph: null, diagnostics: [] })
    if (path === "/api/workflows/review/file") return json({ name: "review", yaml: mine, path: "/g/review.yaml", folder: "global", digest: "sha256:x", editable: true })
    return json({}, 404)
  })
})
afterEach(cleanup)

function renderNew() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(<QueryClientProvider client={client}><MemoryRouter initialEntries={["/workflows/new"]}>
    <Routes><Route path="/workflows/new" element={<WorkflowEditPage mode="new" />} /></Routes>
  </MemoryRouter></QueryClientProvider>)
}

const editorText = async () => (await screen.findByRole("textbox", { name: "Workflow YAML" }) as HTMLTextAreaElement).value

it("offers templates with what starts them, what they ask for and do, Blank and Paste YAML", async () => {
  renderNew()
  const templates = await screen.findByRole("region", { name: "Templates" })
  const card = await within(templates).findByRole("button", { name: /Triage new cards/ })
  expect(card.textContent).toContain("When A card enters Review")
  expect(card.textContent).toContain("Asks for task")
  expect(card.textContent).toContain("Does Look at it → Move it")
  const ways = screen.getByRole("region", { name: "Start from" })
  expect(within(ways).getByRole("button", { name: /^Blank/ })).toBeTruthy()
  expect(within(ways).getByRole("button", { name: /^Paste YAML/ })).toBeTruthy()
})

it("shows a template's outline, then opens the editor on it, unsaved", async () => {
  renderNew()
  fireEvent.click(await screen.findByRole("button", { name: /Triage new cards/ }))
  const preview = screen.getByRole("region", { name: "Template Triage new cards" })
  expect(within(preview).getByRole("navigation", { name: "Outline" }).textContent).toContain("Look at it")
  fireEvent.click(within(preview).getByRole("button", { name: "Use this template" }))
  expect(await editorText()).toBe(triage)
  expect(screen.getByRole("button", { name: "Create" })).toBeTruthy()
})

it("gives a template a free name when one of yours has its name", async () => {
  discovery.definitions = [{ name: "triage", title: "Mine" }]
  renderNew()
  fireEvent.click(await screen.findByRole("button", { name: /Triage new cards/ }))
  fireEvent.click(screen.getByRole("button", { name: "Use this template" }))
  expect(await editorText()).toContain("name: triage-2\n")
})

it("sums up pasted YAML as you paste, and continues with it", async () => {
  renderNew()
  fireEvent.click(await screen.findByRole("button", { name: /^Paste YAML/ }))
  const box = screen.getByRole("textbox", { name: "Pasted YAML" })
  const continueButton = screen.getByRole("button", { name: "Continue" })
  expect(continueButton).toHaveProperty("disabled", true)
  fireEvent.change(box, { target: { value: triage } })
  expect(screen.getByRole("status", { name: "What it is" }).textContent).toBe("Starts when a card enters Review · asks for 1 input · 2 steps")
  fireEvent.click(continueButton)
  expect(await editorText()).toBe(triage)
})

it("copies one of yours under a new name", async () => {
  discovery.definitions = [{ name: "review", title: "Review" }]
  renderNew()
  fireEvent.click(await within(await screen.findByRole("region", { name: "Copy one of yours" })).findByRole("button", { name: /Review/ }))
  expect(await editorText()).toBe(mine.replace("name: review", "name: review-copy").replace("title: Review", "title: Review (copy)"))
})

it("opens Blank with no steps and an inviting first step", async () => {
  localStorage.setItem("legato:workflow-editor-mode", "visual")
  renderNew()
  fireEvent.click(await screen.findByRole("button", { name: /^Blank/ }))
  const then = await screen.findByRole("region", { name: "Steps" })
  fireEvent.click(within(then).getByRole("button", { name: "Add the first step" }))
  expect(screen.getByRole("dialog", { name: "Add a step" })).toBeTruthy()
})

it("is the empty Workflows tab, and what is chosen there opens the editor", async () => {
  localStorage.setItem("legato:workflows-tab", "workflows")
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(<QueryClientProvider client={client}><MemoryRouter initialEntries={["/workflows?tab=workflows"]}>
    <Routes>
      <Route path="/workflows" element={<WorkflowsPage />} />
      <Route path="/workflows/new" element={<WorkflowEditPage mode="new" />} />
    </Routes>
  </MemoryRouter></QueryClientProvider>)
  const start = await screen.findByRole("region", { name: "Start a workflow" })
  fireEvent.click(await within(start).findByRole("button", { name: /Triage new cards/ }))
  fireEvent.click(within(start).getByRole("button", { name: "Use this template" }))
  await waitFor(async () => expect(await editorText()).toBe(triage))
})

// Review fixes (LEG-514).
it("keeps a restored new workflow unsaved, its draft kept", async () => {
  localStorage.setItem("legato:workflow-draft::new", JSON.stringify({ text: triage, digest: "" }))
  renderNew()
  fireEvent.click(within(await screen.findByRole("alertdialog", { name: "Unsaved changes" })).getByRole("button", { name: "Restore them" }))
  expect(await editorText()).toBe(triage)
  await waitFor(() => expect(JSON.parse(localStorage.getItem("legato:workflow-draft::new") ?? "null")?.text).toBe(triage))
  const unload = () => { const e = new Event("beforeunload", { cancelable: true }); window.dispatchEvent(e); return e.defaultPrevented }
  expect(unload()).toBe(true)
})

it("sums up pasted YAML with a malformed trigger without failing", async () => {
  renderNew()
  fireEvent.click(await screen.findByRole("button", { name: /^Paste YAML/ }))
  fireEvent.change(screen.getByRole("textbox", { name: "Pasted YAML" }), { target: { value: "schema_version: 1\nname: x\ntitle: X\non:\n  task.entered_column:\n    column: Review\n    conditions:\n      all: oops\nsteps: []\n" } })
  expect(screen.getByRole("status", { name: "What it is" }).textContent).toMatch(/card enters Review/)
  expect(screen.getByRole("textbox", { name: "Pasted YAML" })).toBeTruthy()
})
