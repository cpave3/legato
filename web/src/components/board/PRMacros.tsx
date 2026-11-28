import { useContext } from "react"
import { Play } from "lucide-react"
import type { BoardCard, PRMetaView } from "../../lib/board-types"
import { suggestedFirst } from "../../lib/macros"
import { BoardMacrosContext, macroPR } from "./prMacrosContext"

// PRActionButton is the one button a pull request that needs the operator
// gets: its suggested macro (Address feedback on the operator's own PR,
// Review on someone else's), started with one press. The rest of the PR's
// macros are in the card's menu. 28 px tall, 44 px on a phone.
export function PRActionButton({ card, pr, action }: { card: BoardCard; pr: PRMetaView; action: "my_pr" | "others_pr" }) {
  const macros = useContext(BoardMacrosContext)
  if (!macros || card.away || !pr.repo) return null
  const { suggested } = suggestedFirst(macros.list.macros, action, macros.list.suggested)
  if (!suggested) return null
  return (
    <button
      type="button"
      aria-label={`Start ${suggested.name} on #${pr.pr_number}`}
      onClick={event => { event.stopPropagation(); macros.run(card, suggested, macroPR(pr)) }}
      onDoubleClick={event => event.stopPropagation()}
      className="inline-flex h-11 shrink-0 items-center gap-1.5 whitespace-nowrap rounded-md border border-lw-line-strong bg-lw-raised px-2.5 text-xs font-medium text-lw-text hover:bg-lw-selected md:h-7"
    >
      <Play size={11} aria-hidden="true" /><span>{suggested.name}</span>
    </button>
  )
}
