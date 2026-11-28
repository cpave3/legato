import { useCallback, useContext, useEffect, useMemo, useState } from "react"
import { itemKey, itemText } from "../../lib/workflowDocument"
import { PendingDrafts } from "./pendingDrafts"
import { placeOf, type Row } from "./outlineRows"

/** HeldYaml is item YAML not written yet, by item key, with the item's
 * text it was begun from. */
export type HeldYaml = Record<string, { base: string; text: string }>

type Options = {
  text: string
  order: Row[]
  held?: HeldYaml
  onHeld?: React.Dispatch<React.SetStateAction<HeldYaml>>
}

// withHeld keeps an item's held text, or drops it (held null); its base
// stays the text it was begun from.
function withHeld(previous: HeldYaml, key: string, base: string, held: string | null): HeldYaml {
  if (held !== null) return { ...previous, [key]: { base: previous[key]?.base === base ? previous[key].base : base, text: held } }
  if (!(key in previous)) return previous
  const next = { ...previous }
  delete next[key]
  return next
}

// heldByPlace is the held text of each row whose item still has the text
// it was begun from, by the row's place.
function heldByPlace(order: Row[], heldStore: HeldYaml, text: string): Record<string, string> {
  const out: Record<string, string> = {}
  for (const row of order) {
    if (row.selection.kind === "about") continue
    const kept = heldStore[itemKey(row.selection)]
    if (kept && itemText(text, row.selection) === kept.base) out[placeOf(order, row.selection)] = kept.text
  }
  return out
}

/** useHeldYaml keeps an item's YAML that does not read yet: by the item's
 * key with the text it was begun from, by the page (so leaving the tab,
 * the row or Visual keeps it) or else here, and counted as unsaved. It is
 * shown only while its item still has that text, so a row that moves or
 * changes never takes another's. heldYaml is the held text by the row's
 * place, which the inspector is keyed by; hold keeps or drops a row's. */
export function useHeldYaml({ text, order, held, onHeld }: Options) {
  const [ownHeld, setOwnHeld] = useState<HeldYaml>({})
  const heldStore = held ?? ownHeld
  const setHeldStore = onHeld ?? setOwnHeld
  const report = useContext(PendingDrafts)
  const heldAny = Object.keys(heldStore).length > 0
  useEffect(() => { report("outline-held-yaml", heldAny) }, [report, heldAny])
  useEffect(() => () => report("outline-held-yaml", false), [report])

  const heldYaml = useMemo(() => heldByPlace(order, heldStore, text), [order, heldStore, text])
  const hold = useCallback((place: string, next: string | null) => {
    const row = order.find(r => placeOf(order, r.selection) === place)
    if (!row || row.selection.kind === "about") return
    const key = itemKey(row.selection)
    const base = itemText(text, row.selection) ?? ""
    setHeldStore(previous => withHeld(previous, key, base, next))
  }, [order, text, setHeldStore])
  return { heldYaml, hold }
}
