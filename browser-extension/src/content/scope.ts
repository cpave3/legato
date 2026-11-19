import { clearedKey, createTabClearedStore, createTabDraftStore, draftsKey } from "../scope-drafts"
import type { ScopeBatchResult, ScopePin, SentPin } from "../scope-types"
import { normalizeScopeOrigins, SCOPE_ORIGINS_KEY } from "../scope-origins"
import { capturePage, captureTarget, locateTarget } from "./scope-capture"
import { lookupSource } from "./scope-source"
import { captureStyles, createStyleReader } from "./scope-styles"
import { createScopeOverlay, type CapturedPin, type ScopeOverlay } from "./scope-overlay"

// The Scope content script. The service worker registers it on each origin
// the person allowed from the toolbar button (scope-origins.ts), and puts it
// into a tab that was open before. Pin mode is toggled by the button's
// message; the drafts live in chrome.storage.local.

// ready is the overlay, or null on an origin Scope is not allowed on; retry
// boots again after such a refusal, as the origin may have been allowed
// since (Chrome's match patterns ignore the port, so the script can be in a
// page of another port of an allowed host).
type Booted = { ready: Promise<ScopeOverlay | null>; retry: () => Promise<ScopeOverlay | null> }
const BOOTED = "__legatoScope"

type Answer = { ok: true; [key: string]: unknown } | { ok: false; error: string }

// A call to the service worker, which owns the token; its refusal is thrown.
async function ask(message: Record<string, unknown>): Promise<Answer & { ok: true }> {
  let answer: Answer | undefined
  try {
    answer = await chrome.runtime.sendMessage(message) as Answer | undefined
  } catch (error) {
    throw new Error(`The extension did not answer (${error instanceof Error ? error.message : String(error)}). Reload the page.`)
  }
  if (!answer) throw new Error("The extension did not answer. Reload the page.")
  if (!answer.ok) throw new Error(answer.error)
  return answer
}

function newId(): string {
  return `d-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`
}

const nextFrame = (win: Window): Promise<void> => new Promise((resolve) => {
  if (typeof win.requestAnimationFrame === "function") win.requestAnimationFrame(() => resolve())
  else win.setTimeout(resolve, 16)
})

// A pin's context: the page, the element and its styles, then a screenshot
// the service worker takes of the visible tab (the overlay is hidden by
// then; two frames let the page paint without it). The default styles are
// read in an iframe put in the overlay's shadow root. The component that
// drew the element is looked up meanwhile, within its bound; a source of
// none is left out of the pin.
async function capture(win: Window, element: Element, container: Node): Promise<CapturedPin> {
  const reader = createStyleReader(win.document, container)
  let pin: CapturedPin["pin"]
  try {
    pin = { page: capturePage(win), target: captureTarget(element), styles: captureStyles(element, reader) }
  } finally {
    reader.dispose()
  }
  const source = lookupSource(element, (message) => chrome.runtime.sendMessage(message))
  await nextFrame(win)
  await nextFrame(win)
  const rect = element.getBoundingClientRect()
  const answer = await chrome.runtime.sendMessage({
    type: "scopeCapture",
    box: { x: rect.left, y: rect.top, width: rect.width, height: rect.height },
    viewport: { width: win.innerWidth, height: win.innerHeight },
  }).catch((error: unknown) => ({ ok: false, error: error instanceof Error ? error.message : String(error) })) as { ok?: boolean; screenshot?: unknown; error?: unknown } | undefined
  const found = await source
  if (found.confidence !== "none") pin = { ...pin, source: found }
  if (!answer?.ok) return { pin, screenshotError: typeof answer?.error === "string" ? answer.error : "The extension did not answer." }
  return typeof answer.screenshot === "string" ? { pin, screenshot: answer.screenshot } : { pin }
}

