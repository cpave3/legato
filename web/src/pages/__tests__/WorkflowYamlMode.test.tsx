import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { MemoryRouter, Route, Routes } from "react-router-dom"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { WorkflowEditPage } from "../WorkflowEditPage"

// YAML mode's outline (LEG-512): a navigator synced with the editor's
// cursor both ways, a jump menu on a phone, and paste on the forms.

const { apiFetch, media } = vi.hoisted(() => ({ apiFetch: vi.fn(), media: { narrow: false } }))
vi.mock("../../lib/api", () => ({ apiFetch }))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../hooks/useMediaQuery", () => ({ useMediaQuery: () => media.narrow }))
vi.mock("../../hooks/useWorkflow", () => ({ useWorkflows: () => ({ data: { definitions: [], errors: null, folders: [] }, refresh: vi.fn() }) }))
// The editor is a text box that shows where it was sent and says where its
// cursor is, as CodeMirror does through jumpTo and onCursor.
vi.mock("../../components/workflow-editor/LazyWorkflowYamlEditor", () => ({
  LazyWorkflowYamlEditor: ({ value, onChange, label = "Workflow YAML", jumpTo, onCursor }: { value: string; onChange: (v: string) => void; label?: string; jumpTo?: { from: number; to: number }; onCursor?: (offset: number) => void }) =>
    <textarea aria-label={label} value={value} data-jump={jumpTo ? `${jumpTo.from}-${jumpTo.to}` : ""} onChange={event => onChange(event.target.value)}
      onSelect={event => onCursor?.((event.target as HTMLTextAreaElement).selectionStart)} />,
}))
vi.mock("../../components/workflow-graph/LazyWorkflowGraph", () => ({ LazyWorkflowGraph: () => null }))

const fileText = `schema_version: 1
name: flow
title: Flow
inputs:
  pr: {type: integer}
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
      prompt: Look
`
const json = (body: unknown, status = 200) => Promise.resolve(new Response(JSON.stringify(body), { status }))

beforeEach(() => {
  localStorage.clear()
  media.narrow = false
  apiFetch.mockReset()
  apiFetch.mockImplementation((_: string, path: string) => {
    if (path === "/api/workflow-files/check") return json({ definition: { name: "flow" }, graph: null, diagnostics: [] })
    if (path === "/api/workflows/flow/file") return json({ name: "flow", yaml: fileText, path: "/g/flow.yaml", folder: "global", digest: "sha256:old", editable: true })
    return json({}, 404)
  })
})
afterEach(cleanup)

function renderAt(mode: "yaml" | "visual", hash = "") {
  localStorage.setItem("legato:workflow-editor-mode", mode)
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(<QueryClientProvider client={client}><MemoryRouter initialEntries={[`/workflows/flow/edit${hash}`]}>
    <Routes><Route path="/workflows/:name/edit" element={<WorkflowEditPage mode="edit" />} /></Routes>
  </MemoryRouter></QueryClientProvider>)
}

const editor = () => screen.getByRole("textbox", { name: "Workflow YAML" }) as HTMLTextAreaElement
const navigator = () => screen.getByRole("region", { name: "Navigator" })

it("moves the editor's cursor to a row's text when the row is chosen", async () => {
  renderAt("yaml")
  const run = await within(await screen.findByRole("region", { name: "Navigator" })).findByRole("button", { name: /Run/ })
  fireEvent.click(run)
  const from = fileText.indexOf("  - id: run") + 2
  const to = fileText.indexOf("prompt: Look") + "prompt: Look".length
  await waitFor(() => expect(editor().getAttribute("data-jump")).toBe(`${from}-${to}`))
  // No add buttons or menus beside the YAML: it is a navigator.
  expect(within(navigator()).queryByRole("button", { name: "Add step" })).toBeNull()
  expect(within(navigator()).queryByRole("button", { name: /Actions for step/ })).toBeNull()
})

it("marks the row the editor's cursor is in", async () => {
  renderAt("yaml")
  await within(await screen.findByRole("region", { name: "Navigator" })).findByRole("button", { name: /Prepare/ })
  const at = fileText.indexOf("repo: a/b")
  editor().setSelectionRange(at, at)
  fireEvent.select(editor())
  await waitFor(() => expect(within(navigator()).getByRole("button", { name: /Prepare/ }).getAttribute("aria-current")).toBe("true"))
  const input = fileText.indexOf("pr: {type")
  editor().setSelectionRange(input, input)
  fireEvent.select(editor())
  await waitFor(() => expect(within(navigator()).getByRole("button", { name: /^pr/ }).getAttribute("aria-current")).toBe("true"))
  expect(within(navigator()).getByRole("button", { name: /Prepare/ }).getAttribute("aria-current")).toBeNull()
})

it("is a jump menu on a phone", async () => {
  media.narrow = true
  renderAt("yaml")
  const menu = await screen.findByRole("combobox", { name: "Jump to" })
  fireEvent.change(menu, { target: { value: "steps/run" } })
  await waitFor(() => expect(editor().getAttribute("data-jump")).toMatch(new RegExp(`^${fileText.indexOf("  - id: run") + 2}-`)))
})

const paste = (target: Element, text: string) => {
  const event = new Event("paste", { bubbles: true, cancelable: true }) as Event & { clipboardData: { getData: () => string } }
  event.clipboardData = { getData: () => text }
  fireEvent(target, event)
}

it("asks before a whole workflow pasted on the forms replaces this one", async () => {
  renderAt("visual", "#steps/prep")
  const outline = await screen.findByRole("navigation", { name: "Outline" })
  const other = "schema_version: 1\nname: flow\ntitle: Another\nsteps:\n  - id: x\n    title: X\n    type: task.note\n    with: {body: hi}\n"
  paste(outline, other)
  const ask = screen.getByRole("alertdialog", { name: "Replace the workflow" })
  fireEvent.click(within(ask).getByRole("button", { name: "Replace" }))
  fireEvent.click(screen.getByRole("radio", { name: "YAML" }))
  expect(editor().value).toBe(other)
  fireEvent.click(screen.getByRole("button", { name: "Undo" }))
  expect(editor().value).toBe(fileText)
})

it("inserts pasted steps after the step chosen, under IDs not taken", async () => {
  renderAt("visual", "#steps/prep")
  const outline = await screen.findByRole("navigation", { name: "Outline" })
  paste(outline, "- id: run\n  title: Run again\n  type: agent.run\n  with: {prompt: Again}\n- id: tidy\n  title: Tidy\n  type: task.note\n  with: {body: done}\n")
  fireEvent.click(screen.getByRole("radio", { name: "YAML" }))
  expect(editor().value.match(/- id: \S+/g)).toEqual(["- id: prep", "- id: run-2", "- id: tidy", "- id: run"])
})

it("leaves text pasted into a field to the field", async () => {
  renderAt("visual", "#steps/prep")
  const title = await screen.findByRole("textbox", { name: "Step title" })
  paste(title, "- id: x\n  type: task.note\n")
  fireEvent.click(screen.getByRole("radio", { name: "YAML" }))
  expect(editor().value).toBe(fileText)
})
