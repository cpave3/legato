import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { afterEach, expect, it, vi } from "vitest"
import { WorkflowsPage } from "../WorkflowsPage"

const { apiFetch, refresh } = vi.hoisted(() => ({ apiFetch: vi.fn(), refresh: vi.fn() }))
vi.mock("../../lib/api", () => ({ apiFetch }))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../hooks/useWorkflow", () => ({
  useWorkflows: () => ({ data: {
    definitions: [{ schema_version: 1, name: "triage", title: "Triage", source: "triage.yaml", inputs: { task: { type: "task", required: true } }, on: { "task.entered_column": { column: "Triage", enabled: false } }, steps: [] }],
    errors: [],
    trigger_states: { triage: [{ trigger: "task.entered_column", file_enabled: false, enabled: false, overridden: false }] },
  }, refresh }),
  useWorkflowMap: () => ({ data: null, loading: false, error: null, refresh: vi.fn() }), useWorkflowStats: () => ({ data: null, refresh: vi.fn() }), useWorkflowRuns: () => ({ data: [], refresh: vi.fn() }),
}))
afterEach(cleanup)

it("switches a trigger on here, from an opened row, without touching its file", async () => {
  apiFetch.mockResolvedValue(new Response(JSON.stringify({ status: "ok" })))
  render(<MemoryRouter><WorkflowsPage /></MemoryRouter>)
  fireEvent.click(screen.getByRole("tab", { name: "Workflows" }))
  fireEvent.click(screen.getByRole("button", { name: "Details of Triage" }))
  const toggle = screen.getByRole("switch", { name: "When a task enters Triage" })
  expect(toggle.getAttribute("aria-checked")).toBe("false")
  expect(toggle.textContent).toContain("paused here")
  fireEvent.click(toggle)
  await waitFor(() => expect(apiFetch).toHaveBeenCalled())
  const [, path, init] = apiFetch.mock.calls[0]
  expect(path).toBe("/api/workflows/triage/triggers/task.entered_column")
  expect(init.method).toBe("PUT")
  expect(JSON.parse(init.body)).toEqual({ enabled: true })
  await waitFor(() => expect(refresh).toHaveBeenCalled())
})

it("switches a workflow's only trigger from its row", async () => {
  apiFetch.mockResolvedValue(new Response(JSON.stringify({ status: "ok" })))
  render(<MemoryRouter><WorkflowsPage /></MemoryRouter>)
  fireEvent.click(screen.getByRole("tab", { name: "Workflows" }))
  fireEvent.click(screen.getByRole("button", { name: "Triggers of Triage: paused" }))
  await waitFor(() => expect(apiFetch).toHaveBeenCalled())
  const [, path, init] = apiFetch.mock.calls.at(-1)!
  expect(path).toBe("/api/workflows/triage/triggers/task.entered_column")
  expect(JSON.parse(init.body)).toEqual({ enabled: true })
})
