import type { EvenAppBridge } from "@evenrealities/even_hub_sdk"
import type { Source } from "./legato"

// The phone-side settings: which Legato the app reads and its web token,
// kept in the Even app's storage for this app on the phone. Never in the
// bundle: anyone can unpack an .ehpk.
export type Storage = Pick<EvenAppBridge, "getLocalStorage" | "setLocalStorage">

const URL_KEY = "legato.url"
const TOKEN_KEY = "legato.token"

// Read through the dev server (dev/legato-dev.ts) when nothing is saved.
export const DEV_SOURCE: Source = { url: "/legato" }

// The saved address and token; with none saved, `fallback`: the dev server,
// or the Legato a packed build was made for (scripts/pack.mjs).
export async function loadSource(storage: Storage, fallback: Source = DEV_SOURCE): Promise<Source> {
  const url = (await storage.getLocalStorage(URL_KEY)).trim()
  if (!url) return fallback
  const token = (await storage.getLocalStorage(TOKEN_KEY)).trim()
  return token ? { url, token } : { url }
}

const loopback = new Set(["localhost", "127.0.0.1", "[::1]"])

// The address's origin, refusing what is not an address and plain HTTP to
// another machine, which would carry the token in the clear.
function origin(url: string): string {
  let parsed: URL
  try {
    parsed = new URL(url)
  } catch {
    throw new Error(`"${url}" is not an address`)
  }
  if (parsed.protocol === "https:" || (parsed.protocol === "http:" && loopback.has(parsed.hostname))) return parsed.origin
  throw new Error("Legato's address must use HTTPS")
}

// Saves the address and token. With no token given, the one saved stays, but
// only for the same server: a token never follows an address elsewhere. An
// empty address clears both.
export async function saveSource(storage: Storage, source: { url: string; token?: string }): Promise<void> {
  const url = source.url.trim()
  if (!url) {
    await storage.setLocalStorage(URL_KEY, "")
    await storage.setLocalStorage(TOKEN_KEY, "")
    return
  }
  const to = origin(url)
  let token = source.token?.trim() ?? ""
  if (!token) {
    const saved = (await storage.getLocalStorage(URL_KEY)).trim()
    const sameServer = saved !== "" && (() => {
      try {
        return new URL(saved).origin === to
      } catch {
        return false
      }
    })()
    if (sameServer) token = (await storage.getLocalStorage(TOKEN_KEY)).trim()
  }
  await storage.setLocalStorage(URL_KEY, url)
  await storage.setLocalStorage(TOKEN_KEY, token)
}
