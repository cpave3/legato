import { QueryClientProvider } from "@tanstack/react-query"
import { createQueryClient, queryKeys } from "../../lib/query"
import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { MemoryRouter, Route, Routes } from "react-router-dom"
import { beforeEach, describe, expect, it, vi } from "vitest"
import type { ReviewRunDetails, WorkflowRunView } from "../../lib/workflow"
import { WorkflowRunPage } from "../WorkflowRunPage"

// The run page's review loop and its steward (spec LEG-559 stories 11–13,
// 20, 23; AC 20, 34, 36, 39; LEG-594).

const { fetchRun, anotherPass, stepAction, stopAgent } = vi.hoisted(() => ({ fetchRun: vi.fn(), anotherPass: vi.fn(), stepAction: vi.fn(), stopAgent: vi.fn() }))

vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../hooks/useNow", () => ({ useNow: () => Date.parse("2026-11-26T10:00:00Z") }))
vi.mock("../../lib/workflow", async importOriginal => ({ ...(await importOriginal<typeof import("../../lib/workflow")>()), fetchWorkflowRun: fetchRun, anotherReviewPass: anotherPass, workflowStepAction: stepAction, stopTaskAgent: stopAgent }))

const run = { id: "wr-1", workflow_name: "review-loop", workflow_title: "Review loop on acme/widget", definition_source: "builtin:review-loop", definition_digest: "d", inputs_json: '{"repo":"acme/widget"}', created_at: "2026-11-26T09:00:00Z", started_at: "2026-11-26T09:00:00Z" }
const stepRun = (path: string, step_order: number, title: string, executor_type: string, status: string, extra = {}) => ({ run_id: "wr-1", step_id: path.split("/").pop()!, path, step_order, title, executor_type, definition_json: "{}", resolved_inputs_json: "{}", outputs_json: "{}", status, latest_attempt: status === "pending" ? 0 : 1, ...extra })

const layers = [
  { number: 21, url: "https://github.com/acme/widget/pull/21", title: "Greeting", state: "merged", draft: false, mergeable: "" },
  { number: 22, url: "https://github.com/acme/widget/pull/22", title: "Farewell", state: "open", draft: false, mergeable: "CONFLICTING" },
]
const watching: ReviewRunDetails = {
  phase: "clean", pass: 2, summary: "Stewarding", blocking_open: 0, reviews: [], loop_id: "rl-0123456789ab",
  reviewer: "claude-code:deep", reviewer_model: "opus", findings: { total: 3, blocking: 1, nits: 2, open: 0 }, layers,
  steward: { held: false, last_wake: "pushed", last_wake_at: "2026-11-26T09:30:00Z", reentries: 1, max_reentries: 5, idle_deadline: "2026-12-03T09:30:00Z" },
  actions: ["stop_watching", "end"],
}

function show(details: ReviewRunDetails, status = "waiting", path = "steward/0/watch", runStatus = "running") {
  const executor = path.endsWith("watch") || path.endsWith("author") ? "github.pr.review.wait" : "github.pr.review.pass"
  const data = { run: { ...run, status: runStatus, current_step_id: path }, steps: [stepRun("open", 0, "Open the review loop", "github.pr.review.open", "succeeded"), stepRun(path, 1, "Watch", executor, status)], attempts: [], artifacts: [], details: { [path]: details } }
  fetchRun.mockResolvedValue(data)
  const client = createQueryClient()
  client.setQueryData(queryKeys.workflowRun("", "wr-1"), data as unknown as WorkflowRunView)
  render(<QueryClientProvider client={client}><MemoryRouter initialEntries={["/workflows/runs/wr-1"]}><Routes><Route path="/workflows/runs/:runId" element={<WorkflowRunPage />} /></Routes></MemoryRouter></QueryClientProvider>)
}
const now = () => screen.getByRole("region", { name: "Now" })
// click presses a button and lets its action settle.
const click = async (button: HTMLElement) => { await act(async () => { fireEvent.click(button) }) }

