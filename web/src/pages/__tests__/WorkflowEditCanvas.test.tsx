import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { WorkflowEditPage } from "../WorkflowEditPage"

// LEG-655: the editor's Visual side is the workflow's graph, with the
// outline's sections as cards docked on the left and the inspector docked
// on the right, as on a workflow's page.

const { apiFetch } = vi.hoisted(() => ({ apiFetch: vi.fn() }))
vi.mock("../../lib/api", () => ({ apiFetch }))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../hooks/useMediaQuery", () => ({ useMediaQuery: () => false }))
vi.mock("../../hooks/useWorkflow", () => ({ useWorkflows: () => ({ data: { definitions: [], errors: null, folders: [{ id: "global", label: "Your workflows", path: "/g" }], trigger_states: { flow: [{ trigger: "schedule", file_enabled: true, enabled: true, overridden: false }] } }, refresh: vi.fn() }) }))
vi.mock("../../components/workflow-editor/LazyWorkflowYamlEditor", () => ({
  LazyWorkflowYamlEditor: ({ value, onChange, label = "Workflow YAML" }: { value: string; onChange: (v: string) => void; label?: string }) =>
    <textarea aria-label={label} value={value} onChange={event => onChange(event.target.value)} />,
}))
// The graph: a button a node, the one selected pressed; centerOn says
// where it was last asked to go.
vi.mock("../../components/workflow-graph/LazyWorkflowGraph", () => ({
  LazyWorkflowGraph: ({ graph, selectedId, onSelect, centerOn }: { graph: { nodes: { id: string }[] }; selectedId?: string | null; onSelect?: (id: string) => void; centerOn?: { id: string } | null }) =>
    <div aria-label="Graph" data-centred={centerOn?.id ?? ""}>{graph.nodes.map(node => <button key={node.id} type="button" aria-pressed={selectedId === node.id} onClick={() => onSelect?.(node.id)}>{node.id}</button>)}</div>,
}))

