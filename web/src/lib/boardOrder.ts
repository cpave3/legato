// Where a card dragged over a column will land: the slot between its cards
// under the pointer, and the card it then goes before.

export interface CardBox {
  top: number
  height: number
}

// The slot (0 to cards.length) for a pointer at y: above the first card
// whose middle is below the pointer, else the end of the column.
export function dropSlot(cards: CardBox[], y: number): number {
  const index = cards.findIndex((card) => y < card.top + card.height / 2)
  return index === -1 ? cards.length : index
}

export interface Placement {
  // The card the dragged one goes before; null for the end of the column.
  before: string | null
  // The dragged card is in this column and the slot is just above or just
  // below it: dropping there changes nothing.
  unchanged: boolean
}

// The placement of the card draggedId dropped at slot among a column's cards
// (as shown, which may be filtered: the server puts it before the same card
// in the whole column).
export function dropPlacement(cardIds: string[], draggedId: string, slot: number): Placement {
  const before = cardIds.slice(slot).find((id) => id !== draggedId) ?? null
  const at = cardIds.indexOf(draggedId)
  const unchanged = at !== -1 && (slot === at || slot === at + 1)
  return { before, unchanged }
}
