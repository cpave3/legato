import { QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { MemoryRouter, Route, Routes } from "react-router-dom"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { createQueryClient, queryKeys } from "../../lib/query"
import type { WorkflowRunView } from "../../lib/workflow"
import { mockReactFlow } from "../../test/reactFlow"
import { WorkflowRunPage } from "../WorkflowRunPage"

mockReactFlow()

const { fetchRun } = vi.hoisted(() => ({ fetchRun: vi.fn() }))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../lib/workflow", async importOriginal => ({ ...(await importOriginal<typeof import("../../lib/workflow")>()), fetchWorkflowRun: fetchRun }))

const steps = {
  investigate: { id: "investigate", title: "Investigate the task", type: "agent.run", trigger: "automatic", with: { prompt: "Look", token: "${{ workflow.inputs.token }}" } },
  note: { id: "note", title: "Add a note", type: "task.note", trigger: "manual", with: { body: "x" } },
  review: { id: "review", title: "Review in passes", type: "github.pr.review_passes", trigger: "automatic", with: { repo: "acme/widget", pr_number: 4, max_passes: "${{ workflow.inputs.max_passes }}" } },
  move: { id: "move", title: "Move to Ready", type: "task.move", trigger: "automatic", with: { column: "Ready" } },
}
const definition = { schema_version: 1, name: "triage", title: "Triage", inputs: { token: { type: "string", secret: true } }, steps: Object.values(steps) }
const stepNode = (s: (typeof steps)[keyof typeof steps], extra = {}) => ({ id: `step:${s.id}`, kind: "step", step: { id: s.id, title: s.title, type: s.type, trigger: s.trigger, key: {}, definition: s, ...extra } })
const graph = { version: 1, nodes: [
  { id: "trigger:manual", kind: "trigger", trigger: { kind: "manual", summary: "Run by hand", enabled: true } },
  { id: "trigger:task.entered_column", kind: "trigger", trigger: { kind: "task.entered_column", summary: "Card enters Triage", enabled: true, max_runs_per_day: 3 } },
  stepNode(steps.investigate), stepNode(steps.note, { gate: "manual" }), stepNode(steps.review, { group: "review_loop" }),
  { id: "step:review/pass", kind: "inner", parent: "step:review", inner: { role: "pass", title: "Review pass" } },
  { id: "step:review/fixes", kind: "inner", parent: "step:review", inner: { role: "fixes", title: "Author fixes" } },
  stepNode(steps.move),
], edges: [
  { id: "start:trigger:manual>step:investigate", kind: "start", source: "trigger:manual", target: "step:investigate" },
  { id: "start:trigger:task.entered_column>step:investigate", kind: "start", source: "trigger:task.entered_column", target: "step:investigate" },
  { id: "order:step:investigate>step:note", kind: "order", source: "step:investigate", target: "step:note" },
  { id: "order:step:note>step:review", kind: "order", source: "step:note", target: "step:review" },
  { id: "order:step:review>step:move", kind: "order", source: "step:review", target: "step:move" },
  { id: "loop:step:review/pass>step:review/fixes", kind: "loop", source: "step:review/pass", target: "step:review/fixes" },
  { id: "loop:step:review/fixes>step:review/pass", kind: "loop", source: "step:review/fixes", target: "step:review/pass" },
] }

const stepRun = (step_id: string, step_order: number, executor_type: string, status: string, extra = {}) => ({ run_id: "wr-1", step_id, step_order, title: Object.values(steps).find(s => s.id === step_id)!.title, executor_type, definition_json: "{}", resolved_inputs_json: "{}", outputs_json: "{}", status, latest_attempt: 1, started_at: "2026-11-21T10:00:00Z", ...extra })
const attempt = (n: number, extra = {}) => ({ id: `wa-${n}`, run_id: "wr-1", step_id: "investigate", attempt_number: n, executor_type: "agent.run", agent_session_id: 7, task_id: "task-1", agent_kind: "claude-code", tier: "fast", working_dir: "/repo", status: "failed", error: "exited", started_at: "2026-11-21T10:00:00Z", ended_at: "2026-11-21T10:05:00Z", ...extra })

function view(over: { steps?: unknown[]; attempts?: unknown[]; details?: unknown; trigger_kind?: string; definition_changed?: boolean; definition_missing?: boolean; status?: string } = {}) {
  return {
    run: { id: "wr-1", workflow_name: "triage", workflow_title: "Triage LEG-1", definition_source: "/w/triage.yaml", definition_digest: "d", definition_json: JSON.stringify(definition), inputs_json: JSON.stringify({ token: "hunter2", max_passes: 5 }), status: over.status ?? "running", current_step_id: "investigate", created_at: "2026-11-21T10:00:00Z", started_at: "2026-11-21T10:00:00Z" },
    steps: over.steps ?? [
      stepRun("investigate", 0, "agent.run", "running", { resolved_inputs_json: JSON.stringify({ prompt: "Look", token: "hunter2" }), outputs_json: JSON.stringify({ summary: "token was hunter2" }) }),
      stepRun("note", 1, "task.note", "pending", { started_at: undefined }),
      stepRun("review", 2, "github.pr.review_passes", "pending", { started_at: undefined }),
      stepRun("move", 3, "task.move", "pending", { started_at: undefined }),
    ],
    attempts: over.attempts ?? [attempt(1), attempt(2), attempt(3, { status: "waiting_agent", error: undefined, ended_at: undefined })],
    artifacts: [],
    details: over.details ?? null,
    trigger_kind: over.trigger_kind ?? "task.entered_column",
    definition_changed: over.definition_changed ?? false,
    definition_missing: over.definition_missing ?? false,
    graph,
  }
}

function show(data: unknown) {
  fetchRun.mockResolvedValue(data)
  const client = createQueryClient()
  client.setQueryData(queryKeys.workflowRun("", "wr-1"), data as WorkflowRunView)
  render(<QueryClientProvider client={client}><MemoryRouter initialEntries={["/workflows/runs/wr-1"]}><Routes><Route path="/workflows/runs/:runId" element={<WorkflowRunPage />} /></Routes></MemoryRouter></QueryClientProvider>)
  return client
}

beforeEach(() => {
  cleanup()
  fetchRun.mockReset()
  vi.stubGlobal("matchMedia", (query: string) => ({ matches: query.includes("min-width"), media: query, addEventListener: () => {}, removeEventListener: () => {} }))
})
afterEach(() => { vi.unstubAllGlobals() })

describe("the run graph", () => {
  it("replaces the step list on a wide screen, keeping the Now panel and Cancel run", async () => {
    show(view())
    expect(await screen.findByRole("button", { name: "Step: Investigate the task, Running" })).toBeTruthy()
    expect(screen.queryByRole("list", { name: "Steps" })).toBeNull()
    expect(screen.getByRole("region", { name: "Now" })).toBeTruthy()
    expect(screen.getByRole("button", { name: "Cancel run" })).toBeTruthy()
  })

  it("says each step's state as a word, with attempts folded, the agent tool and elapsed time", async () => {
    show(view())
    const investigate = await screen.findByRole("button", { name: "Step: Investigate the task, Running" })
    expect(investigate.textContent).toContain("Running")
    expect(investigate.textContent).toContain("×3")
    expect(investigate.textContent).toContain("claude-code")
    expect(investigate.textContent).toMatch(/\d+[smhd]/)
    expect(screen.getByRole("button", { name: "Step: Add a note, waits for you, Waiting" }).textContent).toContain("Waiting")
  })

  it.each([
    ["pending", "Waiting"], ["running", "Running"], ["waiting_manual", "Needs you"], ["failed", "Failed"], ["succeeded", "Done"], ["skipped", "Skipped"], ["cancelled", "Skipped"],
  ])("draws a step that is %s as %s", async (status, word) => {
    show(view({ attempts: [], steps: [stepRun("move", 3, "task.move", status)] }))
    expect(await screen.findByRole("button", { name: `Step: Move to Ready, ${word}` })).toBeTruthy()
  })

  it("says Needs you for an agent that needs attention", async () => {
    show(view({ attempts: [attempt(1, { status: "waiting_agent", needs_attention: true, attention_reason: "agent idle", error: undefined })] }))
    expect(await screen.findByRole("button", { name: "Step: Investigate the task, Needs you" })).toBeTruthy()
  })

  it("shows the review loop's pass, and Needs you while it waits on an author that needs the operator", async () => {
    show(view({ attempts: [], steps: [stepRun("review", 2, "github.pr.review_passes", "waiting", { resolved_inputs_json: JSON.stringify({ max_passes: 5 }) })], details: { review: { phase: "waiting_for_author", pass: 2, summary: "Waiting for author", blocking_open: 3, needs_you: true, reviews: [{ pass: 1, pr: 4, url: "https://github.com/acme/widget/pull/4#r1" }] } } }))
    const group = await screen.findByRole("button", { name: "Step: Review in passes, Needs you" })
    expect(group.textContent).toContain("Pass 2 of 5")
  })

  it("shows a review loop whose agent author is fixing the findings as running", async () => {
    show(view({ attempts: [], steps: [stepRun("review", 2, "github.pr.review_passes", "waiting")], details: { review: { phase: "waiting_for_author", pass: 1, summary: "Author fixing · 2 blocking", blocking_open: 2, needs_you: false, reviews: [] } } }))
    expect(await screen.findByRole("button", { name: "Step: Review in passes, Running" })).toBeTruthy()
  })

  it("shows the working agent on the agents graph, with its run", async () => {
    show(view())
    const link = await screen.findByRole("link", { name: "Show on agents graph" })
    expect(link.getAttribute("href")).toBe("/agents?view=graph&task=task-1&workflow=wr-1")
  })

  it("links an interview to the agent it borrows, not to the run's task", async () => {
    show(view({ attempts: [attempt(1, { executor_type: "agent.interview", task_id: "run-task", agent_id: "LEG-2", status: "waiting_agent", error: undefined, ended_at: undefined })] }))
    expect((await screen.findByRole("link", { name: "Show on agents graph" })).getAttribute("href")).toBe("/agents?view=graph&task=LEG-2&workflow=wr-1")
    expect(screen.getAllByRole("link", { name: "Open agent" })[0].getAttribute("href")).toContain("task=LEG-2")
  })

  it("marks the trigger that started the run and dims the others", async () => {
    show(view())
    expect(await screen.findByRole("button", { name: "Trigger: Card enters Triage, started this run" })).toBeTruthy()
    expect(screen.getByRole("button", { name: "Trigger: Run by hand" }).className).toContain("opacity")
  })

  it("opens a step's side panel: inputs, outputs, attempts, its YAML, secrets redacted", async () => {
    show(view())
    fireEvent.click(await screen.findByRole("button", { name: "Step: Investigate the task, Running" }))
    const panel = screen.getByRole("complementary", { name: "Investigate the task" })
    expect(panel.textContent).toContain("Look")
    expect(panel.textContent).toContain("summary")
    const attempts = within(panel).getByRole("list", { name: "Attempts" })
    expect(within(attempts).getAllByRole("listitem")).toHaveLength(3)
    expect(attempts.textContent).toContain("claude-code (fast)")
    expect(attempts.textContent).toContain("/repo")
    expect(attempts.textContent).toContain("exited")
    expect(within(panel).getAllByRole("link", { name: "Open agent" })[0].getAttribute("href")).toContain("task=task-1")
    expect(within(panel).getByLabelText("Step YAML").textContent).toContain("id: investigate")
    expect(panel.textContent).not.toContain("hunter2")
  })

  it("lists the review loop's passes with their reviews in its panel", async () => {
    show(view({ attempts: [], steps: [stepRun("review", 2, "github.pr.review_passes", "waiting")], details: { review: { phase: "reviewing", pass: 2, summary: "Reviewing · pass 2", blocking_open: 1, reviews: [{ pass: 1, pr: 4, url: "https://github.com/acme/widget/pull/4#r1" }] } } }))
    fireEvent.click(await screen.findByRole("button", { name: "Step: Review in passes, Running" }))
    const panel = screen.getByRole("complementary", { name: "Review in passes" })
    expect(panel.textContent).toContain("Reviewing · pass 2")
    expect(panel.textContent).toContain("1 blocking")
    expect(within(panel).getByRole("link", { name: "Pass 1 review of #4" }).getAttribute("href")).toContain("pull/4")
  })

  it("updates a node's state when the run is refetched, without moving it", async () => {
    const client = show(view({ attempts: [], steps: [stepRun("move", 3, "task.move", "running")] }))
    const before = await screen.findByRole("button", { name: "Step: Move to Ready, Running" })
    const place = before.style.transform
    fetchRun.mockResolvedValue(view({ attempts: [], steps: [stepRun("move", 3, "task.move", "succeeded")] }))
    await act(async () => { await client.invalidateQueries({ queryKey: queryKeys.workflowRun("", "wr-1") }) })
    const after = await screen.findByRole("button", { name: "Step: Move to Ready, Done" })
    expect(after.style.transform).toBe(place)
  })

  it.each([
    [{ definition_changed: true }, "This workflow has changed since this run"],
    [{ definition_missing: true }, "This workflow's file is gone"],
  ])("says when the workflow's file differs from the run's copy", async (over, notice) => {
    show(view(over))
    await screen.findByRole("button", { name: /^Step: Investigate/ })
    expect(screen.getByText(notice)).toBeTruthy()
  })

  it("keeps the step list on a narrow screen", async () => {
    vi.stubGlobal("matchMedia", (query: string) => ({ matches: query.includes("max-width"), media: query, addEventListener: () => {}, removeEventListener: () => {} }))
    show(view())
    expect(screen.getByRole("list", { name: "Steps" })).toBeTruthy()
    await waitFor(() => expect(screen.queryByRole("button", { name: /^Step:/ })).toBeNull())
  })
})
