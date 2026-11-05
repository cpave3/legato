import { useEffect, useState } from "react"
import { ChevronDown, ChevronRight } from "lucide-react"

import { awayGroups, awayLaneName } from "../../lib/away"
import { doneOnWorker } from "../../lib/awayReturn"
import type { BoardCard, BoardWorker } from "../../lib/board-types"
import { AwayRowActions, ReturnAllDoneButton } from "./AwayActions"
import { AwayRow, WorkerReach } from "./AwayLane"

// The Away section at the top of the phone's board: the tasks away on
// workers, one line per worker that holds some ("vm-1 · 6 away · 2 done")
// with its Return all done. Tapping a line shows its rows, each with the
// actions its state allows, as in the Away lane. See docs/ai/web-ui.md
// § The Away lane.

interface MobileAwaySectionProps {
  workers: BoardWorker[]
  // The Away lane's cards, in the order of its groups (withAwayLane).
  cards: BoardCard[]
  highlightedTaskId?: string | null
  // A row tapped: its index among cards.
  onRowClick: (index: number) => void
  // The server Return, Recall and Return all done call; without it the
  // section offers none.
  baseUrl?: string
  onNotice?: (text: string, error: boolean) => void
}

// The line that stands for a worker while folded.
function awayLine(worker: BoardWorker, cards: BoardCard[]): string {
  const done = doneOnWorker(cards).length
  return `${worker.name} · ${cards.length} away${done > 0 ? ` · ${done} done` : ""}`
}

function workerOf(cards: BoardCard[], taskId?: string | null): string | undefined {
  return taskId ? cards.find((card) => card.id === taskId)?.away?.worker_instance_id : undefined
}

export function MobileAwaySection({ workers, cards, highlightedTaskId, onRowClick, baseUrl, onNotice }: MobileAwaySectionProps) {
  const [open, setOpen] = useState<Set<string>>(() => new Set(workerOf(cards, highlightedTaskId) ?? []))
  const highlightedWorker = workerOf(cards, highlightedTaskId)
  useEffect(() => {
    if (highlightedWorker) setOpen((current) => (current.has(highlightedWorker) ? current : new Set(current).add(highlightedWorker)))
  }, [highlightedWorker])

  const groups = awayGroups(workers, cards).filter((group) => group.cards.length > 0)
  if (groups.length === 0) return null
  const toggle = (id: string) => setOpen((current) => {
    const next = new Set(current)
    if (!next.delete(id)) next.add(id)
    return next
  })

  return (
    <section aria-label="Away on workers" className="flex max-h-[50vh] shrink-0 flex-col gap-1.5 overflow-y-auto px-4 pb-2.5">
      <h2 className="m-0 px-1 text-xs font-semibold uppercase tracking-[1px] text-lw-accent-text">{awayLaneName}</h2>
      {groups.map(({ worker, cards: groupCards }) => {
        const expanded = open.has(worker.instance_id)
        return (
          <div key={worker.instance_id} className="overflow-hidden rounded-lg border border-lw-line bg-lw-surface">
            <div className="flex items-center gap-2 pr-2">
              <button
                type="button"
                aria-expanded={expanded}
                onClick={() => toggle(worker.instance_id)}
                className="flex min-h-11 min-w-0 flex-1 items-center gap-1.5 px-3 text-left text-sm text-lw-text"
              >
                {expanded ? <ChevronDown size={14} aria-hidden="true" className="shrink-0 text-lw-text-2" /> : <ChevronRight size={14} aria-hidden="true" className="shrink-0 text-lw-text-2" />}
                <span className="truncate">{awayLine(worker, groupCards)}</span>
              </button>
              {baseUrl !== undefined && <ReturnAllDoneButton baseUrl={baseUrl} worker={worker} cards={groupCards} onNotice={onNotice} />}
            </div>
            {expanded && (
              <div role="group" aria-label={worker.name} className="border-t border-lw-line">
                <div className="px-3 pt-1.5"><WorkerReach worker={worker} /></div>
                {groupCards.map((card) => (
                  <AwayRow
                    key={card.id}
                    card={card}
                    selected={false}
                    highlighted={card.id === highlightedTaskId}
                    onClick={() => onRowClick(cards.indexOf(card))}
                    actions={baseUrl === undefined ? undefined : <AwayRowActions baseUrl={baseUrl} card={card} onNotice={onNotice} />}
                  />
                ))}
              </div>
            )}
          </div>
        )
      })}
    </section>
  )
}
