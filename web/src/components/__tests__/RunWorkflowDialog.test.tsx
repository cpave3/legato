import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { MemoryRouter } from "react-router-dom"
import { RunWorkflowDialog } from "../RunWorkflowDialog"
import type { WorkflowDefinition } from "../../lib/workflow"

const mockFetch = vi.fn()
vi.stubGlobal("fetch", mockFetch)
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))

afterEach(() => { cleanup(); vi.resetAllMocks() })

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

  it("keeps API errors visible for correction", async () => {
    mockFetch.mockResolvedValue({ ok: false, text: async () => "invalid count" })
    render(<MemoryRouter><RunWorkflowDialog definition={definition} repositories={[]} initialValues={{ subject: "TASK-1" }} readOnlyInputs={["subject"]} onClose={() => {}} /></MemoryRouter>)
    fireEvent.change(screen.getByLabelText("count"), { target: { value: "3" } })
    fireEvent.submit(screen.getByRole("dialog").querySelector("form")!)
    await waitFor(() => expect(screen.getByText(/invalid count/)).toBeTruthy())
    expect(screen.getByRole("dialog")).toBeTruthy()
  })
})
