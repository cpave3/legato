import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { MemoryRouter, Route, Routes } from "react-router-dom"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { parse } from "yaml"
import { WorkflowEditPage } from "../WorkflowEditPage"

// The inspector's Setup and Outputs of a step (LEG-508).

const { apiFetch } = vi.hoisted(() => ({ apiFetch: vi.fn() }))
vi.mock("../../lib/api", () => ({ apiFetch }))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../hooks/useMediaQuery", () => ({ useMediaQuery: () => false }))
vi.mock("../../hooks/useWorkflow", () => ({ useWorkflows: () => ({ data: { definitions: [], errors: null, folders: [] }, refresh: vi.fn() }) }))
vi.mock("../../components/workflow-editor/LazyWorkflowYamlEditor", () => ({
  LazyWorkflowYamlEditor: ({ value, onChange, label = "Workflow YAML" }: { value: string; onChange: (v: string) => void; label?: string }) =>
    <textarea aria-label={label} value={value} onChange={event => onChange(event.target.value)} />,
}))
vi.mock("../../components/workflow-graph/LazyWorkflowGraph", () => ({ LazyWorkflowGraph: () => null }))

const v1 = `schema_version: 1
name: review-pr
title: Review
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
      prompt: Look at it
`
const json = (body: unknown, status = 200) => Promise.resolve(new Response(JSON.stringify(body), { status }))
let fileYaml = v1

beforeEach(() => {
  localStorage.clear()
  localStorage.setItem("legato:workflow-editor-mode", "visual")
  fileYaml = v1
  apiFetch.mockReset()
  apiFetch.mockImplementation((_: string, path: string) => {
    if (path === "/api/workflow-files/check") return json({ definition: { name: "review-pr" }, graph: null, diagnostics: [] })
    if (path === "/api/workflows/review-pr/file") return json({ name: "review-pr", yaml: fileYaml, path: "/g/review-pr.yaml", folder: "global", digest: "sha256:old", editable: true })
    if (path === "/api/adapters") return json({ adapters: ["claude-code", "codex"], default: "claude-code" })
    if (path === "/api/repositories") return json({ repositories: [{ name: "app", repo: "acme/app", primary_dir: "/src/app", configured: true }] })
    return json({}, 404)
  })
})
afterEach(cleanup)

async function renderAt(hash: string) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(<QueryClientProvider client={client}><MemoryRouter initialEntries={[`/workflows/review-pr/edit${hash}`]}>
    <Routes><Route path="/workflows/:name/edit" element={<WorkflowEditPage mode="edit" />} /></Routes>
  </MemoryRouter></QueryClientProvider>)
  await screen.findByRole("region", { name: "Inspector" })
}

const inspector = () => screen.getByRole("region", { name: "Inspector" })
const fileNow = async () => {
  fireEvent.click(screen.getByRole("radio", { name: "YAML" }))
  const value = (await screen.findByRole("textbox", { name: "Workflow YAML" }) as HTMLTextAreaElement).value
  fireEvent.click(screen.getByRole("radio", { name: "Visual" }))
  return value
}
const stepWith = async (id: string) => parse(await fileNow()).steps.find((s: { id: string }) => s.id === id)

it("shows the essential fields first, under their labels, and the rest folded under More options", async () => {
  await renderAt("#steps/run")
  await screen.findByRole("textbox", { name: "Step title" })
  const more = within(inspector()).getByText(/^More options \(\d+\)$/).closest("details")!
  expect(more.open).toBe(false)
  const essential = [...inspector().querySelectorAll("[data-essential]")].filter(el => !more.contains(el)).map(el => el.querySelector("span")?.textContent)
  expect(essential).toEqual([expect.stringMatching(/^What it should do/), expect.stringMatching(/^Works in/), expect.stringMatching(/^Or a clone of/)])
  expect(within(more).getByLabelText("keep_agent of step run")).toBeTruthy()
})

