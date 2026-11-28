import { awayActions } from "./awayReturn"
import type { BoardCard, PRMetaView } from "./board-types"
import { providerLabel } from "./providers"
import { reviewLoopKey, type FoundReviewLoop, type ReviewLoopLookup } from "./reviewLoops"

// The items of a board card's context menu, apart from how they are drawn.
// See components/board/CardContextMenu.tsx and docs/ai/dev-notes.md § Card
// context overlay.

export type CardContextAction = "open" | "move" | "delete" | "handoff" | "openOnWorker" | "openSource" | "recall"

export interface CardActionItem {
  action: CardContextAction
  label: string
}

// cardActionItems lists a card's plain actions. An away task can change only
// on its worker: it offers its details, its source, the worker, and Recall
// when the worker can be asked. Another may be moved, deleted, and handed
// off once a worker is paired.
export function cardActionItems(card: BoardCard, canHandOff: boolean): CardActionItem[] {
  const open: CardActionItem = { action: "open", label: "Open details" }
  const source: CardActionItem[] = card.source_url ? [{ action: "openSource", label: `Open in ${providerLabel(card.provider)}` }] : []
  if (card.away) return [open, ...source, ...awayItems(card.away)]
  return [
    open,
    ...source,
    { action: "move", label: "Move" },
    ...(canHandOff ? [{ action: "handoff" as const, label: "Hand off…" }] : []),
    { action: "delete", label: "Delete" },
  ]
}

function awayItems(away: NonNullable<BoardCard["away"]>): CardActionItem[] {
  const items: CardActionItem[] = []
  if (away.url) items.push({ action: "openOnWorker", label: `Open on ${away.worker}` })
  if (awayActions(away).recall) items.push({ action: "recall", label: "Recall…" })
  return items
}

export interface ReviewMenuItem {
  key: string
  label: string
  pr: PRMetaView
  scope?: "stack" | "pr"
  agentAuthor?: boolean
  loop?: FoundReviewLoop
}

const mode = (agent: boolean) => agent ? "agent" : "person"

// reviewMenuItems lists the review items for a card's open pull requests,
// each once it is looked up. One a loop holds opens that loop, whatever
// started it; any other starts review passes as an agent's or a person's.
// Several pull requests of one repository, when the review loop takes a
// scope, are each reviewed alone (scope pr), after the whole stack (started
// on the bottom one, an agent's only when every layer is) while no loop
// holds a layer of it.
export function reviewMenuItems(reviewPasses: PRMetaView[], reviewLoops: Record<string, ReviewLoopLookup> | null | undefined, reviewStack: boolean): ReviewMenuItem[] {
  const looked = reviewPasses.flatMap(pr => { const found = reviewLoops?.[reviewLoopKey(pr)]; return found ? [{ pr, ...found }] : [] })
  const several = reviewStack && looked.length > 1 && looked.every(item => item.pr.repo === looked[0].pr.repo)
  const stack = several && !looked.some(item => item.loop) ? [stackItem(looked)] : []
  return [...stack, ...looked.map(item => prItem(item, several))]
}

function stackItem(looked: ({ pr: PRMetaView } & ReviewLoopLookup)[]): ReviewMenuItem {
  const agent = looked.every(item => item.agent_author)
  const first = looked[0].pr
  const last = looked[looked.length - 1].pr
  return { key: "stack", label: `Review passes · stack #${first.pr_number}–#${last.pr_number} · ${mode(agent)}`, pr: first, scope: "stack", agentAuthor: agent }
}

function prItem(item: { pr: PRMetaView } & ReviewLoopLookup, several: boolean): ReviewMenuItem {
  const key = `${item.pr.repo}-${item.pr.pr_number}`
  if (item.loop) return { key, label: `Open review loop · #${item.pr.pr_number}`, pr: item.pr, loop: item.loop }
  return { key, label: `Review passes · #${item.pr.pr_number} · ${mode(item.agent_author)}`, pr: item.pr, scope: several ? "pr" : undefined }
}

// runReviewItem does what a review item says: opens its loop, or starts
// review passes with only the arguments its scope needs (a stack's carries
// its mode, a lone pull request's nothing more).
export function runReviewItem(item: ReviewMenuItem, start: (pr: PRMetaView, scope?: "stack" | "pr", agentAuthor?: boolean) => void, openLoop?: (loop: FoundReviewLoop) => void) {
  if (item.loop) openLoop?.(item.loop)
  else if (item.scope === "stack") start(item.pr, item.scope, item.agentAuthor)
  else if (item.scope) start(item.pr, item.scope)
  else start(item.pr)
}
