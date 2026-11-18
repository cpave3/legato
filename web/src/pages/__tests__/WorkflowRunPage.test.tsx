import { QueryClientProvider } from "@tanstack/react-query"
import { createQueryClient, queryKeys } from "../../lib/query"
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { MemoryRouter, Route, Routes } from "react-router-dom"
import { beforeEach, describe, expect, it, vi } from "vitest"
import type { WorkflowRunView } from "../../lib/workflow"
import { WorkflowRunPage } from "../WorkflowRunPage"

const { fetchArtifact, fetchRun, triggerStep, retryStep, promptStep, anotherPass } = vi.hoisted(() => ({ fetchArtifact: vi.fn(), fetchRun: vi.fn(), triggerStep: vi.fn(), retryStep: vi.fn(), promptStep: vi.fn(), anotherPass: vi.fn() }))

vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../lib/workflow", async importOriginal => ({ ...(await importOriginal<typeof import("../../lib/workflow")>()), fetchWorkflowRun: fetchRun, fetchWorkflowArtifact: fetchArtifact, triggerWorkflowStep: triggerStep, retryWorkflowStep: retryStep, promptWorkflowStep: promptStep, anotherReviewPass: anotherPass }))

const run = { id: "wr-1", workflow_name: "plan-task", workflow_title: "Plan jgek1vt3", definition_source: "/w/plan-task.yaml", definition_digest: "d", definition_json: JSON.stringify({ title: "Plan a task" }), inputs_json: '{"task":"jgek1vt3"}', related_task_id: "jgek1vt3", related_task_title: "Sync retries hammer Jira", created_at: "2026-07-28T10:00:00Z", started_at: "2026-07-28T10:00:00Z" }
const stepRun = (step_id: string, step_order: number, title: string, executor_type: string, status: string, extra = {}) => ({ run_id: "wr-1", step_id, step_order, title, executor_type, definition_json: "{}", resolved_inputs_json: "{}", outputs_json: "{}", status, latest_attempt: status === "pending" ? 0 : 1, ...extra })
const agentAttempt = (extra = {}) => ({ id: "wa-1", run_id: "wr-1", step_id: "plan", attempt_number: 1, executor_type: "agent.run", agent_session_id: 7, task_id: "task-1", agent_kind: "claude-code", working_dir: "/repo", status: "waiting_agent", started_at: "2026-07-28T10:00:00Z", ...extra })

function show(data: unknown) {
  fetchRun.mockResolvedValue(data)
  const client = createQueryClient()
  client.setQueryData(queryKeys.workflowRun("", "wr-1"), data as WorkflowRunView)
  render(<QueryClientProvider client={client}><MemoryRouter initialEntries={["/workflows/runs/wr-1"]}><Routes><Route path="/workflows/runs/:runId" element={<WorkflowRunPage />} /></Routes></MemoryRouter></QueryClientProvider>)
  return client
}
const now = () => screen.getByRole("region", { name: "Now" })

