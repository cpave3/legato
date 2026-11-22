import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { MemoryRouter, Route, Routes } from "react-router-dom"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { WorkflowEditPage } from "../WorkflowEditPage"
import { schemaDiagnostics } from "../../lib/workflowSchema"
import { expressionDiagnostics } from "../../lib/workflowExpressions"

const { apiFetch } = vi.hoisted(() => ({ apiFetch: vi.fn() }))
vi.mock("../../lib/api", () => ({ apiFetch }))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../hooks/useWorkflow", () => ({ useWorkflows: () => ({ data: { definitions: [], errors: null, folders: [] }, refresh: vi.fn() }) }))
vi.mock("../../components/workflow-editor/LazyWorkflowYamlEditor", () => ({
  LazyWorkflowYamlEditor: ({ value, onChange }: { value: string; onChange: (v: string) => void }) =>
    <textarea aria-label="Workflow YAML" value={value} onChange={event => onChange(event.target.value)} />,
}))
vi.mock("../../components/workflow-editor/LazyWorkflowGraph", () => ({ LazyWorkflowGraph: () => null }))
vi.mock("../../components/workflow-graph/LazyWorkflowGraph", () => ({ LazyWorkflowGraph: () => null }))

const fileText = `# Review every pull request.
schema_version: 1
name: review
title: Review # the title
inputs:
  pr: {type: integer, required: true} # the number
steps:
  # The reviewer.
  - id: run
    title: Run
    type: agent.run
    with:
      prompt: Review \${{ workflow.inputs.pr }}
`
const json = (body: unknown, status = 200) => Promise.resolve(new Response(JSON.stringify(body), { status }))
let checkDiagnostics: unknown[] = []

beforeEach(() => {
  localStorage.setItem("legato:workflow-editor-mode", "visual")
  checkDiagnostics = []
  apiFetch.mockReset()
  apiFetch.mockImplementation((_: string, path: string) => {
    if (path === "/api/workflow-files/check") return json({ definition: { name: "review" }, graph: null, diagnostics: checkDiagnostics })
    if (path === "/api/workflows/review/file") return json({ name: "review", yaml: fileText, path: "/g/review.yaml", folder: "global", digest: "sha256:old", editable: true })
    return json({}, 404)
  })
})
afterEach(cleanup)

function renderEdit() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={client}><MemoryRouter initialEntries={["/workflows/review/edit"]}>
    <Routes><Route path="/workflows/:name/edit" element={<WorkflowEditPage mode="edit" />} /></Routes>
  </MemoryRouter></QueryClientProvider>)
}

const toYaml = () => fireEvent.click(screen.getByRole("radio", { name: "YAML" }))
const toVisual = () => fireEvent.click(screen.getByRole("radio", { name: "Visual" }))
const yaml = () => (screen.getByRole("textbox", { name: "Workflow YAML" }) as HTMLTextAreaElement).value
const commit = (element: HTMLElement, value: string) => { fireEvent.change(element, { target: { value } }); fireEvent.blur(element) }
const clean = (text: string) => [...schemaDiagnostics(text), ...expressionDiagnostics(text)].filter(d => d.severity === "error")

it("shows a title edited in Visual in the YAML, with every comment kept", async () => {
  renderEdit()
  commit(await screen.findByRole("textbox", { name: "Title" }), "Review it")
  toYaml()
  expect(yaml()).toBe(fileText.replace("title: Review # the title", "title: Review it # the title"))
  expect(clean(yaml())).toEqual([])
})

it("shows a YAML edit in Visual", async () => {
  localStorage.setItem("legato:workflow-editor-mode", "yaml")
  renderEdit()
  await waitFor(() => expect(yaml()).toBe(fileText))
  fireEvent.change(screen.getByRole("textbox", { name: "Workflow YAML" }), { target: { value: fileText.replace("title: Review #", "title: Typed #") } })
  toVisual()
  expect((await screen.findByRole("textbox", { name: "Title" }) as HTMLInputElement).value).toBe("Typed")
})

it("renames an input and its references", async () => {
  renderEdit()
  commit(await screen.findByRole("textbox", { name: "Name of input pr" }), "number")
  toYaml()
  expect(yaml()).toContain("  number: {type: integer, required: true} # the number")
  expect(yaml()).toContain("prompt: Review ${{ workflow.inputs.number }}")
  expect(clean(yaml())).toEqual([])
})

it("adds an input, sets its fields, and the result passes the checks", async () => {
  renderEdit()
  fireEvent.change(await screen.findByRole("textbox", { name: "New input name" }), { target: { value: "repo" } })
  fireEvent.click(screen.getByRole("button", { name: "Add input" }))
  const input = await screen.findByRole("listitem", { name: "Input repo" })
  fireEvent.change(within(input).getByRole("combobox", { name: "Type of input repo" }), { target: { value: "repository" } })
  fireEvent.click(within(input).getByRole("checkbox", { name: "Input repo is required" }))
  commit(within(input).getByRole("textbox", { name: "Description of input repo" }), "Where to look")
  toYaml()
  expect(yaml()).toContain("  repo:\n    type: repository\n    required: true\n    description: Where to look\n")
  expect(clean(yaml())).toEqual([])
})

