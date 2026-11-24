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
  expect(within(screen.getByRole("region", { name: "When" })).getAllByRole("button").map(b => b.textContent)).toEqual(["Every weekday at 09:00paused", "Add trigger"])
  const then = screen.getByRole("region", { name: "Then" })
  expect([...then.querySelectorAll("[data-address]")].map(b => b.getAttribute("data-address"))).toEqual(["steps/prep", "steps/run"])
  expect(within(then).getByRole("button", { name: "Add step" })).toBeTruthy()
})

it("opens the row its address names, and a row chosen is the address", async () => {
  renderAt("#steps/run")
  expect((await screen.findByRole("textbox", { name: "Step title" }) as HTMLInputElement).value).toBe("Run")
  expect(row(/^\d+\s*Run/).getAttribute("aria-current")).toBe("true")
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
  row(/^\d+\s*Prepare/).focus()
  fireEvent.keyDown(row(/^\d+\s*Prepare/), { key: "j" })
  expect(address()).toBe("#steps/run")
  fireEvent.keyDown(row(/^\d+\s*Run/), { key: "j" })
  expect(address()).toBe("#about")
  fireEvent.keyDown(row(/^About/), { key: "k" })
  fireEvent.keyDown(row(/^\d+\s*Run/), { key: "k" })
  fireEvent.keyDown(row(/^\d+\s*Prepare/), { key: "k" })
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
  fireEvent.click(within(screen.getByRole("dialog", { name: "Add a step" })).getByRole("button", { name: /Move a task/ }))
  await waitFor(() => expect(address()).toBe("#steps/move-a-task"))
  expect((screen.getByRole("textbox", { name: "Step title" }) as HTMLInputElement).value).toBe("Move a task")
})

it("on a phone, is the outline, opens a row as a sheet, and Back returns to the outline", async () => {
  media.narrow = true
  renderAt()
  await screen.findByRole("navigation", { name: "Outline" })
  expect(screen.queryByRole("region", { name: "Inspector" })).toBeNull()
  expect(within(screen.getByRole("toolbar", { name: "Save" })).getByRole("button", { name: "Save" })).toBeTruthy()
  fireEvent.click(row(/^\d+\s*Run/))
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
  fireEvent.click(row(/^\d+\s*Run/))
  fireEvent.click(await within(inspector()).findByRole("tab", { name: "YAML" }))
  expect((await within(inspector()).findByRole("textbox", { name: "YAML of steps/run" }) as HTMLTextAreaElement).value.startsWith("id: run\n")).toBe(true)
})

it("drops held YAML once its item is changed elsewhere, so it no longer counts as unsaved", async () => {
  renderAt("#steps/prep")
  await screen.findByRole("textbox", { name: "Step title" })
  fireEvent.click(within(inspector()).getByRole("tab", { name: "YAML" }))
  const own = await within(inspector()).findByRole("textbox", { name: "YAML of steps/prep" }) as HTMLTextAreaElement
  fireEvent.change(own, { target: { value: own.value + "\n  bad: [" } })
  const unload = () => { const e = new Event("beforeunload", { cancelable: true }); window.dispatchEvent(e); return e.defaultPrevented }
  await waitFor(() => expect(unload()).toBe(true))
  fireEvent.click(screen.getByRole("radio", { name: "YAML" }))
  fireEvent.change(await screen.findByRole("textbox", { name: "Workflow YAML" }), { target: { value: fileText.replace("repo: a/b", "repo: c/d") } })
  fireEvent.click(screen.getByRole("button", { name: "Undo" }))
  fireEvent.click(screen.getByRole("button", { name: "Redo" }))
  // Changed, but not saved: unsaved for that, not for the held text.
  await waitFor(() => expect(screen.getByRole("button", { name: "Save" })).toHaveProperty("disabled", false))
  fireEvent.click(screen.getByRole("button", { name: "Save" }))
  await waitFor(() => expect(puts.length).toBe(1))
  await waitFor(() => expect(unload()).toBe(false))
})

// Step rows and connectors (LEG-506).
const withStarts = fileText.replace("      summary: {type: string}\n", `      summary: {type: string}
  - id: note
    title: Note it
    type: task.note
    trigger: agent_ended
    with:
      body: \${{ steps.run.outputs.summary }}
  - id: ok
    title: Approve
    type: task.move
    trigger: manual
    with:
      task: \${{ workflow.task }}
      column: Done
`)

it("sums each step up on its row, under its number and title", async () => {
  renderAt()
  await screen.findByRole("navigation", { name: "Outline" })
  expect(row(/^1.*Prepare/).textContent).toContain("a/b")
  expect(row(/^2.*Run/).textContent).toContain("Review ${{ workflow.inputs.pr }}")
})

it("says on the connector before each step how it starts", async () => {
  fileYaml = withStarts
  renderAt()
  const steps = await screen.findByRole("list", { name: "Steps" })
  const starts = [...steps.querySelectorAll("[data-start]")].map(el => [el.getAttribute("data-start"), el.textContent])
  expect(starts).toEqual([["first", ""], ["automatic", "then"], ["agent_ended", "when Run's agent ends"], ["manual", "waits for you"]])
})

it("inserts a step where its connector is, its ID following its title until saved", async () => {
  renderAt()
  fireEvent.click(await screen.findByRole("button", { name: "Add a step before Run" }))
  fireEvent.click(within(screen.getByRole("dialog", { name: "Add a step" })).getByRole("button", { name: /Move a task/ }))
  await waitFor(() => expect(address()).toBe("#steps/move-a-task"))
  commit(screen.getByRole("textbox", { name: "Step title" }), "Close the card")
  await waitFor(() => expect(address()).toBe("#steps/close-the-card"))
  const ids = (await yamlOf()).match(/- id: \S+/g)
  expect(ids).toEqual(["- id: prep", "- id: close-the-card", "- id: run"])
})

