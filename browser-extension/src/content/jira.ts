import { createLegatoFab } from "./legato-fab"

export type JiraTicketContext = { ticketId: string }

type MessageResponse = { ok: true; taskUrl: string; navigationWarning?: string } | { ok: false; error: string }
type SendMessage = (message: unknown) => Promise<MessageResponse>

const ACTION_ID = "legato-jira-action"
const JIRA_HOST = "urbanx-development.atlassian.net"

export function parseJiraTicket(href: string): JiraTicketContext | null {
  let url: URL
  try { url = new URL(href) } catch { return null }
  if (url.protocol !== "https:" || url.hostname !== JIRA_HOST) return null
  const match = url.pathname.match(/^\/browse\/([A-Za-z][A-Za-z0-9_]*-\d+)(?:\/|$)/)
  return match ? { ticketId: match[1].toUpperCase() } : null
}

export function reconcileJiraAction(doc: Document, href: string, sendMessage: SendMessage): void {
  const context = parseJiraTicket(href)
  const existing = doc.querySelector<HTMLElement>(`#${ACTION_ID}`)
  if (!context) {
    existing?.remove()
    return
  }
  if (existing?.dataset.ticketId === context.ticketId) return
  existing?.remove()

  const fab = createLegatoFab(doc, {
    id: ACTION_ID,
    title: "Jira ticket import",
    subtitle: context.ticketId,
    actionLabel: `Import ${context.ticketId} to Legato`,
  })
  const { root, button, status, setState } = fab
  root.dataset.ticketId = context.ticketId

  let busy = false
  button.addEventListener("click", () => {
    if (busy) return
    busy = true
    setState("loading", "Importing ticket…")
    void sendMessage({ type: "importJiraTicket", context }).then((response) => {
      if (!response.ok) throw new Error(response.error)
      button.textContent = "Imported to Legato"
      setState("success", response.navigationWarning ?? "Ticket imported and opened in Legato.")
    }).catch((error: unknown) => {
      button.textContent = `Retry import ${context.ticketId}`
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

export function initializeJiraContentScript(doc: Document = document, win: Window = window): () => void {
  let scheduled = false
  const reconcile = (): void => {
    if (scheduled) return
    scheduled = true
    queueMicrotask(() => {
      scheduled = false
      reconcileJiraAction(doc, win.location.href, sendMessage)
    })
  }
  const observer = new MutationObserver(reconcile)
  observer.observe(doc.documentElement, { childList: true, subtree: true })
  win.addEventListener("popstate", reconcile)
  reconcile()
  return () => {
    observer.disconnect()
    win.removeEventListener("popstate", reconcile)
  }
}

if (typeof chrome !== "undefined" && chrome.runtime?.id) initializeJiraContentScript()
