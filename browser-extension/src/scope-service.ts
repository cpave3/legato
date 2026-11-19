import { scopeOriginOf, type ScopeOrigin } from "./scope-origins"
import type { Box } from "./scope-types"

// The service worker's side of Scope: the options page's list of origins,
// and (from the content script) capture and the calls to Legato. Only the
// service worker reads the token.

export type ScopeSender = { tab?: { id?: number; url?: string; windowId?: number }; url?: string }

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
}

export type ScopeResponse = { ok: true; [key: string]: unknown } | { ok: false; error: string }

const MESSAGES = new Set(["listScopeOrigins", "removeScopeOrigin", "scopeCapture"])

export function isScopeMessage(message: unknown): boolean {
  return Boolean(message) && typeof message === "object" && MESSAGES.has((message as { type?: string }).type ?? "")
}

function errorText(error: unknown): string {
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
      const request = message as { type?: string; origin?: unknown; box?: unknown; viewport?: unknown }
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
        default:
          throw new Error("Unsupported extension message")
      }
    } catch (error) {
      return { ok: false, error: errorText(error) }
    }
  }
}
