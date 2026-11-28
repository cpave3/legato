import { useEffect, useState } from "react"
import type { Selection } from "../../lib/workflowAddress"
import { useCardFolds } from "../workflow-canvas/useCardFolds"

// cardOfKind is the card over the graph that holds a row of each kind.
const cardOfKind: Record<Selection["kind"], string> = { trigger: "triggers", input: "inputs", step: "steps", about: "about" }

/** useCanvasDock is the canvas's cards, folded or not, and the node to
 * centre; the card that holds the row chosen opens, so the row is never
 * hidden. */
export function useCanvasDock(canvasOn: boolean, selection: Selection | null) {
  const folds = useCardFolds("legato:workflow-editor:folds", {})
  const { open: openCard } = folds
  const [centerOn, setCenterOn] = useState<{ id: string; n: number } | null>(null)
  const centre = (id: string | null) => { if (id) setCenterOn(current => ({ id, n: (current?.n ?? 0) + 1 })) }
  const selectedCard = selection ? cardOfKind[selection.kind] : null
  const selectedFolded = selectedCard ? folds.folded(selectedCard) : false
  useEffect(() => { if (canvasOn && selectedCard && selectedFolded) openCard(selectedCard) }, [canvasOn, selectedCard, selectedFolded, openCard])
  return { folds, centerOn, centre }
}
