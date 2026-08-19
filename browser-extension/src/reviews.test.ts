import { describe, expect, it } from "vitest"
import { buildReviewLookup, selectReviewArtifact, summarizeReviewRun } from "./reviews"
import { DEFAULT_CONFIG } from "./config"
import type { WorkflowArtifact, WorkflowRun, WorkflowRunView } from "./legato"

const run: WorkflowRun = {
  id: "run-1", workflow_name: "review-pr", workflow_title: "Review PR", inputs_json: "{}",
  status: "waiting_manual", current_step_id: "cleanup", created_at: "2026-08-19T01:00:00Z",
}
const artifact = (id: string, outputKey: string, mediaType = "text/markdown"): WorkflowArtifact => ({
  id, run_id: "run-1", step_id: "review", attempt_id: "attempt-1", output_key: outputKey,
  original_path: "review.md", media_type: mediaType, size_bytes: 100, digest: id, created_at: "2026-08-19T01:10:00Z",
})

describe("buildReviewLookup", () => {
  it("uses configured workflow/input names and canonical preset repository", () => {
    expect(buildReviewLookup(DEFAULT_CONFIG, { repository: "CPAVE3/LEGATO", pullRequestNumber: 42 }, {
      definitions: [], errors: [], repositories: [{ name: "Legato", repo: "cpave3/legato", primary_dir: "/projects/legato" }],
    })).toEqual({ workflowName: "review-pr", inputs: { repo: "cpave3/legato", pr_number: "42" } })
  })

  it("rejects missing and ambiguous presets", () => {
    const context = { repository: "cpave3/legato", pullRequestNumber: 42 }
    expect(() => buildReviewLookup(DEFAULT_CONFIG, context, { definitions: [], errors: [], repositories: [] })).toThrow("No Legato repository preset")
    expect(() => buildReviewLookup(DEFAULT_CONFIG, context, { definitions: [], errors: [], repositories: [
      { name: "One", repo: "cpave3/legato", primary_dir: "/one" }, { name: "Two", repo: "CPAVE3/LEGATO", primary_dir: "/two" },
    ] })).toThrow("Multiple Legato repository presets")
  })
})

describe("selectReviewArtifact", () => {
  it("prefers the configured Markdown output key", () => {
    expect(selectReviewArtifact([artifact("other", "notes"), artifact("review", "review")], "review")?.id).toBe("review")
  })

  it("falls back only to one unambiguous Markdown artifact", () => {
    expect(selectReviewArtifact([artifact("only", "result")], "review")?.id).toBe("only")
    expect(() => selectReviewArtifact([artifact("one", "one"), artifact("two", "two")], "review")).toThrow("Multiple Markdown artifacts")
    expect(selectReviewArtifact([artifact("plain", "review", "text/plain")], "review")).toBeNull()
  })
})

describe("summarizeReviewRun", () => {
  it("treats artifact presence as review-ready independent of workflow status", () => {
    const view: WorkflowRunView = { run, steps: [], attempts: [], artifacts: [artifact("review", "review")] }
    expect(summarizeReviewRun(view, "review")).toMatchObject({ state: "ready", status: "waiting_manual", artifactId: "review" })
  })

  it("does not call succeeded-without-artifact readable", () => {
    const view: WorkflowRunView = { run: { ...run, status: "succeeded" }, steps: [], attempts: [], artifacts: [] }
    expect(summarizeReviewRun(view, "review")).toMatchObject({ state: "missing_artifact", status: "succeeded" })
  })
})
