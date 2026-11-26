import type { EvenAppBridge } from "@evenrealities/even_hub_sdk"

// The phone-side settings, kept in the Even app's storage for this app on
// the phone: the pairing code of the Legato Android app's glasses link, and
// how often to read while the glasses' screen is off.
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

const SCREEN_OFF_KEY = "legato.screen-off"

// How often the app reads Legato while the glasses' screen is off. Waking
// the screen sends the app no event, so what it shows on waking is only as
// fresh as its last read: by default it reads every 5 seconds, as while the
// screen is on, so a page left open stays live. 30 or 60 seconds, or not at
// all, save the phone reads; any press reads again at once. A choice saved
// before 5 seconds was one stays as it was.
export type ScreenOff = "5" | "30" | "60" | "off"
export const SCREEN_OFF_CHOICES: ScreenOff[] = ["5", "30", "60", "off"]
const isScreenOff = (v: string): v is ScreenOff => (SCREEN_OFF_CHOICES as string[]).includes(v)

export async function loadScreenOff(storage: Storage): Promise<ScreenOff> {
  const saved = (await storage.getLocalStorage(SCREEN_OFF_KEY)).trim()
  return isScreenOff(saved) ? saved : "5"
}

export async function saveScreenOff(storage: Storage, value: string): Promise<ScreenOff> {
  if (!isScreenOff(value)) throw new Error(`Read every 5, 30 or 60 seconds, or off; not ${JSON.stringify(value)}`)
  await storage.setLocalStorage(SCREEN_OFF_KEY, value)
  return value
}

// The choice in milliseconds, or undefined for off.
export function screenOffMs(value: ScreenOff): number | undefined {
  return value === "off" ? undefined : Number(value) * 1000
}
