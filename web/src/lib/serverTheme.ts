import { apiFetch } from "./api"

export interface ThemePreference { id: string; revision: number }
export const SERVER_THEME_CACHE_KEY = "legato:server-theme-cache"
const uncached = new Map<string, ThemePreference | undefined>()
const connectionEpochs = new Map<string, number>()
let mountedConnection: string | undefined

// Storage is the next startup's choice, not another tab's active connection.
export function activateThemeConnection(baseUrl: string | undefined): void {
  mountedConnection = baseUrl
}

function readCache(): Record<string, ThemePreference | null> {
  try { return JSON.parse(localStorage.getItem(SERVER_THEME_CACHE_KEY) ?? "{}") ?? {} }
  catch { return {} }
}

export function themeConnectionIdentity(baseUrl: string): string {
  return new URL(baseUrl || window.location.origin, window.location.origin).href.replace(/\/$/, "")
}

export function activeThemeConnection(): string {
  if (mountedConnection !== undefined) return mountedConnection
  try { return localStorage.getItem("legato:active-server") ?? "" } catch { return "" }
}

export function cachedServerTheme(baseUrl: string): ThemePreference | undefined {
  const identity = themeConnectionIdentity(baseUrl)
  return uncached.has(identity) ? uncached.get(identity) : readCache()[identity] ?? undefined
}

export function themeConnectionEpoch(baseUrl: string): number {
  return connectionEpochs.get(themeConnectionIdentity(baseUrl)) ?? 0
}

export function cacheServerTheme(baseUrl: string, preference?: ThemePreference): void {
  const identity = themeConnectionIdentity(baseUrl)
  try {
    const cache = readCache()
    cache[identity] = preference ?? null
    localStorage.setItem(SERVER_THEME_CACHE_KEY, JSON.stringify(cache))
    uncached.delete(identity)
  } catch { uncached.set(identity, preference) }
}

export function forgetServerTheme(baseUrl: string): void {
  const identity = themeConnectionIdentity(baseUrl)
  connectionEpochs.set(identity, themeConnectionEpoch(baseUrl) + 1)
  // A removed connection must not inherit its old cache if added offline.
  uncached.set(identity, undefined)
  try {
    const cache = readCache()
    delete cache[identity]
    localStorage.setItem(SERVER_THEME_CACHE_KEY, JSON.stringify(cache))
  } catch { /* The in-memory invalidation still takes effect. */ }
}

export async function fetchThemePreference(baseUrl: string): Promise<ThemePreference> {
  const response = await apiFetch(baseUrl, "/api/settings/theme")
  if (!response.ok) throw new Error("Could not load the server theme preference. Try again.")
  return response.json()
}

export async function saveThemePreference(baseUrl: string, id: string, revision: number): Promise<ThemePreference> {
  const response = await apiFetch(baseUrl, "/api/settings/theme", {
    method: "PUT", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ id, expected_revision: revision }),
  })
  if (!response.ok) {
    if (response.status === 409) throw new Error("The server theme changed elsewhere. Reload the preference and try again.")
    const body = await response.json().catch(() => null)
    throw new Error(body?.error || "Could not save the server theme preference. Try again.")
  }
  return response.json()
}
