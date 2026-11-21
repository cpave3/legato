import { useEffect, useReducer } from "react"
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { MemoryRouter, Route, Routes } from "react-router-dom"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { mockReactFlow } from "../../test/reactFlow"
import type { WorkflowDiscovery, WorkflowTriggerState } from "../../lib/workflow"
import { WorkflowPage } from "../WorkflowPage"

mockReactFlow()

const columnTrigger = { column: "Triage", enabled: false }
const scheduleTrigger = { every: "weekdays", at: "09:00", max_runs_per_day: 1 }
const definition = {
  schema_version: 1, name: "triage", title: "Triage a task", source: "/w/triage.workflow.yaml",
  on: { "task.entered_column": columnTrigger, schedule: scheduleTrigger },
  steps: [{ id: "investigate", title: "Investigate the task", type: "agent.run", trigger: "automatic", with: { prompt: "Look" } }],
  graph: { version: 1, nodes: [
    { id: "trigger:manual", kind: "trigger", trigger: { kind: "manual", summary: "Run by hand", enabled: true } },
    { id: "trigger:task.entered_column", kind: "trigger", trigger: { kind: "task.entered_column", summary: "Card enters Triage", enabled: false, max_runs_per_day: 3, definition: columnTrigger } },
    { id: "trigger:schedule", kind: "trigger", trigger: { kind: "schedule", summary: "Every weekday at 09:00", enabled: true, max_runs_per_day: 1, definition: scheduleTrigger } },
    { id: "step:investigate", kind: "step", step: { id: "investigate", title: "Investigate the task", type: "agent.run", trigger: "automatic", key: { field: "prompt", value: "Look" }, definition: {} } },
  ], edges: [
    { id: "start:trigger:manual>step:investigate", kind: "start", source: "trigger:manual", target: "step:investigate" },
    { id: "start:trigger:task.entered_column>step:investigate", kind: "start", source: "trigger:task.entered_column", target: "step:investigate" },
    { id: "start:trigger:schedule>step:investigate", kind: "start", source: "trigger:schedule", target: "step:investigate" },
  ] },
}

function states(columnOn: boolean): WorkflowTriggerState[] {
  return [
    { trigger: "task.entered_column", file_enabled: false, enabled: columnOn, overridden: columnOn, max_runs_per_day: 3 },
    { trigger: "schedule", file_enabled: true, enabled: true, overridden: false, max_runs_per_day: 1, schedule: { description: "Every weekday at 09:00", timezone: "Europe/London", next: ["2026-11-23T09:00:00Z", "2026-11-24T09:00:00Z"], missed: [{ due_time: "2026-11-20T09:00:00Z", status: "missed" }], catch_up: "once" } },
  ]
}

let discovery: WorkflowDiscovery
const listeners = new Set<() => void>()
const setTrigger = vi.fn(async () => {
  discovery = { ...discovery, trigger_states: { triage: states(true) } }
  return { status: "ok" }
})

vi.mock("../../lib/workflow", async importOriginal => ({ ...await importOriginal<typeof import("../../lib/workflow")>(), setWorkflowTrigger: (...args: unknown[]) => setTrigger(...(args as [])) }))
vi.mock("../../hooks/useWorkflow", () => ({
  useWorkflows: () => {
    const [, rerender] = useReducer((n: number) => n + 1, 0)
    useEffect(() => { listeners.add(rerender); return () => { listeners.delete(rerender) } }, [])
    return { data: discovery, loading: false, error: null, refresh: async () => { listeners.forEach(listener => listener()) } }
  },
  useWorkflowRuns: () => ({ data: [], loading: false, error: null, refresh: vi.fn() }),
}))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))

beforeEach(() => {
  setTrigger.mockClear()
  discovery = { definitions: [definition] as unknown as WorkflowDiscovery["definitions"], errors: [], trigger_states: { triage: states(false) } }
})

function renderPage() {
  return render(<MemoryRouter initialEntries={["/workflows/triage"]}><Routes><Route path="/workflows/:name" element={<WorkflowPage />} /></Routes></MemoryRouter>)
}

describe("trigger nodes on the workflow graph", () => {
  it("draws each trigger with its summary, whether it is on here, and its loop stop", async () => {
    renderPage()
    const column = await screen.findByRole("button", { name: "Trigger: Card enters Triage, paused here" })
    expect(column.textContent).toContain("Card enters Triage")
    expect(column.textContent).toContain("paused here")
    expect(column.textContent).toContain("3/day")
    const schedule = screen.getByRole("button", { name: "Trigger: Every weekday at 09:00, on here" })
    expect(schedule.textContent).toContain("1/day")
    const manual = screen.getByRole("button", { name: "Trigger: Run by hand" })
    expect(manual.textContent).not.toContain("/day")
  })

  it("opens a trigger's side panel, whose switch turns it on and updates its node", async () => {
    renderPage()
    fireEvent.click(await screen.findByRole("button", { name: "Trigger: Card enters Triage, paused here" }))
    const panel = screen.getByRole("complementary", { name: "Card enters Triage" })
    expect(panel.textContent).toContain("column: Triage")
    expect(panel.textContent).toContain("3 runs per item per day")
    fireEvent.click(within(panel).getByRole("switch"))
    await waitFor(() => expect(setTrigger).toHaveBeenCalledWith("", "triage", "task.entered_column", true))
    expect(await screen.findByRole("button", { name: "Trigger: Card enters Triage, on here" })).toBeTruthy()
  })

  it("shows a schedule's next due times and missed runs in its panel", async () => {
    renderPage()
    fireEvent.click(await screen.findByRole("button", { name: "Trigger: Every weekday at 09:00, on here" }))
    const panel = screen.getByRole("complementary", { name: "Every weekday at 09:00" })
    expect(within(panel).getByLabelText("Next runs of Triage a task").textContent).toContain("2026-11-23 09:00")
    expect(within(panel).getByLabelText("Missed runs of Triage a task").textContent).toContain("2026-11-20 09:00")
  })
})
