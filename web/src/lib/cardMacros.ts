import type { BoardCard, PRMetaView } from "./board-types"
import { prIsMine, suggestedFirst, type Macro, type MacroList } from "./macros"

// One of a card's open pull requests with the macros that apply to it, the
// suggested one first.
export interface PRMacroChoice {
  pr: PRMetaView
  macros: Macro[]
}

// openPR says whether a pull request can still take a macro: it has a
// repository and is neither merged nor closed.
export function openPR(pr: PRMetaView): boolean {
  return Boolean(pr.repo) && !["MERGED", "CLOSED"].includes(pr.state.toUpperCase())
}

// cardPRMacros is what a card's menu lists under Macros, by pull request:
// each open one in the card's order (bottom first), with the macros that
// apply to it, the suggested one first. An away card has none.
export function cardPRMacros(card: BoardCard, macros: MacroList): PRMacroChoice[] {
  if (card.away) return []
  return (card.pull_requests ?? []).filter(openPR).map(pr => ({ pr, macros: suggestedFirst(macros.macros, prIsMine(pr) ? "my_pr" : "others_pr", macros.suggested).list })).filter(choice => choice.macros.length > 0)
}

