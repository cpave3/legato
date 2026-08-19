import { renderSafeMarkdown } from "./markdown"
import type { ReviewPanelModel } from "./review-model"

export function renderReviewPanelView(doc: Document, model: ReviewPanelModel): void {
  const contextElement = doc.querySelector<HTMLElement>("#context")!
  const stateElement = doc.querySelector<HTMLElement>("#state")!
  const runLabel = doc.querySelector<HTMLElement>("#run-label")!
  const runSelect = doc.querySelector<HTMLSelectElement>("#run-select")!
  const details = doc.querySelector<HTMLElement>("#details")!
  const badge = doc.querySelector<HTMLElement>("#status-badge")!
  const step = doc.querySelector<HTMLElement>("#step")!
  const error = doc.querySelector<HTMLElement>("#error")!
  const content = doc.querySelector<HTMLElement>("#review-content")!
  const openRun = doc.querySelector<HTMLAnchorElement>("#open-run")!

  contextElement.textContent = `${model.context.repository} · PR #${model.context.pullRequestNumber}`
  runSelect.replaceChildren(...model.runs.map((run) => {
    const option = doc.createElement("option")
    option.value = run.id
    option.textContent = `${run.status} · ${new Date(run.created_at).toLocaleString()}`
    option.selected = run.id === model.selectedRunId
    return option
  }))
  runLabel.hidden = model.runs.length === 0
  details.hidden = model.runs.length === 0
  stateElement.hidden = model.runs.length > 0
  content.replaceChildren()
  error.style.display = "none"

  if (model.state === "empty") {
    stateElement.textContent = model.emptyMessage ?? "No matching review run. Start the configured workflow from the Legato action on this pull request."
    return
  }
  badge.textContent = model.state === "ready" ? "Review ready" : model.status.replaceAll("_", " ")
  step.textContent = model.run?.current_step_id ? `Current step: ${model.run.current_step_id}` : ""
  openRun.href = model.runUrl ?? "#"
  if (model.state === "ready" && model.markdown !== undefined) {
    renderSafeMarkdown(content, model.markdown)
  } else if (model.state === "failed") {
    error.textContent = model.error || "The review workflow failed."
    error.style.display = "block"
  } else if (model.state === "missing_artifact") {
    error.textContent = "The workflow completed without a readable Markdown review artifact."
    error.style.display = "block"
  } else {
    content.textContent = "Waiting for the Markdown review artifact. This panel will refresh automatically."
  }
}
