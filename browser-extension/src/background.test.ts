import { describe, expect, it, vi } from "vitest"
import { DEFAULT_CONFIG } from "./config"
import { createMessageHandler, createPanelTracker, openReviewPanelFromGesture } from "./background"
import type { WorkflowDiscovery } from "./legato"

const prInputs = { repo: { type: "string" }, pr_number: { type: "integer" }, primary_dir: { type: "string" } }
const discovery: WorkflowDiscovery = {
  definitions: [{ name: "review-pr", inputs: prInputs }, { name: "discuss-pr", inputs: prInputs }],
  errors: [],
  repositories: [{ name: "Legato", repo: "cpave3/legato", primary_dir: "/projects/legato" }],
}

function dependencies() {
  return {
    loadConfig: vi.fn().mockResolvedValue(DEFAULT_CONFIG),
    hasPermission: vi.fn().mockResolvedValue(true),
    discover: vi.fn().mockResolvedValue(discovery),
    start: vi.fn().mockResolvedValue({ run: { id: "run-1" } }),
    openRun: vi.fn().mockResolvedValue(undefined),
    importTicket: vi.fn().mockResolvedValue({ id: "task-1" }),
    openTask: vi.fn().mockResolvedValue(undefined),
    loadReviewModel: vi.fn().mockResolvedValue({ state: "empty", status: "", context: { repository: "cpave3/legato", pullRequestNumber: 7 }, runs: [], poll: false }),
    getTabUrl: vi.fn().mockResolvedValue("https://github.com/cpave3/legato/pull/7"),
    saveReviewContext: vi.fn().mockResolvedValue(undefined),
    listRuns: vi.fn().mockResolvedValue([]),
  }
}

const pr = { repository: "cpave3/legato", pullRequestNumber: 7 }
const discussionRun = (id: string, status: string) => ({
  id, workflow_name: "discuss-pr", workflow_title: "Discuss", status, current_step_id: "end",
  task_id: `task-${id}`, created_at: "2026-10-28T00:00:00Z",
  inputs_json: JSON.stringify({ repo: "cpave3/legato", pr_number: 7, primary_dir: "/projects/legato" }),
})

// A review run paused at currentStep: "end" waits for its agent to end, as
// review-pr does once the review is written; "cleanup" is a manual step.
const reviewDefinition = JSON.stringify({
  name: "review-pr",
  steps: [
    { id: "review", type: "agent.run", trigger: "automatic" },
    { id: "end", type: "agent.stop", trigger: "agent_ended" },
    { id: "cleanup", type: "worktree.remove", trigger: "manual" },
  ],
})
const reviewRun = (id: string, status: string, currentStep: string) => ({
  ...discussionRun(id, status), workflow_name: "review-pr", current_step_id: currentStep, definition_json: reviewDefinition,
})

describe("openReviewPanelFromGesture", () => {
  it("invokes Side Panel open before context persistence settles", async () => {
    const calls: string[] = []
    let resolvePersist!: () => void
    const persist = vi.fn(() => new Promise<void>((resolve) => { resolvePersist = resolve; calls.push("persist") }))
    const effects = {
      cache: vi.fn(() => calls.push("cache")),
      persist,
      open: vi.fn(async () => { calls.push("open") }),
      notify: vi.fn(() => calls.push("notify")),
    }
    const result = openReviewPanelFromGesture(4, { repository: "cpave3/legato", pullRequestNumber: 7 }, effects)
    expect(calls).toEqual(["cache", "open", "persist"])
    resolvePersist()
    await result
    expect(calls).toEqual(["cache", "open", "persist", "notify"])
  })
})

describe("openReviewPanelFromGesture with the panel already open", () => {
  it("does not open it again, which would close it, and still hands the panel its update", async () => {
    const effects = { cache: vi.fn(), persist: vi.fn().mockResolvedValue(undefined), open: vi.fn().mockResolvedValue(undefined), notify: vi.fn() }

    await openReviewPanelFromGesture(4, { repository: "cpave3/legato", pullRequestNumber: 7 }, effects, { alreadyOpen: true })

    expect(effects.open).not.toHaveBeenCalled()
    expect(effects.persist).toHaveBeenCalled()
    expect(effects.notify).toHaveBeenCalledWith(4)
  })
})

