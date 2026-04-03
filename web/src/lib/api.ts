import { getToken } from "./auth"

/**
 * Fetch wrapper that prepends the active server's base URL and includes
 * the auth token. Use instead of raw fetch() for all API calls.
 */
export function apiFetch(baseUrl: string, path: string, init?: RequestInit): Promise<Response> {
  const url = baseUrl ? `${baseUrl}${path}` : path
  const token = getToken(baseUrl || undefined)
  const headers: Record<string, string> = {
    ...(init?.headers as Record<string, string> ?? {}),
  }
  if (token) {
    headers["Authorization"] = `Bearer ${token}`
  }
  return fetch(url, { ...init, headers })
}
