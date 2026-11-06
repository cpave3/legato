import { matchesKeyPattern, normalizeTicketProviders, TICKET_PROVIDERS_KEY, type TicketProvider } from "../ticket-providers"
import { createLegatoFab } from "./legato-fab"

export type TicketContext = { provider: string; ticketId: string }

type MessageResponse = { ok: true; taskUrl?: string; navigationWarning?: string; desktopLink?: string } | { ok: false; error: string }

// Opens a legato-desktop:// link, which the system hands to the desktop app.
// Chrome may ask the first time whether to open it.
function openDesktopLink(doc: Document, link: string): void {
  const a = doc.createElement("a")
  a.href = link
  a.click()
}

const SENT_TO_DESKTOP = "Sent to the Legato desktop app."
type SendMessage = (message: unknown) => Promise<MessageResponse>

const ACTION_ID = "legato-ticket-action"
const INLINE_ID = "legato-ticket-inline"

// How each provider's pages name a ticket, and what the widget calls one.
type ProviderPages = { noun: string; keyIn: (url: URL, doc: Document) => string | null }

const pages: Record<string, ProviderPages> = {
  // https://<site>/browse/AG-9804
  jira: {
    noun: "ticket",
    keyIn: (url) => url.pathname.match(/^\/browse\/([A-Za-z][A-Za-z0-9_]*-\d+)(?:\/|$)/)?.[1].toUpperCase() ?? null,
  },
  // https://<host>/EST-42 (a key link, before Burnlite redirects it), or a
  // project page: /project/<uuid>?item=EST-42 while a task's drawer is open,
  // and without ?item= the open drawer itself.
  burnlite: {
    noun: "task",
    keyIn: (url, doc) => {
      const link = url.pathname.match(/^\/([A-Za-z][A-Za-z0-9_]*-\d+)\/?$/)
      if (link) return link[1]
      if (url.pathname.startsWith("/project/")) {
        const item = url.searchParams.get("item")
        if (item) return item
      }
      return doc.querySelector('[data-testid="task-drawer"] [data-testid="item-key"]')?.textContent?.trim() || null
    },
  },
}

export function providerForOrigin(providers: TicketProvider[], origin: string): TicketProvider | undefined {
  return providers.find((provider) => provider.web_origin === origin && provider.name in pages)
}

// The key of the ticket the page shows, when it is the provider's page and
// the key is one of the provider's.
export function parseTicketKey(provider: TicketProvider, href: string, doc: Document): string | null {
  const reader = pages[provider.name]
  if (!reader) return null
  let url: URL
  try { url = new URL(href) } catch { return null }
  if (url.origin !== provider.web_origin) return null
  const key = reader.keyIn(url, doc)
  return key && matchesKeyPattern(provider, key) ? key : null
}

export function reconcileTicketAction(doc: Document, href: string, provider: TicketProvider, sendMessage: SendMessage): void {
  // Burnlite shows a task in a modal drawer that closes on a press outside
  // it, so its import is a button inside the drawer instead of a bubble.
  if (provider.name === "burnlite") {
    doc.querySelector(`#${ACTION_ID}`)?.remove()
    reconcileDrawerButton(doc, provider, sendMessage)
    return
  }
  const ticketId = parseTicketKey(provider, href, doc)
  const existing = doc.querySelector<HTMLElement>(`#${ACTION_ID}`)
  // An import in flight keeps its widget, so its outcome is seen, whatever
  // the page does meanwhile (a drawer that closes takes its key away).
  if (existing?.dataset.importing === "true") return
  if (!ticketId) {
    existing?.remove()
    return
  }
  if (existing?.dataset.ticketId === ticketId && existing.dataset.provider === provider.name) return
  existing?.remove()

  const context: TicketContext = { provider: provider.name, ticketId }
  const fab = createLegatoFab(doc, {
    id: ACTION_ID,
    title: `${provider.label} ${pages[provider.name].noun} import`,
    subtitle: ticketId,
    actionLabel: `Import ${ticketId} to Legato`,
  })
  const { root, button, status, setState } = fab
  root.dataset.ticketId = ticketId
  root.dataset.provider = provider.name

  let busy = false
  button.addEventListener("click", () => {
    if (busy) return
    busy = true
    root.dataset.importing = "true"
    setState("loading", "Importing ticket…")
    void sendMessage({ type: "importTicket", context }).then((response) => {
      if (!response.ok) throw new Error(response.error)
      if (response.desktopLink) {
        openDesktopLink(doc, response.desktopLink)
        button.textContent = `Import ${ticketId} to Legato`
        setState("success", SENT_TO_DESKTOP)
        return
      }
      button.textContent = "Imported to Legato"
      setState("success", response.navigationWarning ?? "Ticket imported and opened in Legato.")
    }).catch((error: unknown) => {
      button.textContent = `Retry import ${ticketId}`
      setState("error", error instanceof Error ? error.message : String(error))
    }).finally(() => {
      busy = false
      delete root.dataset.importing
      if (status.dataset.state === "loading") setState("idle", "")
    })
  })

  doc.body.append(root)
}