const fileText = `schema_version: 2
name: flow
title: Flow
inputs:
  pr: {type: integer, required: true}
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
const graph = { version: 2, nodes: [{ id: "trigger:manual", kind: "trigger", trigger: { kind: "manual", summary: "Run by hand", enabled: true } }, { id: "trigger:schedule", kind: "trigger", trigger: { kind: "schedule", summary: "", enabled: true } }, { id: "step:prep", kind: "step", step: { id: "prep" } }, { id: "step:run", kind: "step", step: { id: "run" } }], edges: [] }
const json = (body: unknown, status = 200) => Promise.resolve(new Response(JSON.stringify(body), { status }))
let checks = true

beforeEach(() => {
  // jsdom has no CSS.escape, which j and k use to focus the next row.
  if (!globalThis.CSS?.escape) vi.stubGlobal("CSS", { escape: (value: string) => value })
  localStorage.clear()
  localStorage.setItem("legato:workflow-editor-mode", "visual")
  checks = true
  apiFetch.mockReset()
  apiFetch.mockImplementation((_: string, path: string) => {
    if (path === "/api/workflow-files/check") return checks ? json({ definition: { name: "flow" }, graph, diagnostics: [] }) : new Promise(() => {})
    if (path === "/api/workflows/flow/file") return json({ name: "flow", yaml: fileText, path: "/g/flow.yaml", folder: "global", digest: "sha256:old", editable: true })
    return json({}, 404)
  })
})
afterEach(() => { cleanup(); vi.restoreAllMocks() })

function Where() { return <output aria-label="Address">{useLocation().hash}</output> }

function renderAt(hash = "") {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(<QueryClientProvider client={client}><MemoryRouter initialEntries={[`/workflows/flow/edit${hash}`]}>
    <Routes><Route path="/workflows/:name/edit" element={<><WorkflowEditPage mode="edit" /><Where /></>} /></Routes>
  </MemoryRouter></QueryClientProvider>)
}

const address = () => screen.getByRole("status", { name: "Address" }).textContent
const canvas = () => screen.getByRole("region", { name: "Workflow graph" })
const card = (name: string) => within(screen.getByRole("navigation", { name: "Outline" })).getByRole("region", { name })
const rowAt = (address: string) => screen.getByRole("navigation", { name: "Outline" }).querySelector<HTMLElement>(`[data-address="${address}"]`)!
const node = async (id: string) => within(await screen.findByRole("region", { name: "Workflow graph" })).findByRole("button", { name: id })

it("draws the graph as the page, the outline as cards over it, and no List or Map", async () => {
  renderAt()
  expect(await screen.findByRole("heading", { level: 1, name: "Flow" })).toBeTruthy()
  expect(within(screen.getByRole("banner")).getByText("1 trigger on")).toBeTruthy()
  expect(await node("step:run")).toBeTruthy()
  const cards = within(screen.getByRole("navigation", { name: "Outline" })).getAllByRole("region").map(region => region.getAttribute("aria-label"))
  expect(cards).toEqual(["Triggers", "Inputs", "Steps", "About"])
  expect(within(card("Inputs")).getByRole("button", { name: /pr/ })).toBeTruthy()
  expect(screen.queryByRole("radiogroup", { name: "Outline view" })).toBeNull()
  expect(screen.queryByRole("region", { name: "Inspector" })).toBeNull()
})

it("docks the row chosen in the inspector on the right, and marks and centres its node", async () => {
  renderAt()
  await node("step:run")
  fireEvent.click(rowAt("steps/run"))
  await waitFor(() => expect(address()).toBe("#steps/run"))
  expect((within(screen.getByRole("region", { name: "Inspector" })).getByRole("textbox", { name: "Step title" }) as HTMLInputElement).value).toBe("Run")
  expect((await node("step:run")).getAttribute("aria-pressed")).toBe("true")
  expect(within(canvas()).getByLabelText("Graph").getAttribute("data-centred")).toBe("step:run")
})

it("opens a node's row, and closes the inspector with ✕ or Escape, but not while typing", async () => {
  renderAt()
  fireEvent.click(await node("trigger:schedule"))
  await waitFor(() => expect(address()).toBe("#on/schedule"))
  fireEvent.click(await node("step:prep"))
  await waitFor(() => expect(address()).toBe("#steps/prep"))
  const title = await within(screen.getByRole("region", { name: "Inspector" })).findByRole("textbox", { name: "Step title" })
  fireEvent.keyDown(title, { key: "Escape" })
  expect(address()).toBe("#steps/prep")
  fireEvent.keyDown(document.body, { key: "Escape" })
  await waitFor(() => expect(address()).toBe(""))
  fireEvent.click(await node("step:run"))
  await waitFor(() => expect(address()).toBe("#steps/run"))
  fireEvent.click(within(screen.getByRole("complementary", { name: "Run" })).getByRole("button", { name: "Close" }))
  await waitFor(() => expect(address()).toBe(""))
})

it("leaves Escape to an open step menu", async () => {
  renderAt("#steps/run")
  await node("step:run")
  fireEvent.click(within(card("Steps")).getByRole("button", { name: "Actions for step prep" }))
  const menu = screen.getByRole("menu", { name: "Step prep" })
  fireEvent.keyDown(within(menu).getByRole("menuitem", { name: "Duplicate" }), { key: "Escape" })
  expect(screen.queryByRole("menu", { name: "Step prep" })).toBeNull()
  expect(address()).toBe("#steps/run")
})

it("moves through every card's rows with j and k", async () => {
  renderAt("#inputs/pr")
  await node("step:run")
  fireEvent.keyDown(within(card("Inputs")).getByRole("button", { name: /pr/ }), { key: "j" })
  await waitFor(() => expect(address()).toBe("#steps/prep"))
  fireEvent.keyDown(rowAt("steps/prep"), { key: "k" })
  await waitFor(() => expect(address()).toBe("#inputs/pr"))
})

it("opens a folded card when its row is chosen", async () => {
  renderAt()
  await node("step:run")
  fireEvent.click(within(card("Steps")).getAllByRole("button")[0])
  expect(within(card("Steps")).getAllByRole("button")[0].getAttribute("aria-expanded")).toBe("false")
  expect(rowAt("steps/prep")).toBeNull()
  fireEvent.click(await node("step:prep"))
  await waitFor(() => expect(rowAt("steps/prep")?.getAttribute("aria-current")).toBe("true"))
})

it("where the canvas has no room for both, the cards step aside for the inspector until asked for", async () => {
  // The app's rails leave the canvas 900px: not enough for 360 + 480 and
  // some of the graph.
  vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockReturnValue(900)
  renderAt()
  await node("step:run")
  expect(screen.getByRole("navigation", { name: "Outline" })).toBeTruthy()
  fireEvent.click(await node("step:run"))
  await waitFor(() => expect(address()).toBe("#steps/run"))
  expect(screen.queryByRole("navigation", { name: "Outline" })).toBeNull()
  expect(screen.getByRole("region", { name: "Inspector" })).toBeTruthy()
  fireEvent.click(screen.getByRole("button", { name: /Show panels/ }))
  expect(screen.getByRole("navigation", { name: "Outline" })).toBeTruthy()
  expect(screen.getByRole("region", { name: "Inspector" })).toBeTruthy()
})

it("says the graph comes once Legato has read the file", async () => {
  checks = false
  renderAt()
  await screen.findByRole("navigation", { name: "Outline" })
  expect(within(canvas()).getByText("The graph shows once Legato has read the file.")).toBeTruthy()
})

it("shows the problems as a card when the header's button is pressed", async () => {
  renderAt()
  fireEvent.click(await screen.findByRole("button", { name: "No problems" }))
  expect(within(screen.getByRole("region", { name: "Problems" })).getByText("No problems")).toBeTruthy()
})
