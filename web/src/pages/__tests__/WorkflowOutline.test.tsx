import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { WorkflowEditPage } from "../WorkflowEditPage"

// The editor's shell (LEG-505): the outline, the inspector of the row its
// address names, the keys, and on a phone the inspector as a sheet.

const { apiFetch, media } = vi.hoisted(() => ({ apiFetch: vi.fn(), media: { narrow: false } }))
vi.mock("../../lib/api", () => ({ apiFetch }))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../hooks/useMediaQuery", () => ({ useMediaQuery: () => media.narrow }))
vi.mock("../../hooks/useWorkflow", () => ({ useWorkflows: () => ({ data: { definitions: [], errors: null, folders: [{ id: "global", label: "Your workflows", path: "/g" }] }, refresh: vi.fn() }) }))
vi.mock("../../components/workflow-editor/LazyWorkflowYamlEditor", () => ({
  LazyWorkflowYamlEditor: ({ value, onChange, label = "Workflow YAML" }: { value: string; onChange: (v: string) => void; label?: string }) =>
    <textarea aria-label={label} value={value} onChange={event => onChange(event.target.value)} />,
}))
vi.mock("../../components/workflow-graph/LazyWorkflowGraph", () => ({ LazyWorkflowGraph: () => null }))

const fileText = `schema_version: 1
name: review-pr
title: Review a pull request
inputs:
  pr: {type: integer, required: true}
on:
  schedule:
    every: weekdays
    at: "09:00"
    enabled: false
steps:
  # Prepare it.
  - id: prep
    title: Prepare
    type: repo.ensure
    with:
      repo: a/b
  - id: run
    title: Run
    type: agent.run
    with:
      prompt: Review \${{ workflow.inputs.pr }}
    outputs:
      summary: {type: string}
`
const json = (body: unknown, status = 200) => Promise.resolve(new Response(JSON.stringify(body), { status }))
let puts: { yaml: string }[] = []
let fileYaml = fileText

beforeEach(() => {
  localStorage.clear()
  localStorage.setItem("legato:workflow-editor-mode", "visual")
  media.narrow = false
  puts = []
  fileYaml = fileText
  apiFetch.mockReset()
  apiFetch.mockImplementation((_: string, path: string, init?: RequestInit) => {
    const body = init?.body ? JSON.parse(String(init.body)) : {}
    if (path === "/api/workflow-files/check") return json({ definition: { name: "review-pr" }, graph: null, diagnostics: [] })
    if (path === "/api/workflows/review-pr/file" && init?.method === "PUT") { puts.push(body); return json({ name: "review-pr", path: "/g/review-pr.yaml", folder: "global", digest: "sha256:new", diagnostics: [] }) }
    if (path === "/api/workflows/review-pr/file") return json({ name: "review-pr", yaml: fileYaml, path: "/g/review-pr.yaml", folder: "global", digest: "sha256:old", editable: true })
    return json({}, 404)
  })
})
afterEach(cleanup)

function Where() {
  const location = useLocation()
  return <output aria-label="Address">{location.hash}</output>
}

function renderAt(hash = "") {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={client}><MemoryRouter initialEntries={["/workflows", `/workflows/review-pr/edit${hash}`]} initialIndex={1}>
    <Routes>
      <Route path="/workflows/:name/edit" element={<><WorkflowEditPage mode="edit" /><Where /></>} />
      <Route path="/workflows" element={<p>workflows page</p>} />
    </Routes>
  </MemoryRouter></QueryClientProvider>)
}

const address = () => screen.getByRole("status", { name: "Address" }).textContent
const outline = () => screen.getByRole("navigation", { name: "Outline" })
const inspector = () => screen.getByRole("region", { name: "Inspector" })
const row = (name: RegExp) => within(outline()).getByRole("button", { name })
const commit = (element: HTMLElement, value: string) => { fireEvent.change(element, { target: { value } }); fireEvent.blur(element) }
const yamlOf = async () => { fireEvent.click(screen.getByRole("radio", { name: "YAML" })); return (await screen.findByRole("textbox", { name: "Workflow YAML" }) as HTMLTextAreaElement).value }

