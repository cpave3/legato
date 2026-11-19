import { scopeOriginOf } from "./scope-origins"
import { hostPatternFor } from "./ticket-providers"

export type ScopeActionDeps = {
  // chrome.permissions.request for one origin's pattern. Chrome grants only
  // while the click's gesture lasts, so it is called before anything is
  // awaited; for an origin Chrome already grants it answers at once.
  request: (pattern: string) => Promise<boolean>
  // Whether the origin is one of Scope's (see scope-origins.ts).
  isListed: (origin: string) => Promise<boolean>
  add: (origin: string) => Promise<unknown>
  // Toggles pin mode in the tab (on, with force), putting the Scope script
  // there first when the tab was open before the script was registered.
  toggle: (tabId: number, force?: boolean) => Promise<unknown>
}

export type ScopeActionResult =
  | { kind: "unsupported" }
  | { kind: "denied"; origin: string }
  | { kind: "allowed"; origin: string }
  | { kind: "toggled"; origin: string }
  | { kind: "error"; origin: string; message: string }

// The toolbar button and its shortcut. On a site Scope is not on yet, the
// first click allows that one origin and turns pin mode on; after that it
// toggles pin mode.
export function handleScopeAction(tab: { id?: number; url?: string }, deps: ScopeActionDeps): Promise<ScopeActionResult> {
  const origin = scopeOriginOf(tab.url)
  const tabId = tab.id
  if (!origin || tabId === undefined) return Promise.resolve({ kind: "unsupported" })
  const granted = deps.request(hostPatternFor(origin))
  return (async (): Promise<ScopeActionResult> => {
    try {
      if (!(await granted)) return { kind: "denied", origin }
      if (await deps.isListed(origin)) {
        await deps.toggle(tabId, undefined)
        return { kind: "toggled", origin }
      }
      await deps.add(origin)
      await deps.toggle(tabId, true)
      return { kind: "allowed", origin }
    } catch (error) {
      return { kind: "error", origin, message: error instanceof Error ? error.message : String(error) }
    }
  })()
}
