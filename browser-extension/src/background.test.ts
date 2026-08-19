import { describe, expect, it, vi } from "vitest"
import { DEFAULT_CONFIG } from "./config"
import { createMessageHandler } from "./background"
import type { WorkflowDiscovery } from "./legato"

const discovery: WorkflowDiscovery = {
  definitions: [{ name: "review-pr", inputs: {
    repo: { type: "string" }, pr_number: { type: "integer" }, primary_dir: { type: "string" },
  } }],
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
    saveReviewContext: vi.fn().mockResolvedValue(undefined),
    loadReviewModel: vi.fn().mockResolvedValue({ state: "empty", status: "", context: { repository: "cpave3/legato", pullRequestNumber: 7 }, runs: [], poll: false }),
  }
}

describe("GitHub workflow messages", () => {
  it("loads a review model for an explicit panel tab and run selection", async () => {
    const deps = dependencies()
    const response = await createMessageHandler(deps)({ type: "getReviewModel", tabId: 4, selectedRunId: "old" }, {})
    expect(response).toMatchObject({ ok: true, review: { state: "empty" } })
    expect(deps.loadReviewModel).toHaveBeenCalledWith(4, "old")
  })

  it("stores tab-scoped review context without querying Legato", async () => {
    const deps = dependencies()
    const context = { repository: "cpave3/legato", pullRequestNumber: 7 }
    const response = await createMessageHandler(deps)({ type: "setReviewContext", context }, { tab: { id: 4 } })

    expect(response).toEqual({ ok: true })
    expect(deps.saveReviewContext).toHaveBeenCalledWith(4, context)
    expect(deps.discover).not.toHaveBeenCalled()
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