describe("WorkflowRunPage review loop", () => {
  beforeEach(() => { cleanup(); fetchRun.mockReset(); anotherPass.mockReset(); stepAction.mockReset(); stopAgent.mockReset(); stepAction.mockResolvedValue({}); stopAgent.mockResolvedValue({}) })

  it("shows the steward's layers, mergeability, last wake, re-entries, idle deadline and reviewer", () => {
    show(watching)
    const panel = within(now())
    expect(panel.getByRole("heading", { name: /Stewarding/ })).toBeTruthy()
    const prs = within(panel.getByRole("list", { name: "Pull requests" })).getAllByRole("listitem")
    expect(prs.map(item => item.textContent)).toEqual([expect.stringContaining("Merged"), expect.stringContaining("Conflicts with its base")])
    expect(within(prs[1]).getByRole("link", { name: "#22" }).getAttribute("href")).toBe(layers[1].url)
    const text = now().textContent ?? ""
    expect(text).toContain("Watching")
    expect(text).toContain("Last woke: a push · 30m ago")
    expect(text).toContain("Re-entries: 1 of 5")
    expect(text).toContain("Ends idle in 7 days")
    expect(text).toContain("Pass 2 · 3 findings, 1 blocking, 0 open")
    expect(text).toContain("claude-code:deep (opus)")
    expect(text).not.toContain("still stands")
  })

  it("Stop watching and End act on the steward's step", async () => {
    show(watching)
    await click(within(now()).getByRole("button", { name: "Stop watching" }))
    expect(stepAction).toHaveBeenCalledWith("", "wr-1", "steward/0/watch", "stop_watching", undefined)
    await click(within(now()).getByRole("button", { name: "End" }))
    expect(stepAction).toHaveBeenCalledWith("", "wr-1", "steward/0/watch", "end", undefined)
    expect(within(now()).queryByRole("button", { name: "Watch again" })).toBeNull()
  })

  it("a held steward says so and that legato-reviews' approval still stands, with Watch again", async () => {
    show({ ...watching, summary: "Held", steward: { ...watching.steward!, held: true }, actions: ["watch_again", "end"], approval_stands: "legato-reviews' approval at abc1234 still stands" })
    expect(within(now()).getByRole("heading", { name: /Held/ })).toBeTruthy()
    expect(now().textContent).toContain("Held: nothing is sent and nothing is reviewed")
    expect(within(now()).getByText("legato-reviews' approval at abc1234 still stands")).toBeTruthy()
    await click(within(now()).getByRole("button", { name: "Watch again" }))
    expect(stepAction).toHaveBeenCalledWith("", "wr-1", "steward/0/watch", "watch_again", undefined)
    expect(within(now()).queryByRole("button", { name: "Stop watching" })).toBeNull()
  })

  it("a stuck wait offers Run another pass, Message the author and End", async () => {
    show({ phase: "stuck", pass: 3, summary: "Stuck: the review reached its limit of 3 passes", blocking_open: 1, needs_you: true, reviews: [], loop_id: "rl-1", findings: { total: 1, blocking: 1, nits: 0, open: 1 }, layers: [layers[1]], actions: ["another_pass", "message", "end"] }, "waiting", "review/2/pass")
    const panel = within(now())
    await click(panel.getByRole("button", { name: "Run another pass" }))
    expect(stepAction).toHaveBeenCalledWith("", "wr-1", "review/2/pass", "another_pass", undefined)
    expect(anotherPass).not.toHaveBeenCalled()
    fireEvent.click(panel.getByRole("button", { name: "Message the author" }))
    const box = panel.getByRole("textbox", { name: "Message to the author" })
    expect((panel.getByRole("button", { name: "Send" }) as HTMLButtonElement).disabled).toBe(true)
    fireEvent.change(box, { target: { value: "Split the refactor out." } })
    await click(panel.getByRole("button", { name: "Send" }))
    expect(stepAction).toHaveBeenCalledWith("", "wr-1", "review/2/pass", "message", "Split the refactor out.")
    await click(panel.getByRole("button", { name: "End" }))
    expect(stepAction).toHaveBeenCalledWith("", "wr-1", "review/2/pass", "end", undefined)
  })

  it("a steward stuck at its budget says its approval still stands and offers Watch again", () => {
    show({ ...watching, phase: "stuck", summary: "Stuck: a push came after 5 re-entries", needs_you: true, steward: { ...watching.steward!, reentries: 5 }, actions: ["watch_again", "message", "end"], approval_stands: "legato-reviews' approval at abc1234 still stands" })
    expect(within(now()).getByText("legato-reviews' approval at abc1234 still stands")).toBeTruthy()
    expect(now().textContent).toContain("Re-entries: 5 of 5")
    for (const name of ["Watch again", "Message the author", "End"]) expect(within(now()).getByRole("button", { name })).toBeTruthy()
    expect(within(now()).queryByRole("button", { name: "Run another pass" })).toBeNull()
  })

  it("says the author agent was left running after the loop ended, and stops it", async () => {
    show({ phase: "ended", pass: 2, summary: "Ended while stuck", blocking_open: 1, reviews: [], loop_id: "rl-1", outcome: "stuck", author_task: "board-1", author_left_running: true }, "succeeded", "review/1/pass", "succeeded")
    const left = screen.getByRole("region", { name: "Author agent" })
    expect(left.textContent).toContain("board-1's agent was left running")
    fireEvent.click(within(left).getByRole("button", { name: "Stop" }))
    expect(stopAgent).toHaveBeenCalledWith("", "board-1")
    expect(await within(left).findByText("Stopped.")).toBeTruthy()
  })

  it("says nothing of the author when it was not left running", () => {
    show({ phase: "clean", pass: 1, summary: "Approved", blocking_open: 0, reviews: [], loop_id: "rl-1", outcome: "approved", author_task: "board-1" }, "succeeded", "review/0/pass", "succeeded")
    expect(screen.queryByRole("region", { name: "Author agent" })).toBeNull()
  })
})
