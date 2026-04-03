const TOKEN_PREFIX = "legato:token:"

export function getToken(serverUrl?: string): string | null {
  const key = serverUrl ? TOKEN_PREFIX + serverUrl : TOKEN_PREFIX + "local"
  return localStorage.getItem(key)
}

export function setToken(token: string, serverUrl?: string): void {
  const key = serverUrl ? TOKEN_PREFIX + serverUrl : TOKEN_PREFIX + "local"
  localStorage.setItem(key, token)
}

export function clearToken(serverUrl?: string): void {
  const key = serverUrl ? TOKEN_PREFIX + serverUrl : TOKEN_PREFIX + "local"
  localStorage.removeItem(key)
}

/** Build fetch headers with auth token if available. */
export function authHeaders(serverUrl?: string): Record<string, string> {
  const token = getToken(serverUrl)
  if (token) {
    return { Authorization: `Bearer ${token}` }
  }
  return {}
}
