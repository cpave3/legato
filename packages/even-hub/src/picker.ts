import { fitItem, LIST_ITEMS, type Item } from "./glance"
import type { Instance } from "./legato"

// What the picker page shows: the Legatos saved in the Legato app, to choose
// one. Its header is a title, any problem, and when the list was read.
export type PickerScreen = {
  title: string
  status: string
  clock: string
  items: Item[]
}

export type PickerOptions = {
  // Why the list may be stale or missing, or "".
  problem?: string
  // Whether a read has finished, so no list means none could be read.
  read?: boolean
  updatedAt?: Date
}

function clock(at: Date | undefined): string {
  if (!at) return ""
  const two = (n: number) => String(n).padStart(2, "0")
  return `${two(at.getHours())}:${two(at.getMinutes())}`
}

// The picker for the instances the Legato app listed (undefined before any
// list was read). An item's `open` is the instance's ID.
export function picker(instances: Instance[] | undefined, options: PickerOptions = {}): PickerScreen {
  let items: Item[]
  if (instances === undefined) {
    items = [{ text: options.read || options.problem ? "No Legatos to show." : "Reading the Legato app…" }]
  } else if (!instances.length) {
    items = [{ text: "Add a Legato in the Legato app." }]
  } else {
    items = instances.map((i) => ({ text: fitItem(i.name.trim() || i.id), open: i.id }))
    if (items.length > LIST_ITEMS) items = [...items.slice(0, LIST_ITEMS - 1), { text: `+${items.length - LIST_ITEMS + 1} more` }]
  }
  return { title: "Legato", status: options.problem ?? "", clock: clock(options.updatedAt), items }
}
