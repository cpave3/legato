import { scopeOriginOf, type ScopeOrigin } from "./scope-origins"

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
}

export type ScopeResponse = { ok: true; [key: string]: unknown } | { ok: false; error: string }

const MESSAGES = new Set(["listScopeOrigins", "removeScopeOrigin"])

export function isScopeMessage(message: unknown): boolean {
  return Boolean(message) && typeof message === "object" && MESSAGES.has((message as { type?: string }).type ?? "")
}

function errorText(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}

function exactOrigin(value: unknown): string {
  if (typeof value !== "string" || scopeOriginOf(value) !== value) throw new Error("Invalid origin")
  return value
}

export function createScopeMessageHandler(deps: ScopeServiceDeps) {
  const fromExtension = (sender: ScopeSender): void => {
    if (!sender.url?.startsWith(deps.extensionUrl)) throw new Error("Only the extension's own pages may do that")
  }
  return async (message: unknown, sender: ScopeSender): Promise<ScopeResponse> => {
    try {
      const request = message as { type?: string; origin?: unknown }
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
        default:
          throw new Error("Unsupported extension message")
      }
    } catch (error) {
      return { ok: false, error: errorText(error) }
    }
  }
}