export function bootScope(doc: Document = document, win: Window = window): Promise<ScopeOverlay | null> {
  const holder = win as unknown as Record<string, Booted | undefined>
  const existing = holder[BOOTED]
  if (existing) return existing.retry()
  const origin = win.location.origin

  // Listening starts at once, so a toggle sent as the script arrives is
  // answered when the overlay is ready.
  let ready: Promise<ScopeOverlay | null> = Promise.resolve(null)
  const retry = (): Promise<ScopeOverlay | null> => {
    ready = ready.then((overlay) => overlay ?? start())
    holder[BOOTED] = { ready, retry }
    return ready
  }
  chrome.runtime.onMessage.addListener((message: unknown, _sender, sendResponse) => {
    const request = message as { type?: string; force?: unknown }
    if (request?.type !== "scopeTogglePin") return false
    void retry().then((overlay) => {
      if (!overlay) sendResponse({ ok: false, error: `Scope is not allowed on ${origin}` })
      else sendResponse({ ok: true, pinMode: overlay.setPinMode(request.force === true ? true : undefined) })
    })
    return true
  })

  // Listeners are added only once the overlay exists, so a refused boot
  // tried again adds none.
  const start = async (): Promise<ScopeOverlay | null> => {
    const stored = await chrome.storage.local.get(SCOPE_ORIGINS_KEY).catch(() => ({} as Record<string, unknown>))
    // Chrome's match patterns ignore the port, so the script may land on
    // another port of an allowed host.
    if (!normalizeScopeOrigins(stored[SCOPE_ORIGINS_KEY]).includes(origin)) return null
    const store = createTabDraftStore(chrome.storage.local, origin, ask)
    const overlay: ScopeOverlay = createScopeOverlay(doc, win, {
      store,
      capture: (element) => capture(win, element, overlay.root),
      locate: (target) => locateTarget(doc, target),
      newId,
      now: () => new Date().toISOString(),
      sent: {
        send: async (pins: ScopePin[]) => (await ask({ type: "scopeSend", pins })).result as ScopeBatchResult,
        reopen: async (id, comment) => { await ask({ type: "scopeReopen", id, comment }) },
        withdraw: async (id) => { await ask({ type: "scopeWithdraw", id }) },
        cleared: createTabClearedStore(chrome.storage.local, origin, ask),
      },
    })
    chrome.storage.onChanged.addListener((changes, area) => {
      if (area === "local" && (draftsKey(origin) in changes || clearedKey(origin) in changes)) void overlay.refresh()
    })
    // The service worker tells the site's tabs when its pins change.
    chrome.runtime.onMessage.addListener((message: unknown) => {
      const update = message as { type?: string; origin?: string; pins?: unknown }
      if (update?.type === "scopePinsChanged" && update.origin === origin && Array.isArray(update.pins)) overlay.setSent(update.pins as SentPin[])
      return false
    })
    // Watching again when the tab comes back: the service worker may have
    // been stopped meanwhile and forgotten the tab.
    const watch = (): void => {
      void ask({ type: "scopeWatch" })
        .then((answer) => overlay.setSent((answer.pins as SentPin[] | undefined) ?? []))
        .catch((error: unknown) => overlay.setSentError(error instanceof Error ? error.message : String(error)))
    }
    doc.addEventListener("visibilitychange", () => { if (doc.visibilityState === "visible") watch() })
    watch()
    watchPage(doc, win, overlay)
    await overlay.refresh()
    return overlay
  }
  ready = start()
  holder[BOOTED] = { ready, retry }
  return ready
}

// A site that navigates without loading a page changes the address and the
// page under the markers: the drafts are read again on a new address, and
// lost markers looked for again when the page changes.
function watchPage(doc: Document, win: Window, overlay: ScopeOverlay): void {
  let href = win.location.href
  let timer: number | undefined
  const check = (): void => {
    timer = undefined
    if (win.location.href !== href) {
      href = win.location.href
      void overlay.refresh()
    } else {
      overlay.relocate()
    }
  }
  const schedule = (): void => {
    if (timer === undefined) timer = win.setTimeout(check, 250)
  }
  new MutationObserver((records) => {
    // The overlay's own changes are in its shadow root, which the observer
    // does not see; a change to its host element is ignored.
    if (records.every((record) => record.target === doc.documentElement && [...record.addedNodes, ...record.removedNodes].every((node) => node.nodeName === "LEGATO-SCOPE"))) return
    schedule()
  }).observe(doc.documentElement, { childList: true, subtree: true })
  win.addEventListener("popstate", schedule)
  win.addEventListener("hashchange", schedule)
}

if (typeof chrome !== "undefined" && chrome.runtime?.id) void bootScope()
