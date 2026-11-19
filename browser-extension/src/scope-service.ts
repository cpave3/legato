import { LegatoRequestError } from "./legato"
import { scopeOriginOf, type ScopeOrigin } from "./scope-origins"
import { MAX_PINS_PER_BATCH, MAX_SCREENSHOT_BYTES, type Box, type ScopeBatch, type ScopeBatchResult, type ScopePin, type SentPin } from "./scope-types"

// The service worker's side of Scope: the options page's list of origins,
// and, for the content script, the screenshot and the calls to Legato. Only
// the service worker reads the token.

export type ScopeSender = { tab?: { id?: number; url?: string; windowId?: number }; url?: string }

// The Legato calls Scope makes (LegatoClient's).
export type ScopeApi = {
  sendScopeBatch: (batch: ScopeBatch) => Promise<ScopeBatchResult>
  listScopePins: (origin: string) => Promise<SentPin[]>
  reopenScopePin: (id: string, comment: string) => Promise<SentPin>
  withdrawScopePin: (id: string) => Promise<void>
}

export type ScopeServiceDeps = {
  // chrome.runtime.getURL(""): the extension's own pages start with it.
  extensionUrl: string
  origins: {
    list: () => Promise<ScopeOrigin[]>
    remove: (origin: string) => Promise<ScopeOrigin[]>
    isAllowed: (origin: string) => Promise<boolean>
  }
  // Gives Chrome's access to an origin back, unless something else of the
  // extension still uses it.
  releasePermission: (origin: string) => Promise<void>
  // chrome.tabs.captureVisibleTab as a PNG data URL.
  captureTab: (windowId: number) => Promise<string>
  // Crops, outlines and encodes it (scope-screenshot.ts); null when the
  // element is not in view.
  crop: (captured: string, box: Box, viewport: { width: number; height: number }) => Promise<string | null>
  // The configured Legato, once the extension may reach it.
  legato: () => Promise<{ baseUrl: string; api: ScopeApi }>
  // Following origins' pins over Legato's WebSocket (scope-live.ts).
  live: {
    watch: (tabId: number, origin: string) => void
    refresh: (origin: string) => Promise<void>
  }
}

export type ScopeResponse = { ok: true; [key: string]: unknown } | { ok: false; error: string }

const MESSAGES = new Set([
  "listScopeOrigins", "removeScopeOrigin", "scopeCapture", "scopeSend", "scopeWatch", "scopeReopen", "scopeWithdraw",
])

export function isScopeMessage(message: unknown): boolean {
  return Boolean(message) && typeof message === "object" && MESSAGES.has((message as { type?: string }).type ?? "")
}

// What the person reads when Legato refuses or cannot be reached. An
// unreachable Legato's message already carries its advice.
export function scopeErrorText(error: unknown): string {
  if (error instanceof LegatoRequestError) {
    if (error.status === 401) return `Legato refused the extension's token (${error.message}). Open the extension options, check the bearer token, and save.`
    if (error.status === 413) return `Legato refused the batch as too large (${error.message}). A batch takes at most ${MAX_PINS_PER_BATCH} pins, and a screenshot at most 500 KB.`
  }
  return error instanceof Error ? error.message : String(error)
}

const finite = (value: unknown): value is number => typeof value === "number" && Number.isFinite(value)

function validBox(value: unknown): Box {
  const box = value as Partial<Box> | null
  if (!box || !finite(box.x) || !finite(box.y) || !finite(box.width) || !finite(box.height)) throw new Error("Invalid box")
  return { x: box.x, y: box.y, width: box.width, height: box.height }
}

function validViewport(value: unknown): { width: number; height: number } {
  const size = value as { width?: unknown; height?: unknown } | null
  if (!size || !finite(size.width) || !finite(size.height) || size.width <= 0 || size.height <= 0) throw new Error("Invalid viewport")
  return { width: size.width, height: size.height }
}

function exactOrigin(value: unknown): string {
  if (typeof value !== "string" || scopeOriginOf(value) !== value) throw new Error("Invalid origin")
  return value
}

function pinId(value: unknown): string {
  if (typeof value !== "string" || !value || value.length > 200) throw new Error("Invalid pin")
  return value
}

