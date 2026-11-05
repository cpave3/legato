import { matchesKeyPattern, normalizeTicketProviders, TICKET_PROVIDERS_KEY, type TicketProvider } from "../ticket-providers"
import { createLegatoFab } from "./legato-fab"

export type TicketContext = { provider: string; ticketId: string }

type MessageResponse = { ok: true; taskUrl: string; navigationWarning?: string } | { ok: false; error: string }
type SendMessage = (message: unknown) => Promise<MessageResponse>

const ACTION_ID = "legato-ticket-action"

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
  const ticketId = parseTicketKey(provider, href, doc)
  const existing = doc.querySelector<HTMLElement>(`#${ACTION_ID}`)
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
    setState("loading", "Importing ticket…")
    void sendMessage({ type: "importTicket", context }).then((response) => {
      if (!response.ok) throw new Error(response.error)
      button.textContent = "Imported to Legato"
      setState("success", response.navigationWarning ?? "Ticket imported and opened in Legato.")
    }).catch((error: unknown) => {
      button.textContent = `Retry import ${ticketId}`
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
