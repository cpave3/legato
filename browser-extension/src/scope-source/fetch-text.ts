import type { FetchText } from "./types"

export const FETCH_TIMEOUT_MS = 1500
// A dev server's module with its inline source map is rarely over a
// megabyte; this bounds what one lookup reads.
export const MAX_FETCH_BYTES = 8 * 1024 * 1024

// Fetches from one origin only, without cookies or redirects, within a time
// and a size.
export function fetchTextOn(origin: string, options: { timeoutMs?: number; maxBytes?: number; fetch?: typeof fetch } = {}): FetchText {
  const timeoutMs = options.timeoutMs ?? FETCH_TIMEOUT_MS
  const maxBytes = options.maxBytes ?? MAX_FETCH_BYTES
  const fetcher = options.fetch ?? fetch
  return async (url) => {
    if (new URL(url).origin !== origin) throw new Error(`${url} is not on ${origin}`)
    const response = await fetcher(url, { credentials: "omit", redirect: "error", cache: "no-store", signal: AbortSignal.timeout(timeoutMs) })
    if (!response.ok) throw new Error(`${url}: ${response.status}`)
    if (Number(response.headers.get("content-length") ?? 0) > maxBytes) throw new Error(`${url} is over ${maxBytes} bytes`)
    if (!response.body) return ""
    const reader = response.body.getReader()
    const chunks: Uint8Array[] = []
    let size = 0
    for (;;) {
      const { done, value } = await reader.read()
      if (done) break
      size += value.byteLength
      if (size > maxBytes) {
        await reader.cancel().catch(() => undefined)
        throw new Error(`${url} is over ${maxBytes} bytes`)
      }
      chunks.push(value)
    }
    const bytes = new Uint8Array(size)
    let offset = 0
    for (const chunk of chunks) {
      bytes.set(chunk, offset)
      offset += chunk.byteLength
    }
    return new TextDecoder().decode(bytes)
  }
}
