import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { afterEach, expect, it, vi } from "vitest"
import { WorkflowsPage } from "../WorkflowsPage"
import type { WorkflowDefinition } from "../../lib/workflow"

const { listed } = vi.hoisted(() => ({ listed: { definitions: [] as WorkflowDefinition[] } }))
vi.mock("../../hooks/useWorkflow", () => ({
  useWorkflows: () => ({ data: { definitions: listed.definitions, errors: [] }, refresh: vi.fn() }),
  useWorkflowRuns: () => ({ data: [], refresh: vi.fn() }),
  useWorkflowMap: () => ({ data: null, loading: false, error: null, refresh: vi.fn() }), useWorkflowStats: () => ({ data: null, refresh: vi.fn() }),
}))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
afterEach(cleanup)

const reviewLoop: WorkflowDefinition = { schema_version: 2, name: "review-loop", title: "Review loop", source: "builtin:review-loop", steps: [{ id: "open", title: "Open the review loop", type: "github.pr.review.open", with: {} }] }
const oldCopy: WorkflowDefinition = { schema_version: 1, name: "review-pr-passes", title: "Review pull request in passes", source: "/home/me/.config/legato/workflows/review-pr-passes.workflow.yaml", editable: true,
  steps: [{ id: "review", title: "Review in passes", type: "github.pr.review_passes", with: {} }, { id: "note", title: "Note", type: "task.note", with: {} }] }
const triage: WorkflowDefinition = { schema_version: 2, name: "triage", title: "Triage", source: "/w/triage.workflow.yaml", steps: [{ id: "read", title: "Read", type: "task.context", with: {} }] }

function show(definitions: WorkflowDefinition[]) {
  listed.definitions = definitions
  render(<MemoryRouter><WorkflowsPage /></MemoryRouter>)
  fireEvent.click(screen.getByRole("tab", { name: "Workflows" }))
}

// Story 10 (LEG-599): the built-in review-loop replaces an old
// review-pr-passes copy, and the page says so, with where to go next.
it("says review-loop replaces an old review-pr-passes copy", () => {
  show([oldCopy, reviewLoop, triage])
  const notice = screen.getByRole("note", { name: "review-loop replaces review-pr-passes" })
  expect(notice.textContent).toContain("Review pull request in passes")
  expect(notice.textContent).toContain("deprecated")
  expect(within(notice).getByRole("link", { name: "Review loop" }).getAttribute("href")).toBe("/workflows/review-loop")
  expect(within(notice).getByRole("link", { name: "Rewrite Review pull request in passes" }).getAttribute("href")).toBe("/workflows/review-pr-passes/edit")
})

it("names any workflow on the deprecated inline step, a loop's body's included", () => {
  const inLoop = { schema_version: 2, name: "nightly", title: "Nightly reviews", source: "/w/nightly.workflow.yaml", editable: true,
    steps: [{ id: "each", title: "Each", type: "", with: {}, steps: [{ id: "r", title: "Review", type: "github.pr.review_passes", with: {} }] }] } as unknown as WorkflowDefinition
  show([inLoop, reviewLoop])
  const notice = screen.getByRole("note", { name: "review-loop replaces review-pr-passes" })
  expect(within(notice).getByRole("link", { name: "Rewrite Nightly reviews" }).getAttribute("href")).toBe("/workflows/nightly/edit")
})

it("says nothing when no workflow uses the inline step", () => {
  show([reviewLoop, triage])
  expect(screen.queryByRole("note", { name: "review-loop replaces review-pr-passes" })).toBeNull()
})

it("says nothing of a review-pr-passes copy already rewritten to call review-loop", () => {
  const rewritten = { ...oldCopy, schema_version: 2, steps: [{ id: "review", title: "Review in passes", type: "workflow.run", with: { workflow: "review-loop", inputs: {} } }] } as WorkflowDefinition
  show([rewritten, reviewLoop, triage])
  expect(screen.queryByRole("note", { name: "review-loop replaces review-pr-passes" })).toBeNull()
})
