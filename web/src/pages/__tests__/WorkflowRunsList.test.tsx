import { fireEvent, render, screen, within } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { WorkflowsPage } from "../WorkflowsPage"

const { useWorkflows } = vi.hoisted(() => ({ useWorkflows: vi.fn(() => ({ data: { definitions: [], errors: [] }, refresh: vi.fn() })) }))
const base = { definition_source: "test", definition_digest: "d", inputs_json: "{}", created_at: "2026-07-28T10:00:00Z" }
// As the API returns them: newest first.
const runs = [
  { ...base, id: "wr-run", workflow_name: "plan-task", workflow_title: "Plan jgek1vt3", related_task_title: "Sync retries hammer Jira", status: "running", current_step_id: "plan", steps: [{ step_id: "read", title: "Read the task", status: "succeeded" }, { step_id: "plan", title: "Write the plan", status: "waiting_agent" }, { step_id: "note", title: "Attach the plan", status: "pending" }] },
  { ...base, id: "wr-fail", workflow_name: "ship-task", workflow_title: "Ship wtwywswa", status: "failed", current_step_id: "move", error: "column \"Shipped\" not found", steps: [{ step_id: "move", title: "Move to Shipped", status: "failed" }] },
  { ...base, id: "wr-done", workflow_name: "plan-task", workflow_title: "Plan xk2pb7do", related_task_title: "Add keyboard help", status: "succeeded", current_step_id: "", started_at: "2026-07-28T09:00:00Z", completed_at: "2026-07-28T09:04:12Z", created_at: "2026-07-28T09:00:00Z", steps: [{ step_id: "read", title: "Read the task", status: "succeeded" }, { step_id: "plan", title: "Write the plan", status: "succeeded" }] },
  { ...base, id: "wr-gate", workflow_name: "triage-task", workflow_title: "Triage gpri1ycr", related_task_title: "Board search ignores workspace filter", status: "waiting", current_step_id: "approve", needs_attention: true, attention_reason: "manual action required: Move to Ready", created_at: "2026-07-28T08:00:00Z", steps: [{ step_id: "read", title: "Read the task", status: "succeeded" }, { step_id: "approve", title: "Move to Ready", status: "waiting_manual", trigger: "manual" }] },
]
vi.mock("../../hooks/useWorkflow", () => ({ useWorkflows, useWorkflowRuns: () => ({ data: runs, refresh: vi.fn() }) }))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))

describe("workflow runs", () => {
  beforeEach(() => { try { localStorage.clear() } catch { /* no storage */ } })

  it("opens on the runs, without loading the definitions, and counts those that need the operator", () => {
    render(<MemoryRouter><WorkflowsPage /></MemoryRouter>)
    expect(screen.getByRole("tab", { name: /Runs/ }).getAttribute("aria-selected")).toBe("true")
    expect(within(screen.getByRole("tab", { name: /Runs/ })).getByLabelText("1 need you")).toBeTruthy()
    expect(useWorkflows).not.toHaveBeenCalled()
  })

  it("puts runs in progress first, those that need the operator at the top, and says where each is", () => {
    render(<MemoryRouter><WorkflowsPage /></MemoryRouter>)
    const active = within(screen.getByRole("list", { name: "Runs in progress" })).getAllByRole("listitem")
    expect(active.map(row => row.textContent)).toEqual([expect.stringContaining("Triage gpri1ycr"), expect.stringContaining("Plan jgek1vt3")])
    expect(active[0].textContent).toContain("Needs you")
    expect(active[0].textContent).toContain("Board search ignores workspace filter")
    expect(active[0].textContent).toContain("“Move to Ready” waits for you to run it")
    expect(active[0].textContent).toContain("2 of 2")
    expect(active[1].textContent).toContain("Running")
    expect(active[1].textContent).toContain("An agent is working on “Write the plan”")
    expect(active[1].textContent).toContain("2 of 3")
    expect(within(active[1]).getByRole("link").getAttribute("href")).toBe("/workflows/runs/wr-run")
  })

  it("lists finished runs with how they ended", () => {
    render(<MemoryRouter><WorkflowsPage /></MemoryRouter>)
    const finished = within(screen.getByRole("list", { name: "Finished runs" })).getAllByRole("listitem")
    expect(finished[0].textContent).toContain("Failed")
    expect(finished[0].textContent).toContain("Move to Shipped: column \"Shipped\" not found")
    expect(finished[1].textContent).toContain("Done")
    expect(finished[1].textContent).toContain("Took 4m 12s")
    expect(finished[1].textContent).toContain("2 steps")
  })

  it("opens the tab a link names, else the one chosen last, and remembers a choice", () => {
    render(<MemoryRouter initialEntries={["/workflows?tab=workflows"]}><WorkflowsPage /></MemoryRouter>)
    expect(screen.getByRole("tab", { name: "Workflows" }).getAttribute("aria-selected")).toBe("true")
    fireEvent.click(screen.getByRole("tab", { name: /Runs/ }))
    expect(localStorage.getItem("legato:workflows-tab")).toBe("runs")
  })

  it("opens on the tab chosen last", () => {
    localStorage.setItem("legato:workflows-tab", "workflows")
    render(<MemoryRouter><WorkflowsPage /></MemoryRouter>)
    expect(screen.getByRole("tab", { name: "Workflows" }).getAttribute("aria-selected")).toBe("true")
  })
})
