import { LegatoRequestError } from "./legato"
import { createClearedStore, createDraftStore, createLock, isDraft, type StorageAreaLike } from "./scope-drafts"
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
  // chrome.storage.local, where the drafts live. The tabs change them
  // through the service worker, one change at a time (scope-drafts.ts).
  storage: StorageAreaLike
}

export type ScopeResponse = { ok: true; [key: string]: unknown } | { ok: false; error: string }

const MESSAGES = new Set([
  "listScopeOrigins", "removeScopeOrigin", "scopeCapture", "scopeSend", "scopeWatch", "scopeReopen", "scopeWithdraw",
  "scopeDraftAdd", "scopeDraftUpdate", "scopeDraftRemove", "scopeClearedAdd",
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

function validScreenshot(value: unknown): string | undefined {
  if (value === undefined) return undefined
  if (typeof value !== "string" || !value.startsWith("data:image/")) throw new Error("A pin's screenshot is not an image")
  const base64 = value.slice(value.indexOf(",") + 1)
  if (Math.floor(base64.length * 3 / 4) > MAX_SCREENSHOT_BYTES) throw new Error("A pin's screenshot is over 500 KB")
  return value
}

function validComment(value: unknown): string {
  if (typeof value !== "string" || !value.trim()) throw new Error("Every pin needs a comment")
  return value
}

function pinIds(value: unknown): string[] {
  if (!Array.isArray(value) || value.length > 500) throw new Error("Invalid pin")
  return value.map(pinId)
}

// The checks Legato makes, made first, so the person hears of a problem
// before a large upload.
function validPins(value: unknown, origin: string): ScopePin[] {
  if (!Array.isArray(value) || value.length === 0) throw new Error("There are no pins to send")
  if (value.length > MAX_PINS_PER_BATCH) throw new Error(`A batch takes at most ${MAX_PINS_PER_BATCH} pins`)
  for (const pin of value as Array<Partial<ScopePin>>) {
    if (!pin || typeof pin.comment !== "string" || !pin.comment.trim()) throw new Error("Every pin needs a comment")
    if (scopeOriginOf(pin.page?.url) !== origin) throw new Error("A pin is on another site")
    validScreenshot(pin.screenshot)
  }
  return value as ScopePin[]
}

export function createScopeMessageHandler(deps: ScopeServiceDeps) {
  const drafts = createLock()
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
      const request = message as {
        type?: string; origin?: unknown; box?: unknown; viewport?: unknown; pins?: unknown; id?: unknown; comment?: unknown
        draft?: unknown; screenshot?: unknown; ids?: unknown
      }
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
          // Chrome takes one only with activeTab, which the toolbar button
          // and its shortcut give for the page they were used on.
          let captured: string
          try {
            captured = await deps.captureTab(windowId)
          } catch (error) {
            throw new Error(`Chrome did not let Scope take a screenshot (${error instanceof Error ? error.message : String(error)}). Click the extension's toolbar button or press its shortcut on this page, then pin again.`)
          }
          const screenshot = await deps.crop(captured, box, viewport)
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
        case "scopeDraftAdd": {
          const { origin } = await scopeTab(sender)
          if (!isDraft(request.draft)) throw new Error("Invalid draft")
          if (scopeOriginOf(request.draft.pin.page.url) !== origin) throw new Error("A pin is on another site")
          validComment(request.draft.pin.comment)
          await createDraftStore(deps.storage, origin, drafts).add(request.draft, validScreenshot(request.screenshot))
          return { ok: true }
        }
        case "scopeDraftUpdate": {
          const { origin } = await scopeTab(sender)
          await createDraftStore(deps.storage, origin, drafts).update(pinId(request.id), validComment(request.comment))
          return { ok: true }
        }
        case "scopeDraftRemove": {
          const { origin } = await scopeTab(sender)
          await createDraftStore(deps.storage, origin, drafts).removeMany(pinIds(request.ids))
          return { ok: true }
        }
        case "scopeClearedAdd": {
          const { origin } = await scopeTab(sender)
          await createClearedStore(deps.storage, origin, drafts).add(pinIds(request.ids))
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
