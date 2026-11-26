import { useCallback, useState } from "react"
import { useMediaQuery } from "../../hooks/useMediaQuery"

// useCardFolds is which cards over a workflow's graph are folded (LEG-651,
// LEG-655): as the operator last left them in this browser, under the
// page's own key, else the page's defaults, else open.
export function useCardFolds(key: string, defaults: Record<string, boolean>) {
  const [saved, setSaved] = useState<Record<string, boolean>>(() => {
    try { const value = JSON.parse(localStorage.getItem(key) ?? "{}"); return value && typeof value === "object" ? value : {} } catch { return {} }
  })
  // The defaults by value, so a page's fresh object each render keeps
  // toggle and open the same functions.
  const shape = JSON.stringify(defaults)
  const folded = (card: string) => saved[card] ?? defaults[card] ?? false
  const set = useCallback((card: string, next: (was: boolean) => boolean) => setSaved(current => {
    const was = current[card] ?? (JSON.parse(shape) as Record<string, boolean>)[card] ?? false
    if (next(was) === was) return current
    const changed = { ...current, [card]: next(was) }
    try { localStorage.setItem(key, JSON.stringify(changed)) } catch { /* kept for this visit only */ }
    return changed
  }), [key, shape])
  const toggle = useCallback((card: string) => set(card, was => !was), [set])
  // open unfolds a card, such as the one that holds the row chosen.
  const open = useCallback((card: string) => set(card, () => false), [set])
  return { folded, toggle, open }
}

// useLargeScreen is a screen with room for the cards open (LEG-651): there
// Inputs opens by default.
export function useLargeScreen(): boolean {
  return useMediaQuery("(min-width: 1280px) and (min-height: 800px)")
}
