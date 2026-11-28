import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { MemoryRouter, useLocation } from "react-router-dom"
import { RunWorkflowDialog } from "../RunWorkflowDialog"
import type { WorkflowDefinition } from "../../lib/workflow"

const mockFetch = vi.fn()
vi.stubGlobal("fetch", mockFetch)
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))

afterEach(() => { cleanup(); vi.resetAllMocks() })

function LocationPath() {
  const location = useLocation()
  return <output aria-label="location">{location.pathname}{location.search}</output>
}

const definition: WorkflowDefinition = {
  schema_version: 1, name: "triage", title: "Triage", source: "triage.yaml",
  inputs: { subject: { type: "task", required: true }, count: { type: "integer", required: true } },
  steps: [],
}

describe("RunWorkflowDialog", () => {
  it("keeps initial task inputs read-only and submits them under their declared key", async () => {
    mockFetch.mockResolvedValue({ ok: true, json: async () => ({ run: { id: "wr-1" } }) })
    render(<MemoryRouter><RunWorkflowDialog definition={definition} repositories={[]} initialValues={{ subject: "TASK-1" }} readOnlyInputs={["subject"]} onClose={() => {}} /></MemoryRouter>)
    const task = screen.getByLabelText("subject") as HTMLInputElement
    expect(task.readOnly).toBe(true)
    expect(task.value).toBe("TASK-1")
    fireEvent.change(screen.getByLabelText("count"), { target: { value: "3" } })
    fireEvent.click(screen.getByRole("button", { name: "Start" }))
    await waitFor(() => expect(mockFetch).toHaveBeenCalled())
    expect(JSON.parse(mockFetch.mock.calls[0][1].body)).toEqual({ inputs: { subject: "TASK-1", count: "3" } })
  })

  it("submits displayed choice and boolean defaults without requiring edits", async () => {
    mockFetch.mockResolvedValue({ ok: true, json: async () => ({ run: { id: "wr-1" } }) })
    const withDefaults = { ...definition, inputs: { subject: definition.inputs!.subject, count: { type: "integer" as const, default: 2 }, mode: { type: "string" as const, choices: ["fast", "safe"] }, notify: { type: "boolean" as const } } }
    render(<MemoryRouter><RunWorkflowDialog definition={withDefaults} repositories={[]} initialValues={{ subject: "TASK-1" }} readOnlyInputs={["subject"]} onClose={() => {}} /></MemoryRouter>)
    fireEvent.click(screen.getByRole("button", { name: "Start" }))
    await waitFor(() => expect(mockFetch).toHaveBeenCalled())
    expect(JSON.parse(mockFetch.mock.calls[0][1].body).inputs).toEqual({ subject: "TASK-1", count: "2", mode: "fast", notify: "false" })
  })

  it("supports custom fields for a typed repository input", async () => {
    mockFetch.mockResolvedValue({ ok: true, json: async () => ({ run: { id: "wr-1" } }) })
    const repositoryDefinition = { ...definition, inputs: { subject: definition.inputs!.subject, repository: { type: "repository" as const, required: true } } }
    render(<MemoryRouter><RunWorkflowDialog definition={repositoryDefinition} repositories={[{ name: "Legato", repo: "cpave3/legato", primary_dir: "/projects/legato", base_branch: "main" }]} initialValues={{ subject: "TASK-1" }} readOnlyInputs={["subject"]} onClose={() => {}} /></MemoryRouter>)
    fireEvent.change(screen.getByLabelText("repository"), { target: { value: "__custom" } })
    fireEvent.change(screen.getByLabelText("repository repository"), { target: { value: "owner/custom" } })
    fireEvent.change(screen.getByLabelText("repository primary directory"), { target: { value: "/projects/custom" } })
    fireEvent.change(screen.getByLabelText("repository base branch"), { target: { value: "develop" } })
    fireEvent.click(screen.getByRole("button", { name: "Start" }))
    await waitFor(() => expect(mockFetch).toHaveBeenCalled())
    const submitted = JSON.parse(mockFetch.mock.calls[0][1].body).inputs.repository
    expect(JSON.parse(submitted)).toEqual({ repo: "owner/custom", primary_dir: "/projects/custom", base_branch: "develop" })
  })

  it("opens the selected task agent after starting when configured", async () => {
    mockFetch.mockResolvedValue({ ok: true, json: async () => ({ run: { id: "wr-1" } }) })
    const agentDefinition = { ...definition, after_start: "agent" as const, inputs: { subject: definition.inputs!.subject } }
    render(<MemoryRouter><RunWorkflowDialog definition={agentDefinition} repositories={[]} initialValues={{ subject: "TASK-1" }} readOnlyInputs={["subject"]} onClose={() => {}} /><LocationPath /></MemoryRouter>)
    fireEvent.click(screen.getByRole("button", { name: "Start" }))
    await waitFor(() => expect(screen.getByLabelText("location").textContent).toBe("/agents?task=TASK-1"))
  })

  it("closes without navigating after starting when configured to stay", async () => {
    mockFetch.mockResolvedValue({ ok: true, json: async () => ({ run: { id: "wr-1" } }) })
    const onClose = vi.fn()
    const stayDefinition = { ...definition, after_start: "stay" as const, inputs: { subject: definition.inputs!.subject } }
    render(<MemoryRouter initialEntries={["/board"]}><RunWorkflowDialog definition={stayDefinition} repositories={[]} initialValues={{ subject: "TASK-1" }} readOnlyInputs={["subject"]} onClose={onClose} /><LocationPath /></MemoryRouter>)
    fireEvent.click(screen.getByRole("button", { name: "Start" }))
    await waitFor(() => expect(onClose).toHaveBeenCalled())
    expect(screen.getByLabelText("location").textContent).toBe("/board")
  })

  it("asks for a required input before starting", () => {
    render(<MemoryRouter><RunWorkflowDialog definition={definition} repositories={[]} initialValues={{ subject: "TASK-1" }} readOnlyInputs={["subject"]} onClose={() => {}} /></MemoryRouter>)
    fireEvent.submit(screen.getByRole("dialog").querySelector("form")!)
    expect(screen.getByRole("alert").textContent).toBe("count is required")
    expect(mockFetch).not.toHaveBeenCalled()
  })

  it("opens the run it started, saying it is starting meanwhile", async () => {
    let finish: (value: unknown) => void = () => {}
    mockFetch.mockReturnValue(new Promise(resolve => { finish = resolve }))
    render(<MemoryRouter><RunWorkflowDialog definition={definition} repositories={[]} initialValues={{ subject: "TASK-1", count: "1" }} readOnlyInputs={["subject"]} onClose={() => {}} /><LocationPath /></MemoryRouter>)
    fireEvent.click(screen.getByRole("button", { name: "Start" }))
    expect((screen.getByRole("button", { name: "Starting…" }) as HTMLButtonElement).disabled).toBe(true)
    finish({ ok: true, json: async () => ({ run: { id: "wr-9" } }) })
    await waitFor(() => expect(screen.getByLabelText("location").textContent).toBe("/workflows/runs/wr-9"))
  })

  it("says why a start was refused, with its link", async () => {
    const refused = (code: string) => ({ ok: true, json: async () => ({ run: { id: "wr-1", status: "failed", error: "run error" }, steps: [{ status: "failed", error: "PR #4 is held", error_link: "/workflows/runs/wr-0", error_code: code }] }) })
    mockFetch.mockResolvedValue(refused("review_loop_active"))
    const onClose = vi.fn()
    render(<MemoryRouter><RunWorkflowDialog definition={definition} repositories={[]} initialValues={{ subject: "TASK-1", count: "1" }} readOnlyInputs={["subject"]} onClose={onClose} /></MemoryRouter>)
    fireEvent.click(screen.getByRole("button", { name: "Start" }))
    const link = await screen.findByRole("link", { name: "Open the review loop" })
    expect(screen.getByRole("alert").textContent).toBe("PR #4 is held")
    expect(link.getAttribute("href")).toBe("/workflows/runs/wr-0")
    expect((screen.getByRole("button", { name: "Start" }) as HTMLButtonElement).disabled).toBe(false)
    fireEvent.click(link)
    expect(onClose).toHaveBeenCalled()
    cleanup()
    mockFetch.mockResolvedValue(refused("other"))
    render(<MemoryRouter><RunWorkflowDialog definition={definition} repositories={[]} initialValues={{ subject: "TASK-1", count: "1" }} readOnlyInputs={["subject"]} onClose={() => {}} /></MemoryRouter>)
    fireEvent.click(screen.getByRole("button", { name: "Start" }))
    expect(await screen.findByRole("link", { name: "Open" })).toBeTruthy()
  })

  it("leaves hidden inputs out, and draws secrets, descriptions, choices and required marks", async () => {
    mockFetch.mockResolvedValue({ ok: true, json: async () => ({ run: { id: "wr-1" } }) })
    const rich = { ...definition, inputs: { token: { type: "string" as const, secret: true, description: "An API token" }, size: { type: "string" as const, choices: ["s", "m"], required: true }, skip: { type: "string" as const, default: "x" } } }
    render(<MemoryRouter><RunWorkflowDialog definition={rich} repositories={[]} hiddenInputs={["skip"]} onClose={() => {}} /></MemoryRouter>)
    expect(screen.queryByLabelText("skip")).toBeNull()
    expect((screen.getByLabelText("token") as HTMLInputElement).type).toBe("password")
    expect(screen.getByText("An API token")).toBeTruthy()
    expect(screen.getByText("size *")).toBeTruthy()
    fireEvent.change(screen.getByLabelText("size"), { target: { value: "m" } })
    fireEvent.change(screen.getByLabelText("token"), { target: { value: "t" } })
    fireEvent.click(screen.getByRole("button", { name: "Start" }))
    await waitFor(() => expect(mockFetch).toHaveBeenCalled())
    expect(JSON.parse(mockFetch.mock.calls[0][1].body).inputs).toEqual({ token: "t", size: "m" })
  })

  it("fills the repository inputs from a preset, and takes a preset for a typed repository input", async () => {
    mockFetch.mockResolvedValue({ ok: true, json: async () => ({ run: { id: "wr-1" } }) })
    const presets = [{ name: "Legato", repo: "cpave3/legato", primary_dir: "/p/legato" }]
    const withRepo = { ...definition, inputs: { repo: { type: "string" as const }, primary_dir: { type: "string" as const }, base_branch: { type: "string" as const }, where: { type: "repository" as const } } }
    render(<MemoryRouter><RunWorkflowDialog definition={withRepo} repositories={presets} onClose={() => {}} /></MemoryRouter>)
    fireEvent.change(screen.getByLabelText("Repository preset"), { target: { value: "Legato" } })
    expect((screen.getByLabelText("repo") as HTMLInputElement).value).toBe("cpave3/legato")
    expect((screen.getByLabelText("primary_dir") as HTMLInputElement).value).toBe("/p/legato")
    expect((screen.getByLabelText("base_branch") as HTMLInputElement).value).toBe("")
    fireEvent.change(screen.getByLabelText("Repository preset"), { target: { value: "" } })
    fireEvent.change(screen.getByLabelText("where"), { target: { value: "Legato" } })
    fireEvent.click(screen.getByRole("button", { name: "Start" }))
    await waitFor(() => expect(mockFetch).toHaveBeenCalled())
    expect(JSON.parse(mockFetch.mock.calls[0][1].body).inputs).toEqual({ repo: "cpave3/legato", primary_dir: "/p/legato", base_branch: "", where: "Legato" })
  })

  it("offers no repository preset without presets or repository inputs", () => {
    render(<MemoryRouter><RunWorkflowDialog definition={definition} repositories={[{ name: "L", repo: "a/b", primary_dir: "/x" }]} readOnlyInputs={["subject"]} onClose={() => {}} /></MemoryRouter>)
    expect(screen.queryByLabelText("Repository preset")).toBeNull()
  })

  it("keeps API errors visible for correction", async () => {
    mockFetch.mockResolvedValue({ ok: false, text: async () => "invalid count" })
    render(<MemoryRouter><RunWorkflowDialog definition={definition} repositories={[]} initialValues={{ subject: "TASK-1" }} readOnlyInputs={["subject"]} onClose={() => {}} /></MemoryRouter>)
    fireEvent.change(screen.getByLabelText("count"), { target: { value: "3" } })
    fireEvent.submit(screen.getByRole("dialog").querySelector("form")!)
    await waitFor(() => expect(screen.getByText(/invalid count/)).toBeTruthy())
    expect(screen.getByRole("dialog")).toBeTruthy()
  })
})
