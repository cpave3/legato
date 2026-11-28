import { createContext } from "react"
import type { BoardCard, PRMetaView } from "../../lib/board-types"
import type { Macro, MacroList, MacroPR } from "../../lib/macros"

// What a board gives its cards to start macros: the macros, and how to start
// one on a card (and one of its pull requests).
export interface BoardMacros {
  list: MacroList
  run: (card: BoardCard, macro: Macro, pr?: MacroPR) => void
}

export const BoardMacrosContext = createContext<BoardMacros | null>(null)

export function macroPR(pr: PRMetaView): MacroPR {
  return { repo: pr.repo, pr_number: pr.pr_number, url: pr.pr_url || undefined }
}
