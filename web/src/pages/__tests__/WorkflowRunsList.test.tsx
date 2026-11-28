import { fireEvent, render, screen, within } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { WorkflowsPage } from "../WorkflowsPage"
import type { WorkflowRun } from "../../lib/workflow"

const { useWorkflows } = vi.hoisted(() => ({ useWorkflows: vi.fn(() => ({ data: { definitions: [], errors: [] }, refresh: vi.fn() })) }))
const base = { definition_source: "test", definition_digest: "d", inputs_json: "{}", created_at: "2026-07-28T10:00:00Z" }
// As the API returns them: newest first.
const runs: WorkflowRun[] = [
  { ...base, id: "wr-run", workflow_name: "plan-task", workflow_title: "Plan jgek1vt3", related_task_title: "Sync retries hammer Jira", status: "running", current_step_id: "plan", steps: [{ step_id: "read", title: "Read the task", status: "succeeded" }, { step_id: "plan", title: "Write the plan", status: "waiting_agent" }, { step_id: "note", title: "Attach the plan", status: "pending" }] },
  { ...base, id: "wr-fail", workflow_name: "ship-task", workflow_title: "Ship wtwywswa", status: "failed", current_step_id: "move", error: "column \"Shipped\" not found", steps: [{ step_id: "move", title: "Move to Shipped", status: "failed" }] },
  { ...base, id: "wr-done", workflow_name: "plan-task", workflow_title: "Plan xk2pb7do", related_task_title: "Add keyboard help", status: "succeeded", current_step_id: "", started_at: "2026-07-28T09:00:00Z", completed_at: "2026-07-28T09:04:12Z", created_at: "2026-07-28T09:00:00Z", steps: [{ step_id: "read", title: "Read the task", status: "succeeded" }, { step_id: "plan", title: "Write the plan", status: "succeeded" }] },
  { ...base, id: "wr-gate", workflow_name: "triage-task", workflow_title: "Triage gpri1ycr", related_task_title: "Board search ignores workspace filter", status: "waiting", current_step_id: "approve", needs_attention: true, attention_reason: "manual action required: Move to Ready", created_at: "2026-07-28T08:00:00Z", steps: [{ step_id: "read", title: "Read the task", status: "succeeded" }, { step_id: "approve", title: "Move to Ready", status: "waiting_manual", trigger: "manual" }] },
]
vi.mock("../../hooks/useWorkflow", () => ({ useWorkflows, useWorkflowMap: () => ({ data: null, loading: false, error: null, refresh: vi.fn() }), useWorkflowStats: () => ({ data: null, refresh: vi.fn() }), useWorkflowRuns: () => ({ data: runs, refresh: vi.fn() }) }))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))

