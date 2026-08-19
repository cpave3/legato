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

  const root = doc.createElement("div")
  root.id = ACTION_ID
  root.dataset.ticketId = context.ticketId
  root.style.cssText = [
    "position:fixed", "right:24px", "bottom:24px", "z-index:2147483647",
    "display:flex", "flex-direction:column", "gap:8px", "width:min(320px,calc(100vw - 48px))",
    "padding:12px", "border:1px solid #dfe1e6", "border-radius:8px", "background:#fff",
    "color:#172b4d", "box-shadow:0 8px 24px rgba(9,30,66,.25)",
  ].join(";")
  const button = doc.createElement("button")
  button.type = "button"
  button.textContent = `Import ${context.ticketId} to Legato`
  button.style.cssText = "padding:8px 12px;border:0;border-radius:4px;background:#0c66e4;color:#fff;font:600 14px system-ui;cursor:pointer"
  const status = doc.createElement("span")
  status.setAttribute("role", "status")
  status.setAttribute("aria-live", "polite")
  status.style.cssText = "font:12px/1.4 system-ui;white-space:pre-line"
  root.append(button, status)

  let busy = false
  button.addEventListener("click", () => {
    if (busy) return
    busy = true
    button.disabled = true
    status.textContent = "Importing ticket…"
    void sendMessage({ type: "importJiraTicket", context }).then((response) => {
      if (!response.ok) throw new Error(response.error)
      button.textContent = "Imported to Legato"
      status.textContent = response.navigationWarning ?? "Ticket imported and opened in Legato."
    }).catch((error: unknown) => {
      button.textContent = `Retry import ${context.ticketId}`
      status.textContent = error instanceof Error ? error.message : String(error)
    }).finally(() => {
      busy = false
      button.disabled = false
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
