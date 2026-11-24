import type { BoardCard, PRMetaView } from "./board-types"

// openPR says whether a pull request can still take a macro: it has a
// repository and is neither merged nor closed.
export function openPR(pr: PRMetaView): boolean {
  return Boolean(pr.repo) && !["MERGED", "CLOSED"].includes(pr.state.toUpperCase())
}

// cardMacroPRs is the pull requests a card's menu offers macros for: each
// open one in the card's order (bottom first). An away card has none.
export function cardMacroPRs(card: BoardCard): PRMetaView[] {
  if (card.away) return []
  return (card.pull_requests ?? []).filter(openPR)
}