describe("WorkflowRunPage", () => {
  beforeEach(() => { cleanup(); fetchRun.mockReset(); fetchArtifact.mockReset(); triggerStep.mockReset(); retryStep.mockReset(); promptStep.mockReset(); anotherPass.mockReset(); vi.stubGlobal("URL", { ...URL, createObjectURL: vi.fn(() => "blob:test"), revokeObjectURL: vi.fn() }) })

  it("keeps the run and expanded steps on refresh failure, then retries", async () => {
    const data = { run: { ...run, status: "succeeded" }, steps: [stepRun("plan", 0, "Write the plan", "agent.run", "succeeded", { outputs_json: '{"risk":"low"}' })], attempts: [], artifacts: [] }
    const client = show(data)
    fireEvent.click(screen.getByRole("button", { name: /Write the plan/ }))
    fetchRun.mockRejectedValueOnce(new Error("Server unavailable"))
    await act(async () => { await client.invalidateQueries({ queryKey: queryKeys.workflowRun("", "wr-1") }) })
    await waitFor(() => expect(fetchRun).toHaveBeenCalled())
    expect((await screen.findByRole("alert")).textContent).toContain("Showing saved run")
    expect(screen.getByRole("heading", { level: 1, name: run.workflow_title })).toBeTruthy()
    expect(screen.getByRole("button", { name: /Write the plan/ }).getAttribute("aria-expanded")).toBe("true")
    fetchRun.mockResolvedValueOnce({ ...data, run: { ...data.run, workflow_title: "Updated plan" } })
    fireEvent.click(screen.getByRole("button", { name: "Retry" }))
    expect(await screen.findByRole("heading", { name: "Updated plan" })).toBeTruthy()
    expect(screen.queryByRole("alert")).toBeNull()
  })

  it("says what the run is for, its state and the step it is on", () => {
    show({ run: { ...run, status: "running", current_step_id: "plan", inputs_json: JSON.stringify({ task: { id: "jgek1vt3", title: "Sync retries hammer Jira", metadata: {} } }) }, steps: [stepRun("read", 0, "Read the task", "task.context", "succeeded"), stepRun("plan", 1, "Write the plan", "agent.run", "waiting_agent"), stepRun("note", 2, "Attach the plan", "task.note", "pending")], attempts: [agentAttempt({ agent_activity: "working" })], artifacts: [] })
    expect(screen.getByRole("heading", { level: 1, name: "Plan jgek1vt3" })).toBeTruthy()
    expect(screen.getByRole("link", { name: "jgek1vt3" }).getAttribute("href")).toBe("/board?highlight_task=jgek1vt3")
    expect(document.body.textContent).toContain("Plan a task")
    expect(document.body.textContent).toContain("Sync retries hammer Jira")
    expect(document.body.textContent).toContain("Step 2 of 3 · Write the plan")
    expect(within(now()).getByText(/claude-code is working/)).toBeTruthy()
    expect(within(now()).getByRole("link", { name: "Open agent" }).getAttribute("href")).toBe("/agents?task=task-1&workflow=wr-1")
    expect(screen.getByRole("button", { name: "Cancel run" })).toBeTruthy()
    expect(screen.getByText("task").nextElementSibling?.textContent).toBe("jgek1vt3 Sync retries hammer Jira")
    const steps = within(screen.getByRole("list", { name: "Steps" })).getAllByRole("listitem")
    expect(steps.map(step => step.textContent)).toEqual([expect.stringContaining("Read the task"), expect.stringContaining("Write the plan"), expect.stringContaining("Attach the plan")])
  })

  it("offers another pass while a review loop waits, and says why one is refused", async () => {
    show({ run: { ...run, status: "running", current_step_id: "review" }, steps: [stepRun("review", 0, "Review in passes", "github.pr.review_passes", "waiting"), stepRun("note", 1, "Note the outcome", "task.note", "pending")], attempts: [], artifacts: [] })
    expect(within(now()).getByText("Review in passes")).toBeTruthy()
    anotherPass.mockRejectedValueOnce(new Error("a pass is running already"))
    fireEvent.click(within(now()).getByRole("button", { name: "Run another pass" }))
    expect(anotherPass).toHaveBeenCalledWith("", "wr-1", "review")
    expect((await within(now()).findByRole("alert")).textContent).toContain("a pass is running already")
  })

  it("says where a review loop stands, with its reviews", () => {
    const review = (details: unknown) => show({ run: { ...run, status: "running", current_step_id: "review" }, steps: [stepRun("review", 0, "Review in passes", "github.pr.review_passes", "waiting")], attempts: [], artifacts: [], details: { review: details } })
    const reviews = [{ pass: 1, pr: 12, url: "https://github.com/acme/widget/pull/12#pullrequestreview-1" }]
    review({ phase: "reviewing", pass: 2, summary: "Reviewing · pass 2", blocking_open: 0, reviews })
    expect(within(now()).getByText("Reviewing · pass 2")).toBeTruthy()
    expect(within(now()).queryByRole("button", { name: "Run another pass" })).toBeNull()
    expect(within(now()).getByRole("link", { name: "Pass 1 review of #12" }).getAttribute("href")).toBe(reviews[0].url)
    cleanup()
    review({ phase: "waiting_for_author", pass: 1, summary: "Waiting for author · 3 blocking", blocking_open: 3, reviews })
    expect(within(now()).getByText("Waiting for author · 3 blocking")).toBeTruthy()
    expect(within(now()).getByRole("button", { name: "Run another pass" })).toBeTruthy()
    cleanup()
    review({ phase: "stuck", pass: 3, summary: "Stuck: the review reached its limit of 3 passes", blocking_open: 1, reason: "the review reached its limit of 3 passes", reviews })
    expect(within(now()).getByText("Stuck: the review reached its limit of 3 passes")).toBeTruthy()
    expect(within(now()).getByRole("button", { name: "Run another pass" })).toBeTruthy()
    cleanup()
    review({ phase: "clean", pass: 1, summary: "Approved", blocking_open: 0, reviews })
    expect(within(now()).getByText("Approved")).toBeTruthy()
  })

  it("puts an idle agent in front of the operator with a way to nudge it", () => {
    // As the run endpoint sends it: the attention is on the attempt, not the run.
    show({ run: { ...run, status: "running", current_step_id: "plan" }, steps: [stepRun("plan", 0, "Write the plan", "agent.run", "waiting_agent")], attempts: [agentAttempt({ needs_attention: true, attention_reason: "agent idle before step completion", agent_activity: "idle" })], artifacts: [] })
    expect(document.body.textContent).toContain("Needs you")
    expect(document.body.textContent).not.toContain("Running")
    expect(within(now()).getByText("The agent went idle before finishing “Write the plan”")).toBeTruthy()
    expect(within(now()).getByRole("link", { name: "Open agent" })).toBeTruthy()
    promptStep.mockResolvedValue({})
    fireEvent.click(within(now()).getByRole("button", { name: "Prompt to continue" }))
    expect(promptStep).toHaveBeenCalledWith("", "wr-1", "plan")
  })

  it("lets the operator end a run that waits for its agent to end", async () => {
    triggerStep.mockResolvedValue({})
    show({ run: { ...run, status: "waiting", current_step_id: "end", task_id: "task-1" }, steps: [stepRun("agent", 0, "Run agent", "agent.run", "succeeded"), stepRun("end", 1, "Finish", "worktree.remove", "waiting_manual", { definition_json: '{"trigger":"agent_ended"}' })], attempts: [], artifacts: [] })
    expect(document.body.textContent).toContain("Running")
    expect(within(now()).getByText("Finish starts when the agent ends")).toBeTruthy()
    expect(within(now()).getByRole("link", { name: "Open agent" }).getAttribute("href")).toBe("/agents?task=task-1&workflow=wr-1")
    expect(within(now()).queryByRole("button", { name: "Run step" })).toBeNull()
    fireEvent.click(within(now()).getByRole("button", { name: "End now" }))
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Remove worktree" }))
    await waitFor(() => expect(triggerStep).toHaveBeenCalledWith("", "wr-1", "end", false))
  })

  it("never shows the value of a secret input", () => {
    show({ run: { ...run, status: "running", current_step_id: "read", related_task_id: undefined, related_task_title: undefined, definition_json: JSON.stringify({ title: "Deploy", inputs: { token: { type: "string", secret: true } } }), inputs_json: JSON.stringify({ token: "s3cret", env: "staging" }) }, steps: [stepRun("read", 0, "Read", "task.context", "running", { resolved_inputs_json: JSON.stringify({ token: "s3cret" }) })], attempts: [], artifacts: [] })
    fireEvent.click(screen.getByRole("button", { name: /Read/ }))
    expect(document.body.textContent).not.toContain("s3cret")
    expect(document.body.textContent).toContain("staging")
  })

  it("says what a manual step will do and runs it", async () => {
    triggerStep.mockResolvedValue({})
    show({ run: { ...run, status: "waiting", current_step_id: "approve", needs_attention: true, attention_reason: "manual action required: Move to Ready" }, steps: [stepRun("approve", 0, "Move to Ready", "task.move", "waiting_manual", { definition_json: '{"trigger":"manual"}', resolved_inputs_json: JSON.stringify({ task: { id: "gpri1ycr", title: "Board search" }, column: "Ready" }) })], attempts: [], artifacts: [] })
    expect(within(now()).getByText("Move to Ready waits for you")).toBeTruthy()
    expect(within(now()).getByText("Moves gpri1ycr “Board search” to Ready.")).toBeTruthy()
    fireEvent.click(within(now()).getByRole("button", { name: "Run step" }))
    await waitFor(() => expect(triggerStep).toHaveBeenCalledWith("", "wr-1", "approve", false))
  })

  it("names the step that failed, its error, and retries it", async () => {
    retryStep.mockResolvedValue({})
    show({ run: { ...run, status: "failed", current_step_id: "move", error: "column \"Shipped\" not found", completed_at: "2026-07-28T10:00:01Z" }, steps: [stepRun("note", 0, "Record release note", "task.note", "succeeded"), stepRun("move", 1, "Move to Shipped", "task.move", "failed", { error: "column \"Shipped\" not found", resolved_inputs_json: '{"column":"Shipped"}' })], attempts: [], artifacts: [] })
    expect(document.body.textContent).toContain("Failed")
    expect(within(now()).getByText("Move to Shipped failed")).toBeTruthy()
    expect(within(now()).getByText("column \"Shipped\" not found")).toBeTruthy()
    expect(screen.queryByRole("button", { name: "Cancel run" })).toBeNull()
    fireEvent.click(within(now()).getByRole("button", { name: "Retry step" }))
    await waitFor(() => expect(retryStep).toHaveBeenCalledWith("", "wr-1", "move", false))
    const failed = within(screen.getByRole("list", { name: "Steps" })).getAllByRole("listitem")[1]
    expect(within(failed).getByRole("button", { name: /Move to Shipped/ }).getAttribute("aria-expanded")).toBe("true")
    expect(failed.textContent).toContain("Shipped")
  })

  it("shows what a finished run produced, previews it and downloads it under its own name", async () => {
    fetchArtifact.mockRejectedValueOnce(new Error("Preview unavailable"))
    let downloadedName = ""
    vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(function (this: HTMLAnchorElement) { downloadedName = this.download })
    show({ run: { ...run, status: "succeeded", current_step_id: "", completed_at: "2026-07-28T10:04:12Z" }, steps: [stepRun("plan", 0, "Write the plan", "agent.run", "succeeded", { outputs_json: JSON.stringify({ plan: { artifact_id: "wfa-1" }, risk: "low" }) })], attempts: [], artifacts: [{ id: "wfa-1", run_id: "wr-1", step_id: "plan", attempt_id: "wa-1", output_key: "plan", original_path: "reports/plan-xk2pb7do.md", media_type: "text/markdown", size_bytes: 266, digest: "sha256:x", created_at: "now" }] })
    expect(document.body.textContent).toContain("1 step · took 4m 12s")
    expect(within(now()).getByText("plan-xk2pb7do.md")).toBeTruthy()
    expect(within(now()).getByText("risk").nextElementSibling?.textContent).toBe("low")
    fireEvent.click(within(now()).getByText("Preview"))
    expect(await screen.findByText("Preview unavailable")).toBeTruthy()
    let resolvePreview!: (blob: Blob) => void
    fetchArtifact.mockImplementationOnce(() => new Promise<Blob>(resolve => { resolvePreview = resolve }))
    fireEvent.click(screen.getByRole("button", { name: "Retry preview" }))
    expect(screen.queryByText("Preview unavailable")).toBeNull()
    expect(screen.getByRole("status", { name: "Loading preview…" })).toBeTruthy()
    await act(async () => { resolvePreview(new Blob(["# Plan\n\nReady."])) })
    expect(await screen.findByRole("heading", { name: "Plan" })).toBeTruthy()
    fetchArtifact.mockRejectedValueOnce(new Error("Download unavailable"))
    fireEvent.click(within(now()).getByRole("button", { name: "Download" }))
    expect(await screen.findByText(/Download unavailable/)).toBeTruthy()
    expect(screen.getByRole("heading", { name: "Plan" })).toBeTruthy()
    fetchArtifact.mockResolvedValueOnce(new Blob(["# Plan"]))
    fireEvent.click(within(now()).getByRole("button", { name: "Download" }))
    await waitFor(() => expect(downloadedName).toBe("plan-xk2pb7do.md"))
    expect(screen.queryByText(/Download unavailable/)).toBeNull()
  })
})