it("says in More options' header what of it is set", async () => {
  fileYaml = v1.replace("      prompt: Look at it\n", "      prompt: Look at it\n      keep_agent: true\n      branch: fix\n")
  await renderAt("#steps/run")
  await screen.findByRole("textbox", { name: "Step title" })
  expect(within(inspector()).getByText("Branch to work on, Keep the agent running")).toBeTruthy()
})

it("chooses Prompt or Macro as one segmented choice", async () => {
  await renderAt("#steps/run")
  fireEvent.click(await within(inspector()).findByRole("radio", { name: "Macro" }))
  expect((await stepWith("run")).with).toEqual({ macro: "" })
})

it("chooses the agent tool from this Legato's", async () => {
  await renderAt("#steps/run")
  const tools = await within(inspector()).findByRole("radiogroup", { name: "agent of step run" })
  expect(within(tools).getAllByRole("radio").map(r => r.textContent)).toEqual(["Default", "claude-code", "codex"])
  fireEvent.click(within(tools).getByRole("radio", { name: "codex" }))
  expect((await stepWith("run")).with.agent).toBe("codex")
})

it("turns a boolean on and off with a sentence switch", async () => {
  await renderAt("#steps/run")
  const toggle = await within(inspector()).findByRole("switch", { name: "keep_agent of step run" })
  expect(toggle.textContent).toBe("Keep the agent running")
  fireEvent.click(toggle)
  expect((await stepWith("run")).with.keep_agent).toBe(true)
  fireEvent.click(within(inspector()).getByRole("switch", { name: "keep_agent of step run" }))
  expect((await stepWith("run")).with.keep_agent).toBeUndefined()
})

it("suggests this Legato's repositories", async () => {
  await renderAt("#steps/prep")
  const repo = await within(inspector()).findByRole("combobox", { name: "repo of step prep" })
  await waitFor(() => expect([...document.querySelectorAll(`#${repo.getAttribute("list")} option`)].map(o => o.getAttribute("value"))).toEqual(["acme/app"]))
})

it("writes long text in a larger editor", async () => {
  await renderAt("#steps/run")
  fireEvent.click(await within(inspector()).findByRole("button", { name: "Write What it should do in a larger editor" }))
  const editor = screen.getByRole("dialog", { name: "What it should do" })
  fireEvent.change(within(editor).getByRole("textbox"), { target: { value: "Look at it\nthen say so" } })
  fireEvent.click(within(editor).getByRole("button", { name: "Done" }))
  expect((await stepWith("run")).with.prompt).toBe("Look at it\nthen say so")
})

it("sets how a step starts, as trigger: in a v1 file and start: in v2", async () => {
  await renderAt("#steps/run")
  fireEvent.click(await within(inspector()).findByRole("radio", { name: "You start it" }))
  expect((await stepWith("run")).trigger).toBe("manual")
  fireEvent.click(within(inspector()).getByRole("radio", { name: "Right after" }))
  expect((await stepWith("run")).trigger).toBeUndefined()
  cleanup()
  fileYaml = v1.replace("schema_version: 1", "schema_version: 2")
  await renderAt("#steps/run")
  fireEvent.click(await within(inspector()).findByRole("radio", { name: "When the agent ends" }))
  expect((await stepWith("run")).start).toBe("agent_ended")
})

it("keeps the step's ID in a footer, saying it is used in references", async () => {
  await renderAt("#steps/run")
  const id = await within(inspector()).findByRole("textbox", { name: "ID of step run" })
  expect(id.closest("footer")?.textContent).toContain("Used in references")
})

it("lists what later steps can read on Outputs: the type's own, then those declared", async () => {
  await renderAt("#steps/prep")
  fireEvent.click(await within(inspector()).findByRole("tab", { name: "Outputs" }))
  const gives = within(inspector()).getByRole("region", { name: "What prep gives" })
  expect(within(gives).getByText("steps.prep.outputs.working_dir")).toBeTruthy()
  expect(within(inspector()).getByRole("textbox", { name: "New output of step prep" })).toBeTruthy()
})
