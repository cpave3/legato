import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { afterEach, expect, it, vi } from "vitest"
import { WorkflowsPage } from "../WorkflowsPage"

vi.mock("../../lib/api", () => ({ apiFetch: vi.fn() }))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../hooks/useWorkflow", () => ({
  useWorkflows: () => ({ data: {
    definitions: [{ schema_version: 1, name: "standup", title: "Standup", source: "standup.yaml", on: { schedule: { every: "weekdays", at: "09:00" } }, steps: [] }],
    errors: [],
    trigger_states: { standup: [{ trigger: "schedule", file_enabled: true, enabled: true, overridden: false, max_runs_per_day: 3, schedule: {
      description: "Every weekday at 09:00", timezone: "Australia/Sydney",
      next: ["2026-11-23T09:00:00+11:00", "2026-11-24T09:00:00+11:00", "2026-11-25T09:00:00+11:00"],
      last: { due_time: "2026-11-20T09:00:00+11:00", status: "started", run_id: "wr-9" },
      catch_up: "once",
      missed: [{ due_time: "2026-11-19T09:00:00+11:00", status: "missed", error: "Legato was not running at 2026-11-19 09:00 AEDT (catch_up: once)" }],
    } }] },
  }, refresh: vi.fn() }),
  useWorkflowMap: () => ({ data: null, loading: false, error: null, refresh: vi.fn() }), useWorkflowStats: () => ({ data: null, refresh: vi.fn() }), useWorkflowRuns: () => ({ data: [], refresh: vi.fn() }),
}))
afterEach(cleanup)

it("shows a schedule's next runs and its last one", () => {
  render(<MemoryRouter><WorkflowsPage /></MemoryRouter>)
  fireEvent.click(screen.getByRole("tab", { name: "Workflows" }))
  fireEvent.click(screen.getByRole("button", { name: "Details of Standup" }))
  const row = within(screen.getByRole("list", { name: "Workflows" })).getAllByRole("listitem")[0]
  expect(within(row).getByRole("switch", { name: "Every weekday at 09:00" }).getAttribute("aria-checked")).toBe("true")
  const next = within(row).getByLabelText("Next runs of Standup")
  expect(next.textContent).toContain("2026-11-23 09:00")
  expect(next.textContent).toContain("2026-11-24 09:00")
  expect(within(row).getByRole("link", { name: /Last run 2026-11-20 09:00/ }).getAttribute("href")).toBe("/workflows/runs/wr-9")
  const missed = within(row).getByLabelText("Missed runs of Standup")
  expect(missed.textContent).toContain("2026-11-19 09:00")
  expect(missed.textContent).toContain("caught up once")
  expect(within(missed).getByTitle(/not running/)).toBeTruthy()
})