describe("workflow runs", () => {
  beforeEach(() => { try { localStorage.clear() } catch { /* no storage */ } })

  it("opens on the runs, without loading the definitions, and counts those that need the operator", () => {
    render(<MemoryRouter><WorkflowsPage /></MemoryRouter>)
    expect(screen.getByRole("tab", { name: /Runs/ }).getAttribute("aria-selected")).toBe("true")
    expect(within(screen.getByRole("tab", { name: /Runs/ })).getByLabelText("1 need you")).toBeTruthy()
    expect(useWorkflows).not.toHaveBeenCalled()
  })

  it("puts the runs that need the operator first, then those in progress, saying where each is", () => {
    render(<MemoryRouter><WorkflowsPage /></MemoryRouter>)
    const needs = within(screen.getByRole("list", { name: "Runs that need you" })).getAllByRole("listitem")
    expect(needs).toHaveLength(1)
    expect(needs[0].textContent).toContain("Triage gpri1ycr")
    expect(needs[0].textContent).toContain("Needs you")
    expect(needs[0].textContent).toContain("Board search ignores workspace filter")
    expect(needs[0].textContent).toContain("“Move to Ready” waits for you to run it")
    expect(needs[0].textContent).toContain("2 of 2")
    const active = within(screen.getByRole("list", { name: "Runs in progress" })).getAllByRole("listitem")
    expect(active).toHaveLength(1)
    expect(active[0].textContent).toContain("Running")
    expect(active[0].textContent).toContain("An agent is working on “Write the plan”")
    expect(active[0].textContent).toContain("2 of 3")
    expect(within(active[0]).getByRole("link").getAttribute("href")).toBe("/workflows/runs/wr-run")
  })

  it("keeps a suspended run in progress, saying it is suspended and not that it needs the operator", () => {
    runs.push({ ...base, id: "wr-sus", workflow_name: "ship-task", workflow_title: "Ship LEG-559", status: "suspended", current_step_id: "build", error: "Legato stopped while the agent was working", created_at: "2026-07-28T07:00:00Z", steps: [{ step_id: "build", title: "Build the stack", status: "suspended" }] })
    try {
      render(<MemoryRouter><WorkflowsPage /></MemoryRouter>)
      const active = within(screen.getByRole("list", { name: "Runs in progress" })).getAllByRole("listitem")
      const suspended = active.find(row => row.textContent?.includes("Ship LEG-559"))
      expect(suspended?.textContent).toContain("Suspended")
      expect(suspended?.textContent).toContain("“Build the stack” is suspended")
      expect(within(screen.getByRole("tab", { name: /Runs/ })).getByLabelText("1 need you")).toBeTruthy()
    } finally {
      runs.pop()
    }
  })

  it("lists finished runs under the day they started, with how they ended and how long they ran", () => {
    render(<MemoryRouter><WorkflowsPage /></MemoryRouter>)
    expect(screen.getByRole("heading", { name: /Tue 28 Jul/ })).toBeTruthy()
    const finished = within(screen.getByRole("list", { name: "Finished runs, Tue 28 Jul" })).getAllByRole("listitem")
    expect(finished[0].textContent).toContain("Failed")
    expect(finished[0].textContent).toContain("Move to Shipped: column \"Shipped\" not found")
    expect(finished[1].textContent).toContain("Done")
    expect(finished[1].textContent).toContain("4m 12s")
    expect(finished[1].textContent).not.toContain("Took")
    expect(finished[1].textContent).toContain("2 steps")
  })

  it("calls today's runs Today", () => {
    runs.push({ ...base, id: "wr-now", workflow_name: "plan-task", workflow_title: "Plan now", status: "succeeded", current_step_id: "", created_at: new Date().toISOString(), steps: [] })
    try {
      render(<MemoryRouter><WorkflowsPage /></MemoryRouter>)
      expect(within(screen.getByRole("list", { name: "Finished runs, Today" })).getAllByRole("listitem")[0].textContent).toContain("Plan now")
    } finally {
      runs.pop()
    }
  })

  it("filters the runs by how they stand and by workflow", () => {
    render(<MemoryRouter><WorkflowsPage /></MemoryRouter>)
    const chips = screen.getByRole("group", { name: "Status" })
    expect(within(chips).getByRole("button", { name: "All 4" }).getAttribute("aria-pressed")).toBe("true")
    fireEvent.click(within(chips).getByRole("button", { name: "Failed 1" }))
    expect(screen.getAllByRole("listitem").map(row => row.textContent)).toEqual([expect.stringContaining("Ship wtwywswa")])
    fireEvent.click(within(chips).getByRole("button", { name: "All 4" }))
    fireEvent.change(screen.getByRole("combobox", { name: "Workflow" }), { target: { value: "plan-task" } })
    expect(screen.getAllByRole("listitem").map(row => row.textContent)).toEqual([expect.stringContaining("Plan jgek1vt3"), expect.stringContaining("Plan xk2pb7do")])
  })

  it("puts a child run under its parent, folded", () => {
    runs.push({ ...base, id: "wr-child", workflow_name: "review-loop", workflow_title: "Review loop on cpave3/legato", status: "failed", current_step_id: "pass", parent_run_id: "wr-run", created_at: "2026-07-28T10:01:00Z", steps: [] })
    try {
      render(<MemoryRouter><WorkflowsPage /></MemoryRouter>)
      expect(screen.queryByText("Review loop on cpave3/legato")).toBeNull()
      const parent = within(screen.getByRole("list", { name: "Runs in progress" })).getAllByRole("listitem")[0]
      fireEvent.click(within(parent).getByRole("button", { name: "1 run inside · 1 failed" }))
      expect(within(parent).getByRole("list", { name: "Runs inside Plan jgek1vt3" }).textContent).toContain("Review loop on cpave3/legato")
    } finally {
      runs.pop()
    }
  })

  it("opens a child's own children, at every level", () => {
    runs.push(
      { ...base, id: "wr-b", workflow_name: "inner", workflow_title: "Inner run", status: "succeeded", current_step_id: "", parent_run_id: "wr-done", created_at: "2026-07-28T09:01:00Z", steps: [] },
      { ...base, id: "wr-c", workflow_name: "innermost", workflow_title: "Innermost run", status: "succeeded", current_step_id: "", parent_run_id: "wr-b", created_at: "2026-07-28T09:02:00Z", steps: [] },
    )
    try {
      render(<MemoryRouter><WorkflowsPage /></MemoryRouter>)
      const parent = within(screen.getByRole("list", { name: /Finished runs/ })).getAllByRole("listitem").find(row => row.textContent?.includes("Plan xk2pb7do"))!
      fireEvent.click(within(parent).getByRole("button", { name: "1 run inside" }))
      const inner = within(within(parent).getByRole("list", { name: "Runs inside Plan xk2pb7do" })).getAllByRole("listitem")[0]
      fireEvent.click(within(inner).getByRole("button", { name: "1 run inside" }))
      expect(within(inner).getByRole("list", { name: "Runs inside Inner run" }).textContent).toContain("Innermost run")
    } finally {
      runs.splice(-2)
    }
  })

  it("counts and finds a child run that needs the operator, under its parent", () => {
    runs.push({ ...base, id: "wr-gate-child", workflow_name: "ask-child", workflow_title: "Child at a gate", status: "waiting", current_step_id: "ask", needs_attention: true, attention_reason: "manual action required: Ask", parent_run_id: "wr-run", created_at: "2026-07-28T10:02:00Z", steps: [{ step_id: "ask", title: "Ask", status: "waiting_manual", trigger: "manual" }] })
    try {
      render(<MemoryRouter><WorkflowsPage /></MemoryRouter>)
      const chips = screen.getByRole("group", { name: "Status" })
      fireEvent.click(within(chips).getByRole("button", { name: "Needs you 2" }))
      const needs = within(screen.getByRole("list", { name: "Runs that need you" })).getAllByRole("listitem")
      const parent = needs.find(row => row.textContent?.includes("Plan jgek1vt3"))!
      expect(within(parent).getByRole("list", { name: "Runs inside Plan jgek1vt3" }).textContent).toContain("Child at a gate")
      fireEvent.click(within(chips).getByRole("button", { name: /^All/ }))
      expect(within(screen.getByRole("combobox", { name: "Workflow" })).getByRole("option", { name: "ask-child" })).toBeTruthy()
    } finally {
      runs.pop()
    }
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
