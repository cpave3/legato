import { createContext, useContext, useRef, useState } from "react"
import { ChevronDown, Play } from "lucide-react"
import type { BoardCard, PRMetaView } from "../../lib/board-types"
import { prIsMine, suggestedFirst, type Macro, type MacroList, type MacroPR } from "../../lib/macros"

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

// PRMacroButton is a pull request line's macros: a press starts the
// suggested one (Address feedback on the operator's own PR, Review on
// someone else's), and the arrow lists the rest.
export function PRMacroButton({ card, pr }: { card: BoardCard; pr: PRMetaView }) {
  const macros = useContext(BoardMacrosContext)
  const [open, setOpen] = useState(false)
  const root = useRef<HTMLSpanElement>(null)
  if (!macros || card.away || !pr.repo) return null
  const { list, suggested } = suggestedFirst(macros.list.macros, prIsMine(pr) ? "my_pr" : "others_pr", macros.list.suggested)
  if (list.length === 0) return null
  const start = (macro: Macro) => { setOpen(false); macros.run(card, macro, macroPR(pr)) }
  const first = suggested ?? list[0]
  return (
    <span ref={root} className="relative ml-auto inline-flex items-center rounded border border-lw-line" onClick={event => event.stopPropagation()} onDoubleClick={event => event.stopPropagation()}>
      <button type="button" aria-label={`Start ${first.name} on #${pr.pr_number}`} onClick={() => start(first)} className="inline-flex items-center gap-1 px-1.5 py-0.5 text-lw-text-2 hover:bg-lw-raised hover:text-lw-text">
        <Play size={11} aria-hidden="true" /><span>{first.name}</span>
      </button>
      <button type="button" aria-label={`Macros for #${pr.pr_number}`} aria-haspopup="menu" aria-expanded={open} onClick={() => setOpen(value => !value)} className="border-l border-lw-line px-0.5 py-0.5 text-lw-faint hover:bg-lw-raised hover:text-lw-text">
        <ChevronDown size={11} aria-hidden="true" />
      </button>
      {open && <span role="menu" aria-label={`Macros for #${pr.pr_number}`} className="absolute right-0 top-full z-20 mt-1 flex w-48 flex-col rounded-md border border-lw-line-strong bg-lw-surface p-1 shadow-xl" onKeyDown={event => { if (event.key === "Escape") setOpen(false) }}>
        {list.map(macro => <button key={macro.name} type="button" role="menuitem" onClick={() => start(macro)} className="truncate rounded px-2 py-1 text-left text-xs text-lw-text hover:bg-lw-raised">{macro.name}{macro === suggested ? " · suggested" : ""}</button>)}
      </span>}
    </span>
  )
}