it("says the workflow's title and file in its header", async () => {
  renderAt()
  expect(await screen.findByRole("heading", { level: 1, name: "Review a pull request" })).toBeTruthy()
  expect(screen.getByText("review-pr").parentElement?.textContent).toBe("review-pr · Your workflows")
  expect(await screen.findByRole("button", { name: "No problems" })).toBeTruthy()
})

it("lists the workflow as When, Asks for, Then and About", async () => {
  renderAt()
  await screen.findByRole("navigation", { name: "Outline" })
  const sections = within(outline()).getAllByRole("region").map(section => section.getAttribute("aria-label"))
  expect(sections).toEqual(["When", "Asks for", "Then", "About"])
  expect(within(screen.getByRole("region", { name: "When" })).getByRole("button", { name: /On a schedule.*paused/ })).toBeTruthy()
  expect(within(screen.getByRole("region", { name: "Then" })).getAllByRole("button").map(b => b.textContent)).toEqual([
    expect.stringContaining("Prepare"), expect.stringContaining("Run"), expect.stringContaining("Add step"),
  ])
})

it("opens the row its address names, and a row chosen is the address", async () => {
  renderAt("#steps/run")
  expect((await screen.findByRole("textbox", { name: "Step title" }) as HTMLInputElement).value).toBe("Run")
  expect(row(/^Run/).getAttribute("aria-current")).toBe("true")
  fireEvent.click(row(/^pr/))
  expect(address()).toBe("#inputs/pr")
  expect(await within(inspector()).findByRole("textbox", { name: "Name of input pr" })).toBeTruthy()
  fireEvent.click(row(/^About/))
  expect(address()).toBe("#about")
  expect(within(inspector()).getByRole("textbox", { name: "Title" })).toBeTruthy()
})

it("keeps the step chosen when its ID is renamed", async () => {
  renderAt("#steps/run")
  commit(await screen.findByRole("textbox", { name: "ID of step run" }), "review")
  await waitFor(() => expect(address()).toBe("#steps/review"))
  expect((screen.getByRole("textbox", { name: "Step title" }) as HTMLInputElement).value).toBe("Run")
})

it("edits a step's title in the inspector's header, the rest of the file kept", async () => {
  renderAt("#steps/prep")
  commit(await screen.findByRole("textbox", { name: "Step title" }), "Get it ready")
  expect(await yamlOf()).toBe(fileText.replace("title: Prepare", "title: Get it ready"))
})

it("has Setup, Outputs and YAML for a step; its YAML changes that step alone", async () => {
  renderAt("#steps/run")
  await screen.findByRole("textbox", { name: "Step title" })
  fireEvent.click(within(inspector()).getByRole("tab", { name: "Outputs" }))
  expect(within(inspector()).getByRole("group", { name: "Output summary of step run" })).toBeTruthy()
  fireEvent.click(within(inspector()).getByRole("tab", { name: "YAML" }))
  const own = await within(inspector()).findByRole("textbox", { name: "YAML of steps/run" }) as HTMLTextAreaElement
  expect(own.value.startsWith("id: run\ntitle: Run\n")).toBe(true)
  fireEvent.change(own, { target: { value: own.value.replace("title: Run", "title: Look") } })
  // Half a line does not parse: nothing is written until it does.
  fireEvent.change(own, { target: { value: own.value + "\n  bad: [" } })
  expect(within(inspector()).getByRole("status").textContent).toContain("Not written")
  expect(await yamlOf()).toBe(fileText.replace("title: Run", "title: Look"))
})

