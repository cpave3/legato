import { describe, expect, it, vi } from "vitest"
import { loadReviewModel } from "./review-model"
import { DEFAULT_CONFIG } from "./config"
import type { WorkflowDiscovery, WorkflowRun, WorkflowRunView } from "./legato"

const context = { repository: "cpave3/legato", pullRequestNumber: 42 }
const discovery: WorkflowDiscovery = { definitions: [], errors: [], repositories: [{ name: "Legato", repo: "cpave3/legato", primary_dir: "/repo" }] }
const runs: WorkflowRun[] = [
  { id: "new", workflow_name: "review-pr", workflow_title: "Review PR", inputs_json: "{\"repo\":\"cpave3/legato\",\"pr_number\":42}", status: "running", current_step_id: "review", created_at: "2026-08-19T02:00:00Z" },
  { id: "old", workflow_name: "review-pr", workflow_title: "Review PR", inputs_json: "{\"repo\":\"cpave3/legato\",\"pr_number\":42}", status: "waiting_manual", current_step_id: "cleanup", created_at: "2026-08-19T01:00:00Z" },
]

function deps(view: WorkflowRunView) {
  return {
    loadConfig: vi.fn().mockResolvedValue(DEFAULT_CONFIG),
    hasPermission: vi.fn().mockResolvedValue(true),
    loadContext: vi.fn().mockResolvedValue(context),
    discover: vi.fn().mockResolvedValue(discovery),
    listRuns: vi.fn().mockResolvedValue(runs),
    getRun: vi.fn().mockResolvedValue(view),
    getArtifact: vi.fn().mockResolvedValue("# Review"),
  }
}

describe("loadReviewModel", () => {
  it("selects the newest run by default and leaves an active run polling", async () => {
    const dependencies = deps({ run: runs[0], steps: [{ step_id: "review", title: "Review", status: "running" }], attempts: [], artifacts: [] })
    const model = await loadReviewModel(4, undefined, dependencies)
    expect(model).toMatchObject({ context, selectedRunId: "new", state: "active", poll: true })
    expect(model.markdown).toBeUndefined()
    expect(model.runs.map((run) => run.id)).toEqual(["new", "old"])
    expect(dependencies.listRuns).toHaveBeenCalledWith(DEFAULT_CONFIG, "review-pr", { repo: "cpave3/legato", pr_number: "42" })
  })

  it("loads a selected historical review artifact and does not poll", async () => {
    const dependencies = deps({ run: runs[1], steps: [], attempts: [], artifacts: [{
      id: "artifact-1", run_id: "old", step_id: "review", attempt_id: "a", output_key: "review",
      media_type: "text/markdown", size_bytes: 20, digest: "d", created_at: "2026-08-19T01:10:00Z",
    }] })
    const model = await loadReviewModel(4, "old", dependencies)
    expect(model).toMatchObject({ selectedRunId: "old", state: "ready", poll: false, markdown: "# Review", artifactId: "artifact-1" })
  })

  it("defensively rejects unrelated runs from an older server that ignores filters", async () => {
    const dependencies = deps({ run: runs[0] })
    dependencies.listRuns.mockResolvedValue([{ ...runs[0], workflow_name: "other" }, { ...runs[1], inputs_json: "{\"repo\":\"cpave3/legato\",\"pr_number\":99}" }])
    const model = await loadReviewModel(4, undefined, dependencies)
    expect(model).toMatchObject({
      state: "empty", runs: [],
      emptyMessage: "No review runs matched workflow review-pr with repo=cpave3/legato, pr_number=42. Check the extension options or start a review from this pull request.",
    })
    expect(dependencies.getRun).not.toHaveBeenCalled()
  })

  it("returns an empty state without fetching run details", async () => {
    const dependencies = deps({ run: runs[0] })
    dependencies.listRuns.mockResolvedValue([])
    const model = await loadReviewModel(4, undefined, dependencies)
    expect(model).toMatchObject({ state: "empty", runs: [], poll: false })
    expect(dependencies.getRun).not.toHaveBeenCalled()
  })

  it("rejects a selected run outside the matched history", async () => {
    await expect(loadReviewModel(4, "other", deps({ run: runs[0] }))).rejects.toThrow("Selected run does not match")
  })
})
