// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from "vitest"
import { renderReviewPanelView } from "./sidepanel-view"
import type { ReviewPanelModel } from "./review-model"

beforeEach(() => {
  document.body.innerHTML = `
    <p id="context"></p><div id="state"></div><label id="run-label"><select id="run-select"></select></label>
    <section id="details"><span id="status-badge"></span><span id="step"></span><div id="error"></div>
    <article id="review-content"></article><a id="open-run"></a></section>
  `
})

const base: ReviewPanelModel = {
  context: { repository: "cpave3/legato", pullRequestNumber: 42 },
  runs: [], state: "empty", status: "", poll: false,
}

describe("renderReviewPanelView", () => {
  it("renders the no-run state", () => {
    renderReviewPanelView(document, base)
    expect(document.querySelector("#state")?.textContent).toContain("No matching review run")
    expect((document.querySelector("#run-label") as HTMLElement).hidden).toBe(true)
    expect((document.querySelector("#details") as HTMLElement).hidden).toBe(true)
  })

  it("renders waiting-manual Markdown as ready and sanitizes it", () => {
    renderReviewPanelView(document, {
      ...base, state: "ready", status: "waiting_manual", selectedRunId: "run-1", artifactId: "a", markdown: "# Review\n<script>alert(1)</script>",
      run: { id: "run-1", workflow_name: "review-pr", workflow_title: "Review", inputs_json: "{}", status: "waiting_manual", current_step_id: "cleanup", created_at: "2026-08-19T01:00:00Z" },
      runs: [{ id: "run-1", workflow_name: "review-pr", workflow_title: "Review", inputs_json: "{}", status: "waiting_manual", current_step_id: "cleanup", created_at: "2026-08-19T01:00:00Z" }],
      runUrl: "http://localhost:3080/workflows/runs/run-1",
    })
    expect(document.querySelector("#status-badge")?.textContent).toBe("Review ready")
    expect(document.querySelector("#review-content h1")?.textContent).toBe("Review")
    expect(document.querySelector("#review-content script")).toBeNull()
  })

  it("renders active and failed states without Markdown", () => {
    const run = { id: "run-1", workflow_name: "review-pr", workflow_title: "Review", inputs_json: "{}", status: "running", current_step_id: "review", created_at: "2026-08-19T01:00:00Z" }
    renderReviewPanelView(document, { ...base, state: "active", status: "running", selectedRunId: "run-1", runs: [run], run, poll: true })
    expect(document.querySelector("#review-content")?.textContent).toContain("refresh automatically")
    renderReviewPanelView(document, { ...base, state: "failed", status: "failed", selectedRunId: "run-1", runs: [{ ...run, status: "failed" }], run: { ...run, status: "failed" }, error: "agent failed", poll: false })
    expect(document.querySelector("#error")?.textContent).toBe("agent failed")
  })
})