it("undoes across both modes", async () => {
  renderEdit()
  commit(await screen.findByRole("textbox", { name: "Title" }), "Step one")
  toYaml()
  fireEvent.change(screen.getByRole("textbox", { name: "Workflow YAML" }), { target: { value: yaml().replace("Step one", "Step two") } })
  fireEvent.click(screen.getByRole("button", { name: "Undo" }))
  expect(yaml()).toContain("title: Step one")
  fireEvent.click(screen.getByRole("button", { name: "Undo" }))
  expect(yaml()).toBe(fileText)
  fireEvent.click(screen.getByRole("button", { name: "Redo" }))
  toVisual()
  expect((await screen.findByRole("textbox", { name: "Title" }) as HTMLInputElement).value).toBe("Step one")
})

it("won't show YAML that doesn't parse in Visual, and says why", async () => {
  localStorage.setItem("legato:workflow-editor-mode", "yaml")
  renderEdit()
  await waitFor(() => expect(yaml()).toBe(fileText))
  fireEvent.change(screen.getByRole("textbox", { name: "Workflow YAML" }), { target: { value: "name: [oops\n" } })
  toVisual()
  const alert = await screen.findByRole("alert")
  expect(alert.textContent).toContain("doesn't parse")
  fireEvent.click(within(alert).getByRole("button", { name: "Edit as YAML" }))
  expect(yaml()).toBe("name: [oops\n")
})

it("shows the server's problems on their fields", async () => {
  checkDiagnostics = [{ severity: "error", message: "run_title: input \"nope\" is not declared", path: "run_title", line: 1, column: 1, end_line: 1, end_column: 2 }]
  renderEdit()
  const field = await screen.findByRole("textbox", { name: "Run title" })
  await waitFor(() => expect(field.getAttribute("aria-invalid")).toBe("true"))
  expect(within(screen.getByRole("region", { name: "Visual" })).getByText("run_title: input \"nope\" is not declared")).toBeTruthy()
})

it("sends a file with a step that is not a mapping to YAML, rather than failing", async () => {
  localStorage.setItem("legato:workflow-editor-mode", "yaml")
  renderEdit()
  await waitFor(() => expect(yaml()).toBe(fileText))
  fireEvent.change(screen.getByRole("textbox", { name: "Workflow YAML" }), { target: { value: fileText.replace(/steps:\n[\s\S]*$/, "steps:\n  -\n") } })
  toVisual()
  const alert = await screen.findByRole("alert")
  expect(alert.textContent).toContain("step 1 is not a mapping")
  expect(within(alert).getByRole("button", { name: "Edit as YAML" })).toBeTruthy()
})

it("keeps a choice with a comma in it when another is added", async () => {
  const withChoices = fileText.replace("  pr: {type: integer, required: true} # the number\n", "  pr: {type: integer, required: true} # the number\n  city: {type: string, choices: [\"Washington, DC\", London]}\n")
  apiFetch.mockImplementation((_: string, path: string) => path === "/api/workflows/review/file"
    ? json({ name: "review", yaml: withChoices, path: "/g/review.yaml", folder: "global", digest: "sha256:old", editable: true })
    : json({ definition: null, graph: null, diagnostics: [] }))
  renderEdit()
  fireEvent.change(await screen.findByRole("textbox", { name: "New choice of input city" }), { target: { value: "Paris" } })
  fireEvent.click(screen.getByRole("button", { name: "Add a choice to input city" }))
  toYaml()
  expect(yaml()).toContain("choices:\n      - Washington, DC\n      - London\n      - Paris\n")
})

it("keeps the step Undo returned to when typing follows", async () => {
  localStorage.setItem("legato:workflow-editor-mode", "yaml")
  renderEdit()
  await waitFor(() => expect(yaml()).toBe(fileText))
  const typeYaml = (value: string) => fireEvent.change(screen.getByRole("textbox", { name: "Workflow YAML" }), { target: { value } })
  const a = fileText.replace("title: Review #", "title: A #")
  typeYaml(a)
  toVisual()
  commit(await screen.findByRole("textbox", { name: "Title" }), "B")
  fireEvent.click(screen.getByRole("button", { name: "Undo" }))
  toYaml()
  expect(yaml()).toBe(a)
  typeYaml(a.replace("title: A #", "title: C #"))
  fireEvent.click(screen.getByRole("button", { name: "Undo" }))
  expect(yaml()).toBe(a)
})

it("counts text typed in a field and not yet written as unsaved", async () => {
  renderEdit()
  const title = await screen.findByRole("textbox", { name: "Title" })
  const unload = () => { const event = new Event("beforeunload", { cancelable: true }); window.dispatchEvent(event); return event.defaultPrevented }
  expect(unload()).toBe(false)
  fireEvent.change(title, { target: { value: "Typed, not left" } })
  await waitFor(() => expect(unload()).toBe(true))
})