// The task or milestone the open Burnlite drawer shows. A milestone's key is
// from the same series as its project's task keys, and imports the same way.
function burnliteDrawer(doc: Document, provider: TicketProvider): { drawer: Element; key: string } | null {
  const drawer = doc.querySelector('[data-testid="task-drawer"], [data-testid="milestone-drawer"]')
  const key = drawer?.querySelector('[data-testid="item-key"]')?.textContent?.trim()
  if (!drawer || !key || !matchesKeyPattern(provider, key)) return null
  return { drawer, key }
}

function reconcileDrawerButton(doc: Document, provider: TicketProvider, sendMessage: SendMessage): void {
  const found = burnliteDrawer(doc, provider)
  const existing = doc.querySelector<HTMLElement>(`#${INLINE_ID}`)
  if (existing?.dataset.importing === "true" && existing.isConnected) return
  if (!found) {
    existing?.remove()
    return
  }
  if (existing && existing.dataset.ticketId === found.key && found.drawer.contains(existing)) return
  existing?.remove()

  const ticketId = found.key
  const copy = found.drawer.querySelector<HTMLElement>('[data-testid="copy-task-reference"], [data-testid="copy-milestone-reference"]')
  const keyElement = found.drawer.querySelector('[data-testid="item-key"]')!

  const wrap = doc.createElement("span")
  wrap.id = INLINE_ID
  wrap.dataset.ticketId = ticketId
  wrap.style.cssText = "display:inline-flex;align-items:center;gap:6px"
  const button = doc.createElement("button")
  button.type = "button"
  // Burnlite's own look, from the Copy reference button beside it.
  if (copy) button.className = copy.className
  else button.style.cssText = "font:inherit;font-size:12px;padding:4px 10px;border-radius:8px;cursor:pointer"
  button.title = `Import ${ticketId} to Legato`
  const label = (text: string) => { button.textContent = `♩ ${text}` }
  label("Import to Legato")
  const status = doc.createElement("span")
  status.setAttribute("role", "status")
  status.style.cssText = "font-size:12px;max-width:260px"
  wrap.append(button, status)

  const say = (text: string, error = false) => {
    status.textContent = text
    status.style.color = error ? "#e5484d" : ""
  }
  button.addEventListener("click", () => {
    if (wrap.dataset.importing === "true") return
    wrap.dataset.importing = "true"
    button.disabled = true
    label("Importing…")
    say("")
    const context: TicketContext = { provider: provider.name, ticketId }
    void sendMessage({ type: "importTicket", context }).then((response) => {
      if (!response.ok) throw new Error(response.error)
      if (response.desktopLink) {
        openDesktopLink(doc, response.desktopLink)
        label("Import to Legato")
        say(SENT_TO_DESKTOP)
        return
      }
      label("Imported to Legato")
      say(response.navigationWarning ?? "")
    }).catch((error: unknown) => {
      label("Retry import")
      say(error instanceof Error ? error.message : String(error), true)
    }).finally(() => {
      delete wrap.dataset.importing
      button.disabled = false
    })
  })

  if (copy?.parentElement) copy.parentElement.insertBefore(wrap, copy)
  else keyElement.parentElement?.append(wrap)
}

function sendMessage(message: unknown): Promise<MessageResponse> {
  return chrome.runtime.sendMessage(message) as Promise<MessageResponse>
}

async function storedProviders(): Promise<TicketProvider[]> {
  const stored = await chrome.storage.local.get(TICKET_PROVIDERS_KEY)
  return normalizeTicketProviders(stored[TICKET_PROVIDERS_KEY])
}

// The script is registered for each provider's site (see ticket-sites.ts), so
// it finds which provider this page belongs to from the list the extension
// stored. Burnlite moves ?item= with history.replaceState, which fires no
// event; the drawer that opens with it changes the page, so a change to the
// page is when to look again.
export async function initializeTicketContentScript(doc: Document = document, win: Window = window): Promise<() => void> {
  const provider = providerForOrigin(await storedProviders().catch(() => []), win.location.origin)
  if (!provider) return () => undefined
  let scheduled = false
  const reconcile = (): void => {
    if (scheduled) return
    scheduled = true
    queueMicrotask(() => {
      scheduled = false
      reconcileTicketAction(doc, win.location.href, provider, sendMessage)
    })
  }
  const observer = new MutationObserver(reconcile)
  observer.observe(doc.documentElement, { childList: true, subtree: true, characterData: true })
  win.addEventListener("popstate", reconcile)
  reconcile()
  return () => {
    observer.disconnect()
    win.removeEventListener("popstate", reconcile)
  }
}

if (typeof chrome !== "undefined" && chrome.runtime?.id) void initializeTicketContentScript()
