import { createContext, useContext } from "react"

// Whether every setting row shows its config key (Settings › Show config
// keys); otherwise a row shows it on hover and focus.
export const ShowKeysContext = createContext(false)

export function useShowKeys(): boolean {
  return useContext(ShowKeysContext)
}

const storageKey = "legato:settings-show-keys"

export function storedShowKeys(): boolean {
  try { return localStorage.getItem(storageKey) === "true" } catch { return false }
}

export function storeShowKeys(on: boolean) {
  try { localStorage.setItem(storageKey, on ? "true" : "false") } catch { /* Kept for this page only. */ }
}
