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

export async function loadSource(storage: Storage): Promise<Source> {
  const url = (await storage.getLocalStorage(URL_KEY)).trim()
  if (!url) return DEV_SOURCE
  const token = (await storage.getLocalStorage(TOKEN_KEY)).trim()
  return token ? { url, token } : { url }
}

export async function saveSource(storage: Storage, source: { url: string; token: string }): Promise<void> {
  const url = source.url.trim()
  await storage.setLocalStorage(URL_KEY, url)
  await storage.setLocalStorage(TOKEN_KEY, url ? source.token.trim() : "")
}
