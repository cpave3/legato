import { useCallback, useEffect, useMemo, useState } from "react"
import { pickerMacros, type Macro, type PickerOptions } from "../lib/macros"

// A macro as a picker needs it.
export type PickerMacro = Pick<Macro, "name" | "keys" | "favorite" | "applies_to" | "agent" | "tier">

// useMacroPicker is what a macro picker lists: the macros for the caller's
// targets (every macro without them), the favourites, then the rest once
// Show all is pressed, until the picker closes. The message box's keys 1–9
// pick from shown.
export function useMacroPicker<T extends PickerMacro>(macros: T[], open: boolean, options: PickerOptions = {}) {
  const [showAll, setShowAll] = useState(false)
  useEffect(() => {
    if (!open) setShowAll(false)
  }, [open])
  const targets = options.targets?.join(",")
  const { suggested } = options
  const { shown, hidden } = useMemo(() => {
    const { favorites, rest } = pickerMacros(macros, { targets: targets === undefined ? undefined : targets.split(",").filter(Boolean) as PickerOptions["targets"], suggested })
    return showAll ? { shown: [...favorites, ...rest], hidden: 0 } : { shown: favorites, hidden: rest.length }
  }, [macros, showAll, targets, suggested])
  return { shown, hidden, showAll: useCallback(() => setShowAll(true), []) }
}