it("moves through the rows with j and k, and switches to YAML with y", async () => {
  renderAt("#steps/prep")
  await screen.findByRole("textbox", { name: "Step title" })
  row(/^Prepare/).focus()
  fireEvent.keyDown(row(/^Prepare/), { key: "j" })
  expect(address()).toBe("#steps/run")
  fireEvent.keyDown(row(/^Run/), { key: "j" })
  expect(address()).toBe("#about")
  fireEvent.keyDown(row(/^About/), { key: "k" })
  fireEvent.keyDown(row(/^Run/), { key: "k" })
  fireEvent.keyDown(row(/^Prepare/), { key: "k" })
  expect(address()).toBe("#inputs/pr")
  // Not while typing in a field.
  fireEvent.keyDown(screen.getByRole("textbox", { name: "Name of input pr" }), { key: "y" })
  expect(screen.getByRole("radio", { name: "Visual" }).getAttribute("aria-checked")).toBe("true")
  fireEvent.keyDown(document.body, { key: "y" })
  expect(screen.getByRole("radio", { name: "YAML" }).getAttribute("aria-checked")).toBe("true")
})

it("saves with Mod-S", async () => {
  renderAt("#steps/prep")
  commit(await screen.findByRole("textbox", { name: "Step title" }), "Ready")
  await waitFor(() => expect(screen.getByRole("button", { name: "Save" })).toHaveProperty("disabled", false))
  fireEvent.keyDown(document.body, { key: "s", ctrlKey: true })
  await waitFor(() => expect(puts.map(p => p.yaml)).toEqual([fileText.replace("title: Prepare", "title: Ready")]))
})

it("adds a step and opens it", async () => {
  renderAt()
  fireEvent.click(within(await screen.findByRole("region", { name: "Then" })).getByRole("button", { name: "Add step" }))
  fireEvent.click(within(screen.getByRole("group", { name: "Step types" })).getByRole("button", { name: /Move a task/ }))
  await waitFor(() => expect(address()).toBe("#steps/task-move"))
  expect((screen.getByRole("textbox", { name: "Step title" }) as HTMLInputElement).value).toBe("Move a task")
})

it("on a phone, is the outline, opens a row as a sheet, and Back returns to the outline", async () => {
  media.narrow = true
  renderAt()
  await screen.findByRole("navigation", { name: "Outline" })
  expect(screen.queryByRole("region", { name: "Inspector" })).toBeNull()
  expect(within(screen.getByRole("toolbar", { name: "Save" })).getByRole("button", { name: "Save" })).toBeTruthy()
  fireEvent.click(row(/^Run/))
  const sheet = await screen.findByRole("dialog", { name: "Run" })
  expect(address()).toBe("#steps/run")
  expect(within(sheet).getByRole("textbox", { name: "Step title" })).toBeTruthy()
  fireEvent.click(within(sheet).getByRole("button", { name: "Outline" }))
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())
  expect(address()).toBe("")
  expect(screen.getByRole("navigation", { name: "Outline" })).toBeTruthy()
})

it("on a phone, opens the sheet of an address it is reloaded at", async () => {
  media.narrow = true
  renderAt("#inputs/pr")
  const sheet = await screen.findByRole("dialog", { name: "pr" })
  fireEvent.click(within(sheet).getByRole("button", { name: "Outline" }))
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())
  // Not pushed by a tap, so closing it stays on the page.
  expect(screen.queryByText("workflows page")).toBeNull()
})

// Review fixes (LEG-505).
it("edits an ordinary item when another item has a YAML anchor", async () => {
  fileYaml = fileText.replace("title: Review a pull request", "title: Review a pull request\ndescription: &why Because")
  renderAt("#steps/prep")
  commit(await screen.findByRole("textbox", { name: "Step title" }), "Get ready")
  expect(await yamlOf()).toContain("title: Get ready")
})

it("lets Enter press the outline's other buttons", async () => {
  renderAt("#steps/prep")
  await screen.findByRole("textbox", { name: "Step title" })
  const add = within(outline()).getByRole("button", { name: "Add input" })
  const event = new KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true })
  add.dispatchEvent(event)
  expect(event.defaultPrevented).toBe(false)
})

