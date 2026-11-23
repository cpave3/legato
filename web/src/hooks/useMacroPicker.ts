import { useCallback, useEffect, useMemo, useState } from "react"
import { splitFavorites } from "../lib/macros"

// A macro as a prompt input's picker needs it.
export interface PickerMacro {
  name: string
  keys: string
  favorite?: boolean
}

// useMacroPicker is what a prompt input's macro menu lists: the favourites,
// then the rest once Show all is pressed, until the menu closes. The keys
// 1–9 pick from shown.
export function useMacroPicker<T extends PickerMacro>(macros: T[], open: boolean) {
  const [showAll, setShowAll] = useState(false)
  useEffect(() => {
    if (!open) setShowAll(false)
  }, [open])
  const { shown, hidden } = useMemo(() => {
    const { favorites, rest } = splitFavorites(macros)
    return showAll ? { shown: [...favorites, ...rest], hidden: 0 } : { shown: favorites, hidden: rest.length }
  }, [macros, showAll])
  return { shown, hidden, showAll: useCallback(() => setShowAll(true), []) }
}
