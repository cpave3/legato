import { fireEvent, render, screen, within } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { describe, expect, it, vi } from "vitest"
import { WorkflowsPage } from "../WorkflowsPage"

vi.mock("../../hooks/useWorkflow", () => ({
  useWorkflows: () => ({ data: { definitions: [
    { schema_version: 1, name: "review-pr", title: "Review PR", description: "Review code", source: "/project/.legato/workflows/review.yaml", inputs: { repo: { type: "string", required: true } }, steps: [{ id: "prepare", title: "Prepare", type: "github.pr.prepare", with: {} }, { id: "review", title: "Review", type: "agent.run", with: {} }, { id: "cleanup", title: "Remove worktree", type: "worktree.remove", trigger: "manual", with: {} }] },
    { schema_version: 1, name: "triage-task", title: "Triage a task", source: "builtin:triage-task", inputs: { task: { type: "task", required: true } }, on: { "task.entered_column": { column: "Backlog" } }, steps: [{ id: "read", title: "Read the task", type: "task.context", with: {} }] },
  ], errors: [] }, refresh: vi.fn() }),
  useWorkflowRuns: () => ({ data: [{ id: "wr-1", workflow_name: "review-pr", workflow_title: "Review PR #4", definition_source: "x", definition_digest: "d", inputs_json: "{}", status: "failed", current_step_id: "prepare", created_at: "2026-07-28T10:00:00Z" }], refresh: vi.fn() }),
}))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))

describe("WorkflowsPage workflows", () => {
  it("says what each workflow does, what it takes, when it starts and how its last run went", () => {
    render(<MemoryRouter><WorkflowsPage /></MemoryRouter>)
    fireEvent.click(screen.getByRole("tab", { name: "Workflows" }))
    const rows = within(screen.getByRole("list", { name: "Workflows" })).getAllByRole("listitem")
    const review = rows[0]
    expect(within(review).getByRole("heading", { name: "Review PR" })).toBeTruthy()
    expect(review.textContent).toContain("review.yaml")
    expect(review.textContent).not.toContain("/project/.legato")
    expect(review.textContent).toMatch(/Prepare.*Review.*Remove worktree/)
    expect(within(review).getByLabelText("waits for you")).toBeTruthy()
    expect(review.textContent).toContain("Takes repo")
    expect(within(review).getByRole("link", { name: /Failed/ }).getAttribute("href")).toBe("/workflows/runs/wr-1")
    expect(within(review).getByRole("button", { name: "Run Review PR" })).toBeTruthy()
    const triage = rows[1]
    expect(triage.textContent).toContain("built in")
    expect(triage.textContent).toContain("Takes a task")
    expect(triage.textContent).toContain("Starts when a task enters Backlog")
    expect(triage.textContent).toContain("No recent runs")
  })
})