describe("createPanelTracker", () => {
  it("knows which windows have the side panel open", () => {
    const panels = createPanelTracker()
    expect(panels.isOpen(1)).toBe(false)

    const closeFirst = panels.opened(1)
    panels.opened(1)
    closeFirst()

    expect(panels.isOpen(1)).toBe(true)
    expect(panels.isOpen(2)).toBe(false)
  })
})

describe("GitHub workflow messages", () => {
  it("loads a secret-free review model for an explicit panel tab and run selection", async () => {
    const deps = dependencies()
    deps.loadReviewModel.mockResolvedValue({
      state: "ready", status: "waiting", context: { repository: "cpave3/legato", pullRequestNumber: 7 },
      runs: [], poll: false, markdown: "# Review", runUrl: "http://localhost:3080/workflows/runs/run-1",
    })
    const response = await createMessageHandler(deps)({ type: "getReviewModel", tabId: 4, selectedRunId: "old" }, {})
    expect(response).toMatchObject({ ok: true, review: { state: "ready", markdown: "# Review" } })
    expect(JSON.stringify(response)).not.toContain("top-secret")
    expect(deps.saveReviewContext).toHaveBeenCalledWith(4, { repository: "cpave3/legato", pullRequestNumber: 7 })
    expect(deps.loadReviewModel).toHaveBeenCalledWith(4, "old")
  })

  it("detects an existing review from the content-script context", async () => {
    const deps = dependencies()
    deps.loadReviewModel.mockResolvedValue({
      state: "ready", status: "waiting_manual", context: { repository: "cpave3/legato", pullRequestNumber: 7 },
      runs: [{ id: "run-1" }], poll: false,
    })
    const response = await createMessageHandler(deps)(
      { type: "detectGitHubReview", context: { repository: "cpave3/legato", pullRequestNumber: 7 } },
      { tab: { id: 4 } },
    )

    expect(response).toMatchObject({ ok: true, review: { state: "ready", runs: [{ id: "run-1" }] } })
    expect(deps.saveReviewContext).toHaveBeenCalledWith(4, { repository: "cpave3/legato", pullRequestNumber: 7 })
    expect(deps.loadReviewModel).toHaveBeenCalledWith(4)
  })

  it("prepares a secret-free confirmation without starting a run", async () => {
    const deps = dependencies()
    deps.loadConfig.mockResolvedValue({ ...DEFAULT_CONFIG, token: "top-secret" })
    const handler = createMessageHandler(deps)
    const response = await handler({ type: "prepareGitHubWorkflow", context: { repository: "cpave3/legato", pullRequestNumber: 7 } }, { tab: { id: 4 } })

    expect(response).toMatchObject({ ok: true, prepared: { workflowName: "review-pr", pullRequestNumber: 7 } })
    expect(JSON.stringify(response)).not.toContain("top-secret")
    expect(deps.start).not.toHaveBeenCalled()
  })

  it("revalidates context, starts exactly once, and opens the returned run", async () => {
    const deps = dependencies()
    const handler = createMessageHandler(deps)
    const message = { type: "startPreparedWorkflow", context: { repository: "cpave3/legato", pullRequestNumber: 7 } }
    const first = handler(message, { tab: { id: 4 } })
    const duplicate = handler(message, { tab: { id: 4 } })

    await expect(first).resolves.toEqual({ ok: true, runUrl: "http://localhost:3080/workflows/runs/run-1" })
    await expect(duplicate).resolves.toEqual({ ok: false, error: "A Legato action is already running for this tab" })
    expect(deps.start).toHaveBeenCalledOnce()
    expect(deps.start).toHaveBeenCalledWith(DEFAULT_CONFIG, {
      workflowName: "review-pr", repositoryPresetName: "Legato", repository: "cpave3/legato",
      pullRequestNumber: 7, primaryDir: "/projects/legato",
      inputs: { repo: "cpave3/legato", pr_number: "7", primary_dir: "/projects/legato" },
    })
    expect(deps.openRun).toHaveBeenCalledWith("http://localhost:3080/workflows/runs/run-1")
  })

  it("reports a started run without inviting a duplicate when navigation fails", async () => {
    const deps = dependencies()
    deps.openRun.mockRejectedValue(new Error("tabs unavailable"))
    const response = await createMessageHandler(deps)(
      { type: "startPreparedWorkflow", context: { repository: "cpave3/legato", pullRequestNumber: 7 } },
      { tab: { id: 4 } },
    )
    expect(response).toEqual({
      ok: true,
      runUrl: "http://localhost:3080/workflows/runs/run-1",
      navigationWarning: "Workflow started, but Chrome could not open the run. Open Legato to view it.",
    })
    expect(deps.start).toHaveBeenCalledOnce()
  })

  it("imports a Jira ticket once and opens the returned local task", async () => {
    const deps = dependencies()
    const handler = createMessageHandler(deps)
    const message = { type: "importJiraTicket", context: { ticketId: "AG-9804" } }
    const first = handler(message, { tab: { id: 8 } })
    const duplicate = handler(message, { tab: { id: 9 } })

    await expect(first).resolves.toEqual({ ok: true, taskUrl: "http://localhost:3080/board?highlight_task=task-1" })
    await expect(duplicate).resolves.toEqual({ ok: false, error: "This Jira ticket is already being imported" })
    expect(deps.importTicket).toHaveBeenCalledWith(DEFAULT_CONFIG, "AG-9804")
    expect(deps.importTicket).toHaveBeenCalledOnce()
    expect(deps.openTask).toHaveBeenCalledWith("http://localhost:3080/board?highlight_task=task-1")
  })

  it("reports Jira import success when task navigation fails", async () => {
    const deps = dependencies()
    deps.openTask.mockRejectedValue(new Error("tabs unavailable"))
    const response = await createMessageHandler(deps)(
      { type: "importJiraTicket", context: { ticketId: "AG-9804" } },
      { tab: { id: 8 } },
    )
    expect(response).toEqual({
      ok: true,
      taskUrl: "http://localhost:3080/board?highlight_task=task-1",
      navigationWarning: "Ticket imported, but Chrome could not open the task. Open Legato to view it.",
    })
  })

  it("rejects invalid Jira ticket context before calling Legato", async () => {
    const deps = dependencies()
    const response = await createMessageHandler(deps)(
      { type: "importJiraTicket", context: { ticketId: "not valid" } },
      { tab: { id: 8 } },
    )
    expect(response).toEqual({ ok: false, error: "Invalid Jira ticket context" })
    expect(deps.importTicket).not.toHaveBeenCalled()
  })

  it("surfaces missing permission as a fixable error", async () => {
    const deps = dependencies()
    deps.hasPermission.mockResolvedValue(false)
    const response = await createMessageHandler(deps)(
      { type: "prepareGitHubWorkflow", context: { repository: "cpave3/legato", pullRequestNumber: 7 } },
      { tab: { id: 4 } },
    )
    expect(response).toEqual({ ok: false, error: "Open extension options and grant access to the configured Legato origin" })
  })
})

