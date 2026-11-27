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

/**
 * The Error a refused response stands for: the body's `error` when it is a
 * string (an empty one included), else the status text, else
 * "request failed (<status>)". Reads the body.
 */
export async function serverError(response: Response): Promise<Error> {
  let message = response.statusText
  try {
    const body = await response.json()
    if (body && typeof body.error === "string") message = body.error
  } catch {
    // The body was not JSON; the status says enough.
  }
  return new Error(message || `request failed (${response.status})`)
}

/**
 * Returns a successful response untouched, its body unread; throws
 * serverError's Error for any other.
 */
export async function ok(response: Response): Promise<Response> {
  if (response.ok) return response
  throw await serverError(response)
}

export async function toggleAgentNotify(baseUrl: string, taskId: string, enabled: boolean): Promise<void> {
  const res = await apiFetch(baseUrl, `/api/agents/${encodeURIComponent(taskId)}/notify`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ enabled }),
  })
  if (!res.ok) {
    const text = await res.text().catch(() => "unknown error")
    throw new Error(text)
  }
}
