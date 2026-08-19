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
  }
}

describe("GitHub workflow messages", () => {
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
