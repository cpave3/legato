import { useState } from "react"
import { useMediaQuery } from "../../hooks/useMediaQuery"

// useCardFolds is which cards over a workflow's graph are folded (LEG-651,
// LEG-655): as the operator last left them in this browser, under the
// page's own key, else the page's defaults, else open.
export function useCardFolds(key: string, defaults: Record<string, boolean>) {
  const [saved, setSaved] = useState<Record<string, boolean>>(() => {
    try { const value = JSON.parse(localStorage.getItem(key) ?? "{}"); return value && typeof value === "object" ? value : {} } catch { return {} }
  })
  const folded = (card: string) => saved[card] ?? defaults[card] ?? false
  const toggle = (card: string) => setSaved(current => {
    const next = { ...current, [card]: !(current[card] ?? defaults[card] ?? false) }
    try { localStorage.setItem(key, JSON.stringify(next)) } catch { /* kept for this visit only */ }
    return next
  })
  return { folded, toggle }
}

// useLargeScreen is a screen with room for the cards open (LEG-651): there
// Inputs opens by default.
export function useLargeScreen(): boolean {
  return useMediaQuery("(min-width: 1280px) and (min-height: 800px)")
}
