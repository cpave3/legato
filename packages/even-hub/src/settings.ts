import type { EvenAppBridge } from "@evenrealities/even_hub_sdk"

// The phone-side setting: the pairing code of the Legato Android app's
// glasses link, kept in the Even app's storage for this app on the phone.
// The glasses hold no Legato address or token: the Legato app keeps those.
export type Storage = Pick<EvenAppBridge, "getLocalStorage" | "setLocalStorage">

const CODE_KEY = "legato.phone.code"

// The saved pairing code, or "" when there is none.
export async function loadCode(storage: Storage): Promise<string> {
  return (await storage.getLocalStorage(CODE_KEY)).trim()
}

// Saves a pairing code as the operator pasted it, without the spaces and
// dashes it may have been copied with, and returns what was saved. An empty
// code forgets the one saved. The Legato app makes 24 base32 characters.
export async function saveCode(storage: Storage, typed: string): Promise<string> {
  const code = typed.replace(/[\s-]+/g, "").toUpperCase()
  if (code) {
    if (code.length < 16 || code.length > 64) throw new Error("A pairing code has 16 to 64 characters")
    if (!/^[A-Z2-7]+$/.test(code)) throw new Error("A pairing code has only letters and the digits 2 to 7")
  }
  await storage.setLocalStorage(CODE_KEY, code)
  return code
}