it("keeps an item's YAML that doesn't read yet across tabs, and counts it as unsaved", async () => {
  renderAt("#steps/run")
  await screen.findByRole("textbox", { name: "Step title" })
  fireEvent.click(within(inspector()).getByRole("tab", { name: "YAML" }))
  const own = await within(inspector()).findByRole("textbox", { name: "YAML of steps/run" }) as HTMLTextAreaElement
  const held = own.value + "\n  bad: ["
  fireEvent.change(own, { target: { value: held } })
  const unload = () => { const e = new Event("beforeunload", { cancelable: true }); window.dispatchEvent(e); return e.defaultPrevented }
  await waitFor(() => expect(unload()).toBe(true))
  fireEvent.click(within(inspector()).getByRole("tab", { name: "Setup" }))
  fireEvent.click(within(inspector()).getByRole("tab", { name: "YAML" }))
  expect((await within(inspector()).findByRole("textbox", { name: "YAML of steps/run" }) as HTMLTextAreaElement).value).toBe(held)
})

it("stays on the YAML tab while a step's ID is renamed in it", async () => {
  renderAt("#steps/run")
  await screen.findByRole("textbox", { name: "Step title" })
  fireEvent.click(within(inspector()).getByRole("tab", { name: "YAML" }))
  const own = await within(inspector()).findByRole("textbox", { name: "YAML of steps/run" }) as HTMLTextAreaElement
  fireEvent.change(own, { target: { value: own.value.replace("id: run", "id: r") } })
  await waitFor(() => expect(address()).toBe("#steps/r"))
  expect(within(inspector()).getByRole("tab", { name: "YAML" }).getAttribute("aria-selected")).toBe("true")
  expect(within(inspector()).getByRole("textbox", { name: "YAML of steps/r" })).toBeTruthy()
})

it("keeps an item's held YAML across a switch to the whole file's YAML and back", async () => {
  renderAt("#steps/run")
  await screen.findByRole("textbox", { name: "Step title" })
  fireEvent.click(within(inspector()).getByRole("tab", { name: "YAML" }))
  const own = await within(inspector()).findByRole("textbox", { name: "YAML of steps/run" }) as HTMLTextAreaElement
  const held = own.value + "\n  bad: ["
  fireEvent.change(own, { target: { value: held } })
  fireEvent.click(screen.getByRole("radio", { name: "YAML" }))
  await screen.findByRole("textbox", { name: "Workflow YAML" })
  const unload = () => { const e = new Event("beforeunload", { cancelable: true }); window.dispatchEvent(e); return e.defaultPrevented }
  expect(unload()).toBe(true)
  fireEvent.click(screen.getByRole("radio", { name: "Visual" }))
  fireEvent.click(await within(await screen.findByRole("region", { name: "Inspector" })).findByRole("tab", { name: "YAML" }))
  expect((await within(inspector()).findByRole("textbox", { name: "YAML of steps/run" }) as HTMLTextAreaElement).value).toBe(held)
})

it("keeps held YAML with its own step when steps move", async () => {
  renderAt("#steps/prep")
  await screen.findByRole("textbox", { name: "Step title" })
  fireEvent.click(within(inspector()).getByRole("tab", { name: "YAML" }))
  const own = await within(inspector()).findByRole("textbox", { name: "YAML of steps/prep" }) as HTMLTextAreaElement
  fireEvent.change(own, { target: { value: own.value + "\n  bad: [" } })
  fireEvent.click(within(inspector()).getByRole("button", { name: "Move step prep down" }))
  fireEvent.click(row(/^Run/))
  fireEvent.click(await within(inspector()).findByRole("tab", { name: "YAML" }))
  expect((await within(inspector()).findByRole("textbox", { name: "YAML of steps/run" }) as HTMLTextAreaElement).value.startsWith("id: run\n")).toBe(true)
})
