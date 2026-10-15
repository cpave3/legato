import type { Compatibility } from "./model"

export async function probeHealth(baseUrl: string, timeoutMs = 3000): Promise<{ online: boolean; version?: string }> {
  const controller = new AbortController()
  const timeout = setTimeout(() => controller.abort(), timeoutMs)
  try {
    const response = await fetch(`${baseUrl}/health`, {
      method: "GET",
      signal: controller.signal,
      cache: "no-store",
    })
    if (!response.ok) return { online: false }
    const data = await response.json() as { status?: string; version?: string }
    return {
      online: data.status === undefined || data.status === "ok",
      version: typeof data.version === "string" && data.version ? data.version : undefined,
    }
  } catch {
    return { online: false }
  } finally {
    clearTimeout(timeout)
  }
}

export async function probeServer(baseUrl: string): Promise<{ compatibility: Compatibility; version?: string }> {
  const controller = new AbortController()
  const timeout = setTimeout(() => controller.abort(), 5000)
  try {
    const response = await fetch(`${baseUrl}/api/meta`, { signal: controller.signal })
    if (response.status === 404) return { compatibility: "legacy" }
    if (!response.ok) throw new Error(`Server returned ${response.status}`)
    const data = await response.json() as { version?: string; mobile_shell_api?: number }
    return { compatibility: data.mobile_shell_api === 1 ? "compatible" : "unsupported", version: data.version }
  } finally { clearTimeout(timeout) }
}

// bootstrapScript runs in the page before it loads, in its main frame only.
// Besides signing the page in, it tells the page the app can record for it
// (`window.legatoShell`), with a key the page must send back: the frames an
// agent's HTML runs in can post to the app too, and must not start the
// microphone.
export function bootstrapScript(baseUrl: string, token: string, shellKey?: string): string {
  const value = JSON.stringify(token)
  const shell = shellKey ? `window.legatoShell={voice:1,key:${JSON.stringify(shellKey)}};` : ""
  return `localStorage.setItem("legato:token:local",${value});localStorage.setItem(${JSON.stringify(`legato:token:${baseUrl}`)},${value});${shell}true;`
}

export function allowNavigation(baseUrl: string, target: string): boolean {
  try { return new URL(target).origin === new URL(baseUrl).origin } catch { return false }
}
