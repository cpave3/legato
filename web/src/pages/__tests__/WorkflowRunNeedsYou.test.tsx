import { QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { MemoryRouter, Route, Routes } from "react-router-dom"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { createQueryClient, queryKeys } from "../../lib/query"
import type { WorkflowRunView } from "../../lib/workflow"
import { mockReactFlow } from "../../test/reactFlow"
import { WorkflowRunPage } from "../WorkflowRunPage"

mockReactFlow()

const calls = vi.hoisted(() => ({ fetchRun: vi.fn(), trigger: vi.fn(), prompt: vi.fn(), retry: vi.fn(), another: vi.fn(), approvals: vi.fn(), decide: vi.fn(), attention: vi.fn(), execute: vi.fn(), dismiss: vi.fn() }))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../lib/workflow", async importOriginal => ({ ...(await importOriginal<typeof import("../../lib/workflow")>()), fetchWorkflowRun: calls.fetchRun, triggerWorkflowStep: calls.trigger, promptWorkflowStep: calls.prompt, retryWorkflowStep: calls.retry, anotherReviewPass: calls.another }))
vi.mock("../../lib/approvals", async importOriginal => ({ ...(await importOriginal<typeof import("../../lib/approvals")>()), fetchPendingApprovals: calls.approvals, decideApproval: calls.decide }))
vi.mock("../../lib/attention", async importOriginal => ({ ...(await importOriginal<typeof import("../../lib/attention")>()), fetchAttention: calls.attention, executeAttentionAction: calls.execute, dismissAttentionItem: calls.dismiss }))

const def = (id: string, title: string, type: string, trigger = "automatic") => ({ id, title, type, trigger, with: {} })
const steps = [def("investigate", "Investigate", "agent.run"), def("gate", "Approve the plan", "task.note", "manual"), def("end", "End the review", "agent.stop", "agent_ended"), def("review", "Review in passes", "github.pr.review_passes"), def("move", "Move to Ready", "task.move")]
const graph = { version: 1, nodes: [
  { id: "trigger:manual", kind: "trigger", trigger: { kind: "manual", summary: "Run by hand", enabled: true } },
  ...steps.map(s => ({ id: `step:${s.id}`, kind: "step", step: { id: s.id, title: s.title, type: s.type, trigger: s.trigger, key: {}, definition: s } })),
], edges: steps.map((s, i) => i === 0 ? { id: "start", kind: "start", source: "trigger:manual", target: "step:investigate" } : { id: `order${i}`, kind: "order", source: `step:${steps[i - 1].id}`, target: `step:${s.id}` }) }

const stepRun = (step_id: string, executor_type: string, status: string, extra = {}) => ({ run_id: "wr-1", step_id, step_order: 0, title: steps.find(s => s.id === step_id)!.title, executor_type, definition_json: JSON.stringify(steps.find(s => s.id === step_id)), resolved_inputs_json: "{}", outputs_json: "{}", status, latest_attempt: 1, ...extra })
const attempt = { id: "wa-1", run_id: "wr-1", step_id: "investigate", attempt_number: 1, executor_type: "agent.run", agent_session_id: 7, task_id: "task-1", agent_kind: "claude-code", status: "waiting_agent", needs_attention: true, attention_reason: "agent idle", started_at: "2026-11-21T10:00:00Z" }

function view(stepRuns: unknown[], extra = {}) {
  return { run: { id: "wr-1", workflow_name: "w", workflow_title: "The run", definition_source: "/w/w.yaml", definition_digest: "d", definition_json: "{}", inputs_json: "{}", status: "waiting", current_step_id: "investigate", created_at: "2026-11-21T10:00:00Z" }, steps: stepRuns, attempts: [attempt], artifacts: [], graph, ...extra }
}
const approval = (id: string, task_id: string) => ({ id, source: "claude-code", source_key: id, task_id, title: `Run git push (${id})`, details: { kind: "permission" }, options: [{ id: "allow", label: "Allow", kind: "allow" }], blocking: true, status: "pending", revision: 1, created_at: "2026-11-21T10:01:00Z" })
const item = (id: string, task_id: string, producer = "agents") => ({ id, producer, signal_kind: "operator_action", source_key: id, source_revision: "r1", task_id, title: `Item ${id}`, reason: "Do it", category: "agents", urgency: "normal", navigation: { kind: "task", target: task_id }, actions: [{ id: "done", label: "Done", kind: "execute" }], status: "open", created_at: "2026-11-21T10:01:00Z", updated_at: "2026-11-21T10:01:00Z" })

function show(data: unknown) {
  calls.fetchRun.mockResolvedValue(data)
  const client = createQueryClient()
  client.setQueryData(queryKeys.workflowRun("", "wr-1"), data as WorkflowRunView)
  render(<QueryClientProvider client={client}><MemoryRouter initialEntries={["/workflows/runs/wr-1"]}><Routes><Route path="/workflows/runs/:runId" element={<WorkflowRunPage />} /></Routes></MemoryRouter></QueryClientProvider>)
  return client
}

beforeEach(() => {
  cleanup()
  for (const fn of Object.values(calls)) fn.mockReset()
  calls.approvals.mockResolvedValue([])
  calls.attention.mockResolvedValue([])
  for (const fn of [calls.trigger, calls.prompt, calls.retry, calls.another, calls.decide, calls.execute, calls.dismiss]) fn.mockResolvedValue({})
  vi.stubGlobal("matchMedia", (query: string) => ({ matches: query.includes("min-width"), media: query, addEventListener: () => {}, removeEventListener: () => {} }))
})
afterEach(() => { vi.unstubAllGlobals() })

describe("a run's Needs you list", () => {
  it("offers Start for a step waiting for the operator", async () => {
    show(view([stepRun("gate", "task.note", "waiting_manual")], { attempts: [] }))
    const row = await within(await screen.findByRole("region", { name: "Needs you" })).findByRole("article", { name: "Approve the plan waits for you" })
    fireEvent.click(within(row).getByRole("button", { name: "Start" }))
    await waitFor(() => expect(calls.trigger).toHaveBeenCalledWith("", "wr-1", "gate", false))
  })

  it("offers End now for a step that waits for its agent to end", async () => {
    show(view([stepRun("end", "agent.stop", "waiting_manual")], { attempts: [] }))
    const row = await within(await screen.findByRole("region", { name: "Needs you" })).findByRole("article", { name: "End the review starts when the agent ends" })
    fireEvent.click(within(row).getByRole("button", { name: "End now" }))
    await waitFor(() => expect(calls.trigger).toHaveBeenCalledWith("", "wr-1", "end", false))
  })

  it("offers Open agent and Prompt to continue for a stalled agent", async () => {
    show(view([stepRun("investigate", "agent.run", "running")]))
    const row = await within(await screen.findByRole("region", { name: "Needs you" })).findByRole("article", { name: "Investigate: the agent stopped" })
    expect(within(row).getByRole("link", { name: "Open agent" }).getAttribute("href")).toContain("task=task-1")
    fireEvent.click(within(row).getByRole("button", { name: "Prompt to continue" }))
    await waitFor(() => expect(calls.prompt).toHaveBeenCalledWith("", "wr-1", "investigate"))
  })

  it("lists a review loop waiting on an author that needs the operator", async () => {
    show(view([stepRun("review", "github.pr.review_passes", "waiting")], { attempts: [], details: { review: { phase: "waiting_for_author", pass: 2, summary: "Waiting for author · 3 blocking", blocking_open: 3, needs_you: true, reviews: [] } } }))
    const row = await within(await screen.findByRole("region", { name: "Needs you" })).findByRole("article", { name: "Review in passes waits on its author" })
    fireEvent.click(within(row).getByRole("button", { name: "Run another pass" }))
    await waitFor(() => expect(calls.another).toHaveBeenCalledWith("", "wr-1", "review"))
  })

  // LEG-543: the wait is the agent's while its agent author fixes the findings.
  it("does not list a review loop whose agent author is fixing the findings", async () => {
    show(view([stepRun("review", "github.pr.review_passes", "waiting")], { attempts: [], details: { review: { phase: "waiting_for_author", pass: 1, summary: "Author fixing · 2 blocking", blocking_open: 2, needs_you: false, reviews: [] } } }))
    expect(await screen.findByRole("button", { name: "Step: Review in passes, Running" })).toBeTruthy()
    expect(screen.queryByRole("article", { name: "Review in passes waits on its author" })).toBeNull()
  })

  it("offers Retry for a failed step", async () => {
    show(view([stepRun("move", "task.move", "failed", { error: "no column Ready" })], { attempts: [] }))
    const row = await within(await screen.findByRole("region", { name: "Needs you" })).findByRole("article", { name: "Move to Ready failed" })
    expect(row.textContent).toContain("no column Ready")
    fireEvent.click(within(row).getByRole("button", { name: "Retry" }))
    await waitFor(() => expect(calls.retry).toHaveBeenCalledWith("", "wr-1", "move", false))
  })

  it("approves and denies the approvals of the run's agents, and ignores other agents'", async () => {
    calls.approvals.mockResolvedValue([approval("ap-1", "task-1"), approval("ap-2", "task-1"), approval("ap-9", "task-other")])
    show(view([stepRun("investigate", "agent.run", "running")], { attempts: [{ ...attempt, needs_attention: false }] }))
    const region = await screen.findByRole("region", { name: "Needs you" })
    await within(region).findByText("Run git push (ap-1)")
    expect(within(region).queryByText("Run git push (ap-9)")).toBeNull()
    expect(await screen.findByRole("button", { name: "Step: Investigate, Needs you" })).toBeTruthy()
    const first = within(region).getByText("Run git push (ap-1)").closest("[data-run-row]") as HTMLElement
    fireEvent.click(within(first).getByRole("button", { name: "Allow" }))
    await waitFor(() => expect(calls.decide).toHaveBeenCalledWith("", "ap-1", { decision: "allow", option: "allow" }))
    const second = within(region).getByText("Run git push (ap-2)").closest("[data-run-row]") as HTMLElement
    fireEvent.click(within(second).getByRole("button", { name: "Deny…" }))
    fireEvent.click(within(second).getByRole("button", { name: "Deny" }))
    await waitFor(() => expect(calls.decide).toHaveBeenCalledWith("", "ap-2", { decision: "deny", feedback: "" }))
  })

  it("shows the run's agents' operator items with their actions, and no one else's", async () => {
    calls.attention.mockResolvedValue([item("at-1", "task-1"), item("at-9", "task-other"), { ...item("at-wf", "task-1", "workflows"), source_key: "wr-1:investigate" }])
    show(view([stepRun("investigate", "agent.run", "running")], { attempts: [{ ...attempt, needs_attention: false }] }))
    const row = await within(await screen.findByRole("region", { name: "Needs you" })).findByRole("article", { name: "Item at-1" })
    expect(screen.queryByRole("article", { name: "Item at-9" })).toBeNull()
    expect(screen.queryByRole("article", { name: "Item at-wf" })).toBeNull()
    fireEvent.click(within(row).getByRole("button", { name: "Done" }))
    await waitFor(() => expect(calls.execute).toHaveBeenCalledWith("", "at-1", "done", "r1", { confirmed: false }))
  })

  it("hides the run's secret values in a row's reason", async () => {
    show(view([stepRun("move", "task.move", "failed", { error: "token hunter2 was refused" })], { attempts: [], run: { ...view([]).run, definition_json: JSON.stringify({ inputs: { token: { type: "string", secret: true } } }), inputs_json: JSON.stringify({ token: "hunter2" }) } }))
    const row = await within(await screen.findByRole("region", { name: "Needs you" })).findByRole("article", { name: "Move to Ready failed" })
    expect(row.textContent).toContain("token ••••••")
    expect(row.textContent).not.toContain("hunter2")
  })

  it.each([
    ["waiting_manual", "Remove worktree", "Remove worktree?", "Remove worktree", calls.trigger],
    ["failed", "Retry step", "Retry worktree removal?", "Retry removal", calls.retry],
  ])("asks before removing a worktree from a %s cleanup step, with its path and force", async (status, button, heading, confirm, call) => {
    show(view([stepRun("move", "worktree.remove", status, { resolved_inputs_json: JSON.stringify({ expected_path: "/w/legato-12" }) })], { attempts: [] }))
    const region = await screen.findByRole("region", { name: "Needs you" })
    fireEvent.click(within(region).getByRole("button", { name: button }))
    expect(call).not.toHaveBeenCalled()
    const dialog = screen.getByRole("heading", { name: heading }).closest("[role=dialog]") as HTMLElement ?? document.body
    expect(dialog.textContent).toContain("/w/legato-12")
    fireEvent.click(within(dialog).getByRole("checkbox"))
    fireEvent.click(within(dialog).getAllByRole("button", { name: confirm }).at(-1)!)
    await waitFor(() => expect(call).toHaveBeenCalledWith("", "wr-1", "move", true))
  })

  it("selects a row's node when the row is clicked", async () => {
    show(view([stepRun("move", "task.move", "failed", { error: "boom" })], { attempts: [] }))
    const row = await within(await screen.findByRole("region", { name: "Needs you" })).findByRole("article", { name: "Move to Ready failed" })
    fireEvent.click(within(row).getByText("boom"))
    const panel = await screen.findByRole("complementary", { name: "Move to Ready" })
    expect(panel).toBeTruthy()
    expect((await screen.findByRole("button", { name: "Step: Move to Ready, Failed" })).className).toContain("selected")
  })

  it("is not shown when nothing needs the operator", async () => {
    show(view([stepRun("move", "task.move", "running")], { attempts: [] }))
    await screen.findByRole("button", { name: "Step: Move to Ready, Running" })
    expect(screen.queryByRole("region", { name: "Needs you" })).toBeNull()
  })

  it("adds and removes an approval's row as approvals change", async () => {
    const client = show(view([stepRun("investigate", "agent.run", "running")], { attempts: [{ ...attempt, needs_attention: false }] }))
    await screen.findByRole("button", { name: "Step: Investigate, Running" })
    expect(screen.queryByRole("region", { name: "Needs you" })).toBeNull()
    calls.approvals.mockResolvedValue([approval("ap-1", "task-1")])
    await act(async () => { await client.invalidateQueries({ queryKey: queryKeys.approvals("") }) })
    expect(await within(await screen.findByRole("region", { name: "Needs you" })).findByText("Run git push (ap-1)")).toBeTruthy()
    calls.approvals.mockResolvedValue([])
    await act(async () => { await client.invalidateQueries({ queryKey: queryKeys.approvals("") }) })
    await waitFor(() => expect(screen.queryByRole("region", { name: "Needs you" })).toBeNull())
  })
})