describe("discussing a pull request", () => {
  it("prepares the discussion workflow for confirmation", async () => {
    const deps = dependencies()
    const response = await createMessageHandler(deps)({ type: "prepareGitHubWorkflow", purpose: "discuss", context: pr }, { tab: { id: 4 } })

    expect(response).toMatchObject({ ok: true, prepared: { workflowName: "discuss-pr", pullRequestNumber: 7 } })
    expect(deps.start).not.toHaveBeenCalled()
  })

  it("starts the discussion without leaving the pull request", async () => {
    const deps = dependencies()
    deps.start.mockResolvedValue({ run: { id: "run-9", task_id: "task-9" } })

    const response = await createMessageHandler(deps)({ type: "startPreparedWorkflow", purpose: "discuss", context: pr }, { tab: { id: 4 } })

    expect(deps.start).toHaveBeenCalledWith(DEFAULT_CONFIG, expect.objectContaining({ workflowName: "discuss-pr" }))
    expect(response).toEqual({ ok: true, agentUrl: "http://localhost:3080/agents?task=task-9" })
    expect(deps.openRun).not.toHaveBeenCalled()
  })

  it("finds a discussion of this pull request that is still going", async () => {
    const deps = dependencies()
    deps.listRuns.mockResolvedValue([discussionRun("old", "succeeded"), discussionRun("live", "waiting")])

    const response = await createMessageHandler(deps)({ type: "detectGitHubDiscussion", context: pr }, { tab: { id: 4 } })

    expect(response).toEqual({ ok: true, discussion: { runId: "live", taskId: "task-live", agentUrl: "http://localhost:3080/agents?task=task-live" } })
    expect(deps.listRuns).toHaveBeenCalledWith(DEFAULT_CONFIG, "discuss-pr", { repo: "cpave3/legato", pr_number: "7", primary_dir: "/projects/legato" })
  })

  it("reports no discussion when every earlier one has ended", async () => {
    const deps = dependencies()
    deps.listRuns.mockResolvedValue([discussionRun("old", "succeeded"), discussionRun("gone", "cancelled")])

    const response = await createMessageHandler(deps)({ type: "detectGitHubDiscussion", context: pr }, { tab: { id: 4 } })

    expect(response).toEqual({ ok: true, discussion: null })
  })

  it("tells the side panel which discussion belongs to its tab", async () => {
    const deps = dependencies()
    deps.listRuns.mockResolvedValue([discussionRun("live", "waiting")])

    const response = await createMessageHandler(deps)({ type: "getDiscussion", tabId: 4 }, {})

    expect(response).toEqual({ ok: true, discussion: { runId: "live", taskId: "task-live", agentUrl: "http://localhost:3080/agents?task=task-live" } })
    expect(deps.getTabUrl).toHaveBeenCalledWith(4)
  })

  it("discusses the pull request with a finished review's agent while it is still running", async () => {
    const deps = dependencies()
    deps.listRuns.mockImplementation(async (_config: unknown, workflow: string) => workflow === "review-pr"
      ? [reviewRun("reviewing", "running", "review"), reviewRun("reviewed", "waiting", "end")]
      : [discussionRun("live", "waiting")])

    const response = await createMessageHandler(deps)({ type: "detectGitHubDiscussion", context: pr }, { tab: { id: 4 } })

    expect(response).toEqual({ ok: true, discussion: { runId: "reviewed", taskId: "task-reviewed", agentUrl: "http://localhost:3080/agents?task=task-reviewed" } })
    expect(deps.listRuns).toHaveBeenCalledWith(DEFAULT_CONFIG, "review-pr", { repo: "cpave3/legato", pr_number: "7", primary_dir: "/projects/legato" })
  })

  it("starts its own discussion when no review run is waiting on its agent", async () => {
    const deps = dependencies()
    deps.listRuns.mockImplementation(async (_config: unknown, workflow: string) => workflow === "review-pr"
      ? [reviewRun("reviewing", "running", "review"), reviewRun("old", "waiting", "cleanup")]
      : [])

    const response = await createMessageHandler(deps)({ type: "detectGitHubDiscussion", context: pr }, { tab: { id: 4 } })

    expect(response).toEqual({ ok: true, discussion: null })
  })

  it("rejects an unknown purpose", async () => {
    const deps = dependencies()
    const response = await createMessageHandler(deps)({ type: "prepareGitHubWorkflow", purpose: "deploy", context: pr }, { tab: { id: 4 } })

    expect(response).toEqual({ ok: false, error: "Unsupported pull request action" })
  })
})