it("keeps a saved step's ID when its title changes", async () => {
  renderAt("#steps/prep")
  commit(await screen.findByRole("textbox", { name: "Step title" }), "Get ready")
  expect(address()).toBe("#steps/prep")
  expect(await yamlOf()).toContain("- id: prep\n    title: Get ready\n")
})

it("offers to change the references when a step others read is renamed", async () => {
  fileYaml = withStarts
  renderAt("#steps/run")
  commit(await screen.findByRole("textbox", { name: "ID of step run" }), "review")
  const ask = await screen.findByRole("alertdialog", { name: "Rename step run" })
  expect(ask.textContent).toContain("note reads this step's outputs")
  fireEvent.click(within(ask).getByRole("button", { name: "Change them too" }))
  await waitFor(() => expect(address()).toBe("#steps/review"))
  const text = await yamlOf()
  expect(text).toContain("body: ${{ steps.review.outputs.summary }}")
})

it("renames only the ID when asked to", async () => {
  fileYaml = withStarts
  renderAt("#steps/run")
  commit(await screen.findByRole("textbox", { name: "ID of step run" }), "review")
  fireEvent.click(within(await screen.findByRole("alertdialog", { name: "Rename step run" })).getByRole("button", { name: "Only the ID" }))
  const text = await yamlOf()
  expect(text).toContain("- id: review\n")
  expect(text).toContain("body: ${{ steps.run.outputs.summary }}")
})

it("duplicates, moves and deletes a step from its menu", async () => {
  renderAt()
  fireEvent.click(await screen.findByRole("button", { name: "Actions for step prep" }))
  fireEvent.click(within(screen.getByRole("menu", { name: "Step prep" })).getByRole("menuitem", { name: "Duplicate" }))
  await waitFor(() => expect(address()).toBe("#steps/prep-2"))
  fireEvent.click(screen.getByRole("button", { name: "Actions for step prep-2" }))
  fireEvent.click(within(screen.getByRole("menu", { name: "Step prep-2" })).getByRole("menuitem", { name: "Move down" }))
  fireEvent.click(screen.getByRole("button", { name: "Actions for step prep" }))
  fireEvent.click(within(screen.getByRole("menu", { name: "Step prep" })).getByRole("menuitem", { name: "Delete" }))
  expect((await yamlOf()).match(/- id: \S+/g)).toEqual(["- id: run", "- id: prep-2"])
})

it("asks before deleting a step another reads", async () => {
  fileYaml = withStarts
  renderAt()
  fireEvent.click(await screen.findByRole("button", { name: "Actions for step run" }))
  fireEvent.click(within(screen.getByRole("menu", { name: "Step run" })).getByRole("menuitem", { name: "Delete" }))
  const ask = screen.getByRole("alertdialog", { name: "Remove step run" })
  fireEvent.click(within(ask).getByRole("button", { name: "Keep it" }))
  expect((await yamlOf()).match(/- id: \S+/g)).toEqual(["- id: prep", "- id: run", "- id: note", "- id: ok"])
})

it("duplicates the ID-less step whose row it is", async () => {
  fileYaml = fileText.replace(/steps:\n[\s\S]*$/, "steps:\n  - title: First\n    type: task.note\n    with: {body: one}\n  - title: Second\n    type: task.note\n    with: {body: two}\n")
  renderAt()
  fireEvent.click(await screen.findByRole("button", { name: "Actions for step #1" }))
  fireEvent.click(within(screen.getByRole("menu", { name: "Step #1" })).getByRole("menuitem", { name: "Duplicate" }))
  expect((await yamlOf()).match(/title: \S+/g)).toEqual(["title: Review", "title: First", "title: Second", "title: Second"])
})

// The step picker (LEG-507).
it("finds a step type as you type, and Enter adds the first where the + was", async () => {
  renderAt()
  fireEvent.click(await screen.findByRole("button", { name: "Add a step before Run" }))
  const picker = screen.getByRole("dialog", { name: "Add a step" })
  const search = within(picker).getByRole("searchbox", { name: "Search step types" })
  expect(document.activeElement).toBe(search)
  expect(within(picker).getAllByRole("region").map(r => r.getAttribute("aria-label"))).toEqual(["Agents", "Code", "Board", "Chat", "Learn", "Cleanup"])
  fireEvent.change(search, { target: { value: "add a note" } })
  expect(within(picker).getAllByRole("region").map(r => r.getAttribute("aria-label"))).toEqual(["Board"])
  fireEvent.keyDown(search, { key: "Enter" })
  await waitFor(() => expect(address()).toBe("#steps/add-a-note-to-a-task"))
  expect((await yamlOf()).match(/- id: \S+/g)).toEqual(["- id: prep", "- id: add-a-note-to-a-task", "- id: run"])
})

it("focuses the first essential field of the step it adds", async () => {
  renderAt()
  fireEvent.click(within(await screen.findByRole("region", { name: "Then" })).getByRole("button", { name: "Add step" }))
  fireEvent.click(within(screen.getByRole("dialog", { name: "Add a step" })).getByRole("button", { name: /Run an agent/ }))
  await waitFor(() => expect(document.activeElement?.getAttribute("aria-label")).toBe("prompt of step run-an-agent"))
})

it("closes the picker with Escape, adding nothing", async () => {
  renderAt()
  fireEvent.click(await screen.findByRole("button", { name: "Add a step before Run" }))
  fireEvent.keyDown(screen.getByRole("searchbox", { name: "Search step types" }), { key: "Escape" })
  expect(screen.queryByRole("dialog", { name: "Add a step" })).toBeNull()
  expect(await yamlOf()).toBe(fileText)
})
