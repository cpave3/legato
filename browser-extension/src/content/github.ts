import type { PreparedWorkflow, PullRequestPurpose } from "../actions"
import type { ReviewPanelModel } from "../review-model"
import { parseGitHubPullRequestUrl } from "../review-context"
import { reconcileAskButtons } from "./ask-legato"
import { createLegatoFab, type LegatoFab } from "./legato-fab"
import { ensureFilesPage, jumpToFile, resumePendingJump, type JumpTarget } from "./pr-jump"

type MessageResponse = {
  ok: true; prepared?: PreparedWorkflow; runUrl?: string; agentUrl?: string; navigationWarning?: string
  review?: ReviewPanelModel; discussion?: { runId: string; agentUrl: string } | null
} | { ok: false; error: string }
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
    title: "Pull request",
    subtitle: `${context.repository} · #${context.pullRequestNumber}`,
    actionLabel: "Prepare Legato review",
  })
  const { root, button, setPrimaryVisible, setState } = fab
  const viewReview = fab.addSecondaryAction("View review")
  const discuss = fab.addSecondaryAction("Discuss PR")
  setPrimaryVisible(false)
  viewReview.setAttribute("aria-label", `View Legato review for ${context.repository} pull request ${context.pullRequestNumber}`)
  discuss.setAttribute("aria-label", `Discuss ${context.repository} pull request ${context.pullRequestNumber} with a Legato agent`)
  // The side panel opens only in answer to a click, so it is asked for
  // before anything else the click does.
  const openPanel = (view: "review" | "discussion"): void => {
    void sendMessage({ type: "openReviewPanel", view, context }).then((response) => {
      if (!response.ok) throw new Error(response.error)
    }).catch((error: unknown) => setState("error", errorText(error)))
  }
  viewReview.addEventListener("click", () => openPanel("review"))
  root.dataset.context = key
  void sendMessage({ type: "detectGitHubReview", context }).then((response) => {
    if (!response.ok) throw new Error(response.error)
    setPrimaryVisible(response.review?.runs.length === 0)
  }).catch((error: unknown) => {
    setPrimaryVisible(false)
    setState("error", errorText(error))
  })

  const flow = { busy: false }
  startsWithConfirmation(button, { purpose: "review", retry: "Retry Legato review" }, context, sendMessage, fab, flow, {
    onStarted: () => { button.textContent = "Started in Legato" },
  })

  // A discussion that is going is opened, never started again.
  let discussionOpen = false
  const offerOpenDiscussion = (): void => {
    discussionOpen = true
    discuss.textContent = "Open discussion"
    discuss.setAttribute("aria-label", `Open the Legato discussion of ${context.repository} pull request ${context.pullRequestNumber}`)
  }
  void sendMessage({ type: "detectGitHubDiscussion", context }).then((response) => {
    if (response.ok && response.discussion) offerOpenDiscussion()
  }).catch(() => undefined)
  discuss.addEventListener("click", (event) => {
    if (!discussionOpen) return
    event.stopImmediatePropagation()
    openPanel("discussion")
  })
  startsWithConfirmation(discuss, { purpose: "discuss", retry: "Retry discussion" }, context, sendMessage, fab, flow, {
    onConfirm: () => openPanel("discussion"),
    onStarted: () => {
      offerOpenDiscussion()
      fab.setEmphasis(button)
    },
  })

  doc.body.append(root)
}

// Prepares a workflow on the first click and shows what it resolved to; the
// run starts only on a second click that confirms it. One flow runs at a time.
function startsWithConfirmation(
  trigger: HTMLButtonElement,
  options: { purpose: PullRequestPurpose; retry: string },
  context: { repository: string; pullRequestNumber: number },
  sendMessage: SendMessage,
  fab: LegatoFab,
  flow: { busy: boolean },
  hooks: { onConfirm?: () => void; onStarted: () => void },
): void {
  let prepared: PreparedWorkflow | undefined
  trigger.addEventListener("click", () => {
    if (flow.busy) return
    flow.busy = true
    if (prepared) hooks.onConfirm?.()
    fab.setState("loading", prepared ? "Starting workflow…" : "Resolving workflow and repository…")
    const message = { type: prepared ? "startPreparedWorkflow" : "prepareGitHubWorkflow", purpose: options.purpose, context }
    void sendMessage(message).then((response) => {
      if (!response.ok) throw new Error(response.error)
      if (response.prepared) {
        prepared = response.prepared
        trigger.textContent = "Confirm and start"
        fab.setEmphasis(trigger)
        fab.setState("idle", [
          `Workflow: ${prepared.workflowName}`,
          `Repository: ${prepared.repository} (${prepared.repositoryPresetName})`,
          `Checkout: ${prepared.primaryDir}`,
        ].join("\n"))
      } else {
        prepared = undefined
        hooks.onStarted()
        fab.setState("success", response.navigationWarning ?? (options.purpose === "discuss" ? "Discussion started. It is in the side panel." : "Workflow started."))
      }
    }).catch((error: unknown) => {
      trigger.textContent = prepared ? "Confirm and start" : options.retry
      fab.setState("error", errorText(error))
    }).finally(() => {
      flow.busy = false
      if (fab.status.dataset.state === "loading") fab.setState("idle", "")
    })
  })
}

function errorText(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
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
      reconcileAskButtons(document, location.href, () => location.hash, sendMessage)
    })
  }
  document.addEventListener("turbo:load", reconcile)
  document.addEventListener("pjax:end", reconcile)
  new MutationObserver(reconcile).observe(document.documentElement, { childList: true, subtree: true })
  reconcile()

  // The side panel asks to show a file an agent cited.
  chrome.runtime.onMessage.addListener((message: unknown, _sender, respond) => {
    const target = jumpTarget(message)
    if (!target) return
    void (async () => {
      if (!(await ensureFilesPage(window, target))) return { ok: true, status: "navigating", message: "Opening Files changed…" }
      return jumpToFile(window, target)
    })().then(respond, (error: unknown) => respond({ ok: false, message: errorText(error) }))
    return true
  })
  void resumePendingJump(window).then((result) => {
    if (result) void chrome.runtime.sendMessage({ type: "legato:jumpResult", result }).catch(() => undefined)
  })
}

function jumpTarget(message: unknown): JumpTarget | null {
  const candidate = message as { type?: unknown; path?: unknown; line?: unknown }
  if (candidate?.type !== "legato:jump" || typeof candidate.path !== "string" || !candidate.path) return null
  return Number.isSafeInteger(candidate.line) && (candidate.line as number) > 0
    ? { path: candidate.path, line: candidate.line as number }
    : { path: candidate.path }
}