// The checks Legato makes, made first, so the person hears of a problem
// before a large upload.
function validPins(value: unknown, origin: string): ScopePin[] {
  if (!Array.isArray(value) || value.length === 0) throw new Error("There are no pins to send")
  if (value.length > MAX_PINS_PER_BATCH) throw new Error(`A batch takes at most ${MAX_PINS_PER_BATCH} pins`)
  for (const pin of value as Array<Partial<ScopePin>>) {
    if (!pin || typeof pin.comment !== "string" || !pin.comment.trim()) throw new Error("Every pin needs a comment")
    if (scopeOriginOf(pin.page?.url) !== origin) throw new Error("A pin is on another site")
    if (pin.screenshot !== undefined) {
      if (typeof pin.screenshot !== "string" || !pin.screenshot.startsWith("data:image/")) throw new Error("A pin's screenshot is not an image")
      const base64 = pin.screenshot.slice(pin.screenshot.indexOf(",") + 1)
      if (Math.floor(base64.length * 3 / 4) > MAX_SCREENSHOT_BYTES) throw new Error("A pin's screenshot is over 500 KB")
    }
  }
  return value as ScopePin[]
}

export function createScopeMessageHandler(deps: ScopeServiceDeps) {
  const fromExtension = (sender: ScopeSender): void => {
    if (!sender.url?.startsWith(deps.extensionUrl)) throw new Error("Only the extension's own pages may do that")
  }
  // A content script's tab, on an origin Scope is allowed on.
  const scopeTab = async (sender: ScopeSender): Promise<{ origin: string; tabId: number; windowId?: number }> => {
    const origin = scopeOriginOf(sender.tab?.url ?? sender.url)
    if (!origin || sender.tab?.id === undefined) throw new Error("Scope messages must come from a page")
    if (!(await deps.origins.isAllowed(origin))) throw new Error(`Scope is not allowed on ${origin}`)
    return { origin, tabId: sender.tab.id, windowId: sender.tab.windowId }
  }
  return async (message: unknown, sender: ScopeSender): Promise<ScopeResponse> => {
    try {
      const request = message as { type?: string; origin?: unknown; box?: unknown; viewport?: unknown; pins?: unknown; id?: unknown; comment?: unknown }
      switch (request.type) {
        case "listScopeOrigins":
          fromExtension(sender)
          return { ok: true, origins: await deps.origins.list() }
        case "removeScopeOrigin": {
          fromExtension(sender)
          const origin = exactOrigin(request.origin)
          const origins = await deps.origins.remove(origin)
          await deps.releasePermission(origin)
          return { ok: true, origins }
        }
        case "scopeCapture": {
          const { windowId } = await scopeTab(sender)
          const box = validBox(request.box)
          const viewport = validViewport(request.viewport)
          if (windowId === undefined) throw new Error("The tab has no window")
          const screenshot = await deps.crop(await deps.captureTab(windowId), box, viewport)
          return screenshot ? { ok: true, screenshot } : { ok: true }
        }
        case "scopeSend": {
          const { origin } = await scopeTab(sender)
          const pins = validPins(request.pins, origin)
          const { api } = await deps.legato()
          const result = await api.sendScopeBatch({ origin, pins })
          void deps.live.refresh(origin).catch(() => undefined)
          return { ok: true, result }
        }
        case "scopeWatch": {
          const { origin, tabId } = await scopeTab(sender)
          deps.live.watch(tabId, origin)
          const { api } = await deps.legato()
          return { ok: true, pins: await api.listScopePins(origin) }
        }
        case "scopeReopen": {
          const { origin } = await scopeTab(sender)
          const id = pinId(request.id)
          if (typeof request.comment !== "string" || !request.comment.trim()) throw new Error("A reopened pin needs a comment")
          const { api } = await deps.legato()
          await api.reopenScopePin(id, request.comment.trim())
          void deps.live.refresh(origin).catch(() => undefined)
          return { ok: true }
        }
        case "scopeWithdraw": {
          const { origin } = await scopeTab(sender)
          const { api } = await deps.legato()
          await api.withdrawScopePin(pinId(request.id))
          void deps.live.refresh(origin).catch(() => undefined)
          return { ok: true }
        }
        default:
          throw new Error("Unsupported extension message")
      }
    } catch (error) {
      return { ok: false, error: scopeErrorText(error) }
    }
  }
}
