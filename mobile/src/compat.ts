import type { Compatibility } from "./model"

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

export function bootstrapScript(baseUrl: string, token: string): string {
  const value = JSON.stringify(token)
  return `localStorage.setItem("legato:token:local",${value});localStorage.setItem(${JSON.stringify(`legato:token:${baseUrl}`)},${value});true;`
}

export function allowNavigation(baseUrl: string, target: string): boolean {
  try { return new URL(target).origin === new URL(baseUrl).origin } catch { return false }
}
