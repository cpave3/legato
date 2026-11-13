import { useState, type DragEvent } from "react"

// A column, a rail or the Away lane that takes a card dragged from a column:
// onCardDrop is told the card's ID, the target's name and the drop, which
// says where in the target it was let go.
export function useDropTarget(name: string, onCardDrop?: (cardId: string, column: string, event: DragEvent<HTMLElement>) => void) {
  const [dragOver, setDragOver] = useState(false)
  return {
    dragOver,
    handlers: {
      onDragOver: (event: DragEvent<HTMLElement>) => {
        if (!onCardDrop) return
        event.preventDefault()
        event.dataTransfer.dropEffect = "move"
        setDragOver(true)
      },
      onDragLeave: (event: DragEvent<HTMLElement>) => {
        if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setDragOver(false)
      },
      onDrop: (event: DragEvent<HTMLElement>) => {
        event.preventDefault()
        setDragOver(false)
        const cardId = event.dataTransfer.getData("text/plain")
        if (cardId) onCardDrop?.(cardId, name, event)
      },
    },
  }
}
