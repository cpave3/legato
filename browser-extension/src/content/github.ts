import type { GitHubPullRequestContext, PreparedWorkflow } from "../actions"

type MessageResponse = { ok: true; prepared?: PreparedWorkflow; runUrl?: string; navigationWarning?: string } | { ok: false; error: string }
type SendMessage = (message: unknown) => Promise<MessageResponse>

const ACTION_ID = "legato-pr-action"

export function parseGitHubPullRequest(href: string): GitHubPullRequestContext | null {
  let url: URL
  try { url = new URL(href) } catch { return null }
  if (url.protocol !== "https:" || url.hostname !== "github.com") return null
  const match = url.pathname.match(/^\/([^/]+)\/([^/]+)\/pull\/([1-9]\d*)(?:\/|$)/)
  if (!match) return null
  return { repository: `${match[1]}/${match[2]}`, pullRequestNumber: Number(match[3]) }
}

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

  const root = doc.createElement("div")
  root.id = ACTION_ID
  root.dataset.context = key
  root.style.cssText = "display:inline-flex;flex-direction:column;gap:6px;margin:4px 8px;vertical-align:top;max-width:520px"
  const button = doc.createElement("button")
  button.type = "button"
  button.textContent = "Prepare Legato review"
  button.className = "Button--secondary Button--small Button"
  const status = doc.createElement("span")
  status.setAttribute("role", "status")
  status.setAttribute("aria-live", "polite")
  status.style.cssText = "font-size:12px;white-space:pre-line"
  root.append(button, status)

  let busy = false
  let prepared: PreparedWorkflow | undefined
  button.addEventListener("click", () => {
    if (busy) return
    busy = true
    button.disabled = true
    status.textContent = prepared ? "Starting workflow…" : "Resolving workflow and repository…"
    const message = prepared
      ? { type: "startPreparedWorkflow", context }
      : { type: "prepareGitHubWorkflow", context }
    void sendMessage(message).then((response) => {
      if (!response.ok) throw new Error(response.error)
      if (response.prepared) {
        prepared = response.prepared
        button.textContent = "Confirm and start"
        status.textContent = [
          `Workflow: ${prepared.workflowName}`,
          `Repository: ${prepared.repository} (${prepared.repositoryPresetName})`,
          `PR: #${prepared.pullRequestNumber}`,
          `Checkout: ${prepared.primaryDir}`,
        ].join("\n")
      } else {
        button.textContent = "Started in Legato"
        status.textContent = response.navigationWarning ?? "Workflow started."
      }
    }).catch((error: unknown) => {
      status.textContent = error instanceof Error ? error.message : String(error)
      button.textContent = prepared ? "Confirm and start" : "Retry Legato review"
    }).finally(() => {
      busy = false
      button.disabled = false
    })
  })

  const target = doc.querySelector('[data-testid="issue-metadata-fixed"]')
    ?? doc.querySelector(".gh-header-actions")
    ?? doc.querySelector("main")
  target?.append(root)
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
