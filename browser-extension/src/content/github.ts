import type { PreparedWorkflow } from "../actions"
import type { ReviewPanelModel } from "../review-model"
import { parseGitHubPullRequestUrl } from "../review-context"
import { createLegatoFab } from "./legato-fab"

type MessageResponse = { ok: true; prepared?: PreparedWorkflow; runUrl?: string; navigationWarning?: string; review?: ReviewPanelModel } | { ok: false; error: string }
type SendMessage = (message: unknown) => Promise<MessageResponse>

const ACTION_ID = "legato-pr-action"

export const parseGitHubPullRequest = parseGitHubPullRequestUrl

export function reconcileGitHubAction(doc: Document, href: string, sendMessage: SendMessage): void {
  const context = parseGitHubPullRequest(href)
  const existing = doc.querySelector<HTMLElement>(`#${ACTION_ID}`)
  if (!context) {
    existing?.remove()
    return
  }
  const key = `${context.repository}#${context.pullRequestNumber}`
  if (existing?.dataset.context === key) return
  existing?.remove()

  const fab = createLegatoFab(doc, {
    id: ACTION_ID,
    title: "Pull request review",
    subtitle: `${context.repository} · #${context.pullRequestNumber}`,
    actionLabel: "Prepare Legato review",
  })
  const { root, button, status, setPrimaryVisible, setState } = fab
  const viewReview = fab.addSecondaryAction("View review")
  setPrimaryVisible(false)
  viewReview.setAttribute("aria-label", `View Legato review for ${context.repository} pull request ${context.pullRequestNumber}`)
  viewReview.addEventListener("click", () => {
    void sendMessage({ type: "openReviewPanel", context }).then((response) => {
      if (!response.ok) throw new Error(response.error)
    }).catch((error: unknown) => setState("error", error instanceof Error ? error.message : String(error)))
  })
  root.dataset.context = key
  void sendMessage({ type: "detectGitHubReview", context }).then((response) => {
    if (!response.ok) throw new Error(response.error)
    setPrimaryVisible(response.review?.runs.length === 0)
  }).catch((error: unknown) => {
    setPrimaryVisible(false)
    setState("error", error instanceof Error ? error.message : String(error))
  })

  let busy = false
  let prepared: PreparedWorkflow | undefined
  button.addEventListener("click", () => {
    if (busy) return
    busy = true
    setState("loading", prepared ? "Starting workflow…" : "Resolving workflow and repository…")
    const message = prepared
      ? { type: "startPreparedWorkflow", context }
      : { type: "prepareGitHubWorkflow", context }
    void sendMessage(message).then((response) => {
      if (!response.ok) throw new Error(response.error)
      if (response.prepared) {
        prepared = response.prepared
        button.textContent = "Confirm and start"
        setState("idle", [
          `Workflow: ${prepared.workflowName}`,
          `Repository: ${prepared.repository} (${prepared.repositoryPresetName})`,
          `Checkout: ${prepared.primaryDir}`,
        ].join("\n"))
      } else {
        button.textContent = "Started in Legato"
        setState("success", response.navigationWarning ?? "Workflow started.")
      }
    }).catch((error: unknown) => {
      button.textContent = prepared ? "Confirm and start" : "Retry Legato review"
      setState("error", error instanceof Error ? error.message : String(error))
    }).finally(() => {
      busy = false
      if (status.dataset.state === "loading") setState("idle", "")
    })
  })

  doc.body.append(root)
}

function sendMessage(message: unknown): Promise<MessageResponse> {
  return chrome.runtime.sendMessage(message) as Promise<MessageResponse>
}

if (typeof chrome !== "undefined" && chrome.runtime?.id) {
  let scheduled = false
  const reconcile = (): void => {
    if (scheduled) return
    scheduled = true
    queueMicrotask(() => {
      scheduled = false
      reconcileGitHubAction(document, location.href, sendMessage)
    })
  }
  document.addEventListener("turbo:load", reconcile)
  document.addEventListener("pjax:end", reconcile)
  new MutationObserver(reconcile).observe(document.documentElement, { childList: true, subtree: true })
  reconcile()
}
