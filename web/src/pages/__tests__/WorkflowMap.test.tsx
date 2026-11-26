import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { WorkflowEditPage } from "../WorkflowEditPage"

// The graph (LEG-515): read-only, linked to the selection both ways; stale
// while the file can't be read. On a computer it is the page, under the
// outline's cards (LEG-655); on a phone and beside the YAML, Graph shows
// it in the outline's place.

const { apiFetch, media } = vi.hoisted(() => ({ apiFetch: vi.fn(), media: { narrow: false } }))
vi.mock("../../lib/api", () => ({ apiFetch }))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../hooks/useMediaQuery", () => ({ useMediaQuery: (query: string) => query === "(max-width: 767px)" && media.narrow }))
vi.mock("../../hooks/useWorkflow", () => ({ useWorkflows: () => ({ data: { definitions: [], errors: null, folders: [] }, refresh: vi.fn() }) }))
vi.mock("../../components/workflow-editor/LazyWorkflowYamlEditor", () => ({
  LazyWorkflowYamlEditor: ({ value, onChange, label = "Workflow YAML" }: { value: string; onChange: (v: string) => void; label?: string }) =>
    <textarea aria-label={label} value={value} onChange={event => onChange(event.target.value)} />,
}))
// The graph: a button a node, the one selected pressed.
vi.mock("../../components/workflow-graph/LazyWorkflowGraph", () => ({
  LazyWorkflowGraph: ({ graph, selectedId, onSelect }: { graph: { nodes: { id: string }[] }; selectedId?: string | null; onSelect?: (id: string) => void }) =>
    <div aria-label="Graph">{graph.nodes.map(node => <button key={node.id} type="button" aria-pressed={selectedId === node.id} onClick={() => onSelect?.(node.id)}>{node.id}</button>)}</div>,
}))

const fileText = `schema_version: 1
name: flow
title: Flow
on:
  schedule: {every: daily, at: "09:00"}
steps:
  - id: prep
    title: Prepare
    type: repo.ensure
    with: {repo: a/b}
  - id: run
    title: Run
    type: agent.run
    with: {prompt: Look}
`
const graph = { version: 1, nodes: [{ id: "trigger:manual", kind: "trigger", trigger: { kind: "manual", summary: "Run by hand", enabled: true } }, { id: "trigger:schedule", kind: "trigger", trigger: { kind: "schedule", summary: "", enabled: true } }, { id: "step:prep", kind: "step", step: { id: "prep" } }, { id: "step:run", kind: "step", step: { id: "run" } }], edges: [] }
const json = (body: unknown, status = 200) => Promise.resolve(new Response(JSON.stringify(body), { status }))

beforeEach(() => {
  media.narrow = false
  localStorage.clear()
  localStorage.setItem("legato:workflow-editor-mode", "visual")
  apiFetch.mockReset()
  apiFetch.mockImplementation((_: string, path: string, init?: RequestInit) => {
    const body = init?.body ? JSON.parse(String(init.body)) : {}
    if (path === "/api/workflow-files/check") return json(String(body.yaml).includes("[oops") ? { definition: null, graph: null, diagnostics: [] } : { definition: { name: "flow" }, graph, diagnostics: [] })
    if (path === "/api/workflows/flow/file") return json({ name: "flow", yaml: fileText, path: "/g/flow.yaml", folder: "global", digest: "sha256:old", editable: true })
    return json({}, 404)
  })
})
afterEach(cleanup)

function Where() { return <output aria-label="Address">{useLocation().hash}</output> }

function renderAt(hash: string) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(<QueryClientProvider client={client}><MemoryRouter initialEntries={[`/workflows/flow/edit${hash}`]}>
    <Routes><Route path="/workflows/:name/edit" element={<><WorkflowEditPage mode="edit" /><Where /></>} /></Routes>
  </MemoryRouter></QueryClientProvider>)
}

const canvas = () => screen.findByRole("region", { name: "Workflow graph" })
const address = () => screen.getByRole("status", { name: "Address" }).textContent
// Graph, in the outline's place: on a phone, or beside the YAML.
const toGraph = async () => {
  const graph = await screen.findByRole("radio", { name: "Graph" })
  await waitFor(() => expect(graph).toHaveProperty("disabled", false))
  fireEvent.click(graph)
  return screen.findByRole("region", { name: "Graph" })
}

it("draws the graph beside the outline, the row chosen marked in it", async () => {
  renderAt("#steps/run")
  const graph = await canvas()
  expect((await within(graph).findByRole("button", { name: "step:run" })).getAttribute("aria-pressed")).toBe("true")
  expect(screen.getByRole("navigation", { name: "Outline" })).toBeTruthy()
  expect(screen.queryByRole("radio", { name: "Graph" })).toBeNull()
})

it("chooses a node's row", async () => {
  renderAt("#steps/run")
  const graph = await canvas()
  fireEvent.click(await within(graph).findByRole("button", { name: "step:prep" }))
  await waitFor(() => expect(address()).toBe("#steps/prep"))
  fireEvent.click(within(graph).getByRole("button", { name: "trigger:schedule" }))
  await waitFor(() => expect(address()).toBe("#on/schedule"))
  // Running by hand is no row.
  fireEvent.click(within(graph).getByRole("button", { name: "trigger:manual" }))
  expect(address()).toBe("#on/schedule")
})

it("keeps the last graph, marked stale, while the file can't be read", async () => {
  renderAt("#steps/run")
  const graph = await canvas()
  await within(graph).findByRole("button", { name: "step:run" })
  expect(within(graph).queryByText(/last version Legato could read/)).toBeNull()
  fireEvent.click(screen.getByRole("radio", { name: "YAML" }))
  fireEvent.change(await screen.findByRole("textbox", { name: "Workflow YAML" }), { target: { value: fileText.replace("title: Run", "title: Run [oops]") } })
  fireEvent.click(screen.getByRole("radio", { name: "Visual" }))
  const stale = await canvas()
  await waitFor(() => expect(within(stale).getByText(/last version Legato could read/)).toBeTruthy())
  expect(within(stale).getByRole("button", { name: "step:run" })).toBeTruthy()
})

it("keeps the last graph, marked stale, while the YAML doesn't parse", async () => {
  renderAt("#steps/run")
  await within(await canvas()).findByRole("button", { name: "step:run" })
  fireEvent.click(screen.getByRole("radio", { name: "YAML" }))
  await toGraph()
  fireEvent.change(await screen.findByRole("textbox", { name: "Workflow YAML" }), { target: { value: fileText + "broken: [oops\n" } })
  const stale = await screen.findByRole("region", { name: "Graph" })
  await waitFor(() => expect(within(stale).getByText(/last version Legato could read/)).toBeTruthy())
  expect(within(stale).getByRole("button", { name: "step:run" })).toBeTruthy()
  fireEvent.click(screen.getByRole("radio", { name: "Visual" }))
  const graph = await canvas()
  expect(within(graph).getByText(/last version Legato could read/)).toBeTruthy()
  expect(within(graph).getByText(/can't read the YAML/)).toBeTruthy()
})

it("on a phone, Graph shows the graph in the outline's place, and a node opens its row", async () => {
  media.narrow = true
  renderAt("")
  const graph = await toGraph()
  expect(screen.queryByRole("navigation", { name: "Outline" })).toBeNull()
  fireEvent.click(within(graph).getByRole("button", { name: "step:prep" }))
  await waitFor(() => expect(address()).toBe("#steps/prep"))
  expect(await screen.findByRole("dialog", { name: "Prepare" })).toBeTruthy()
})
