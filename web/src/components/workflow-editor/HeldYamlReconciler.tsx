import { useEffect } from "react"
import { itemKey, itemsOf, itemText } from "../../lib/workflowDocument"
import type { HeldYaml } from "./WorkflowOutline"

/** liveHeld is the held item YAML that still belongs to the file: its
 * item is there with the text the buffer was begun from. While the file
 * doesn't parse, nothing can be judged, so all of it is kept. */
function liveHeld(text: string, held: HeldYaml): HeldYaml {
  const { file, items } = itemsOf(text)
  if (file) return held
  const out: HeldYaml = {}
  for (const [key, kept] of Object.entries(held)) {
    const item = items.find(i => itemKey(i.item) === key)?.item
    if (item && itemText(text, item) === kept.base) out[key] = kept
  }
  return out
}

/** HeldYamlReconciler drops held item YAML whose item changed or went,
 * in either mode, so a buffer that can no longer be shown never keeps the
 * page unsaved (LEG-505). It draws nothing. */
export default function HeldYamlReconciler({ text, held, onHeld }: { text: string; held: HeldYaml; onHeld: (held: HeldYaml) => void }) {
  useEffect(() => {
    const live = liveHeld(text, held)
    if (Object.keys(live).length !== Object.keys(held).length) onHeld(live)
  }, [text, held, onHeld])
  return null
}
