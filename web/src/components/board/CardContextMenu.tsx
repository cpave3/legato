import { createPortal } from "react-dom"
import { useMenuLayer } from "../ui/useMenuLayer"
import { useRef, useState, useSyncExternalStore, type ReactNode, type RefObject } from "react"
import { ChevronRight, ExternalLink, Move, Play, Repeat, Send, Trash2, Undo2, Workflow } from "lucide-react"
import type { BoardCard, PRMetaView } from "../../lib/board-types"
import type { CardWorkflowOption } from "../../lib/card-workflows"
import { cardActionItems, reviewMenuItems, runReviewItem, type CardContextAction } from "../../lib/cardMenu"
import { macroAgentLine, macrosFor, prIsMine, type Macro, type MacroSuggestions } from "../../lib/macros"
import { agentKindLabel } from "../../lib/agentKind"
import { LoadingState } from "../ui/LoadingState"
import { MacroPickerList } from "../macros/MacroPicker"
import type { FoundReviewLoop, ReviewLoopLookup } from "../../lib/reviewLoops"

export type { CardContextAction } from "../../lib/cardMenu"

const subscribeViewport = (notify: () => void) => {
  window.addEventListener("resize", notify)
  return () => window.removeEventListener("resize", notify)
}
const compactViewport = () => window.innerWidth < 640

const itemClass = "flex min-h-10 w-full items-center gap-3 rounded px-3 text-left text-sm text-zinc-200 hover:bg-zinc-800 focus:bg-zinc-800 focus:outline-none"

const actionIcons: Record<CardContextAction, ReactNode> = {
  open: <ExternalLink size={16} />,
  openSource: <ExternalLink size={16} />,
  openOnWorker: <ExternalLink size={16} />,
  move: <Move size={16} />,
  handoff: <Send size={16} />,
  delete: <Trash2 size={16} />,
  recall: <Undo2 size={16} />,
}

// A menu beside the card's menu (on a phone, in its place, with a way back).
function MenuFlyout({ label, title, triggerRef, root, compact, position, onBack, onCloseAll, children }: {
  label: string
  title: string
  triggerRef: RefObject<HTMLElement | null>
  root: RefObject<HTMLDivElement | null>
  compact: boolean
  position: { x: number; y: number }
  onBack: () => void
  onCloseAll: () => void
  children: ReactNode
}) {
  const flyoutRef = useRef<HTMLDivElement>(null)
  useMenuLayer({ open: true, onClose: onCloseAll, containerRef: flyoutRef, outsideRef: root, triggerRef, placement: "side", position: compact ? position : undefined })
  return <div ref={flyoutRef} role="menu" aria-label={label} onPointerDown={event => event.stopPropagation()} className="fixed w-70 overflow-y-auto rounded-lg border border-zinc-700 bg-zinc-900 p-1 shadow-2xl" onKeyDownCapture={event => { if (event.key === "ArrowLeft" || event.key === "Escape") { event.preventDefault(); event.stopPropagation(); onBack() } }}>
    {compact && <button type="button" role="menuitem" onClick={onBack} className="min-h-10 w-full rounded px-3 text-left text-sm text-zinc-200 hover:bg-zinc-800">Back</button>}
    <p className="border-b border-zinc-800 px-3 py-2 text-xs font-medium uppercase tracking-wide text-zinc-500">{title}</p>
    {children}
  </div>
}

// A list of macros in the card menu: the shared picker's rows, sized for
// a finger, each named "Macro: <name>" with the agent and tier it starts.
function CardMacros({ macros, target, suggested, onPick }: { macros: Macro[]; target: "task" | "pr"; suggested?: string; onPick: (macro: Macro) => void }) {
  return <MacroPickerList macros={macros} targets={[target]} suggested={suggested} onPick={onPick} numbered={false} menuitem touch empty={null}
    label={macro => `Macro: ${macro.name}`} detail={macro => macroAgentLine(macro, agentKindLabel)} />
}

type FlyoutTrigger = (key: string) => {
  "aria-haspopup": "menu"
  "aria-expanded": boolean
  onMouseEnter: (event: React.MouseEvent<HTMLElement>) => void
  onKeyDown: (event: React.KeyboardEvent<HTMLElement>) => void
  onClick: (event: React.MouseEvent<HTMLElement>) => void
}

// The flyout open beside the menu (Workflows, or a pull request's Macros),
// and the props that make an item open one: on hover (not on a phone), on
// ArrowRight, and on a click. Closing it puts the focus back on its item.
function useFlyout(compact: boolean) {
  const triggerRef = useRef<HTMLElement | null>(null)
  const [flyout, setFlyout] = useState<string | null>(null)
  const open = (key: string, trigger: HTMLElement) => { triggerRef.current = trigger; setFlyout(key) }
  const close = () => { setFlyout(null); triggerRef.current?.focus() }
  const trigger: FlyoutTrigger = (key) => ({
    "aria-haspopup": "menu" as const,
    "aria-expanded": flyout === key,
    onMouseEnter: (event) => { if (!compact) open(key, event.currentTarget) },
    onKeyDown: (event) => { if (event.key === "ArrowRight") { event.preventDefault(); open(key, event.currentTarget) } },
    onClick: (event) => open(key, event.currentTarget),
  })
  return { flyout, triggerRef, trigger, close }
}

const prKey = (pr: PRMetaView) => `pr:${pr.repo}#${pr.pr_number}`

interface ReviewProps {
  reviewPasses: PRMetaView[]
  reviewStack: boolean
  reviewUnavailable?: string
  reviewLoops?: Record<string, ReviewLoopLookup> | null
  onReviewPasses?: (pr: PRMetaView, scope?: "stack" | "pr", agentAuthor?: boolean) => void
  onOpenReviewLoop?: (loop: FoundReviewLoop) => void
}

// The review items of a card's open pull requests (see reviewMenuItems), or
// why none can start while a card has pull requests to review.
function ReviewSection({ reviewPasses, reviewStack, reviewUnavailable, reviewLoops, onReviewPasses, onOpenReviewLoop, pick }: ReviewProps & { pick: (run: () => void) => void }) {
  if (!onReviewPasses) return null
  if (reviewUnavailable) {
    if (reviewPasses.length === 0) return null
    return <p className="flex items-start gap-3 px-3 py-2 text-xs text-zinc-500"><Repeat size={16} className="shrink-0" /><span>{reviewUnavailable}</span></p>
  }
  return <>
    {reviewMenuItems(reviewPasses, reviewLoops, reviewStack).map(item => <button key={`review-${item.key}`} type="button" role="menuitem" onClick={() => pick(() => runReviewItem(item, onReviewPasses, onOpenReviewLoop))} className={itemClass}><Repeat size={16} /><span>{item.label}</span></button>)}
  </>
}

interface MacroProps {
  macros: Macro[]
  suggested?: MacroSuggestions
  onMacro?: (macro: Macro) => void
  macroPRs: PRMetaView[]
  onPRMacro?: (pr: PRMetaView, macro: Macro) => void
  onManageMacros?: () => void
}

// The pull requests whose Macros a card offers: none while no macro is for
// a pull request, or nothing would start one.
function prsWithMacros(macros: Macro[], macroPRs: PRMetaView[], onPRMacro?: unknown): PRMetaView[] {
  if (!onPRMacro || macrosFor(macros, "pr").length === 0) return []
  return macroPRs
}

// The Macros section: the task's macros, an item per pull request that
// opens its macros beside the menu, and a link to the Macros page. Shown
// only when there is a macro to start.
function MacroSection({ macros, suggested, onMacro, macroPRs, onPRMacro, onManageMacros, trigger, pick }: MacroProps & { trigger: FlyoutTrigger; pick: (run: () => void) => void }) {
  const prs = prsWithMacros(macros, macroPRs, onPRMacro)
  const taskMacros = Boolean(onMacro) && macrosFor(macros, "task").length > 0
  if (!taskMacros && prs.length === 0) return null
  return <>
    <p className="mt-1 border-t border-zinc-800 px-3 pb-1 pt-2 text-xs font-medium uppercase tracking-wide text-zinc-500">Macros</p>
    {onMacro && <CardMacros macros={macros} target="task" suggested={suggested?.task} onPick={macro => pick(() => onMacro(macro))} />}
    {prs.map(pr => <button key={prKey(pr)} type="button" role="menuitem" aria-label={`Macros for #${pr.pr_number}`} {...trigger(prKey(pr))} className={itemClass}><Play size={16} className="shrink-0" /><span className="min-w-0 truncate"><span className="font-lw-mono">#{pr.pr_number}</span> <span className="text-[11px] text-lw-text-2">{pr.repo}</span></span><ChevronRight className="ml-auto shrink-0" size={15} /></button>)}
    {onManageMacros && <button type="button" role="menuitem" onClick={() => pick(onManageMacros)} className="flex min-h-10 w-full items-center rounded px-3 text-left text-sm text-lw-link hover:bg-zinc-800 focus:bg-zinc-800 focus:outline-none">Manage macros</button>}
  </>
}

function WorkflowList({ workflows, loading, warning, onPick }: { workflows: CardWorkflowOption[]; loading: boolean; warning: string; onPick: (workflow: CardWorkflowOption) => void }) {
  return <>
    {loading && <LoadingState size="inline" label="Loading workflows…" className="px-3 py-2" />}
    {!loading && workflows.length === 0 && <p className="px-3 py-2 text-sm text-zinc-500">No eligible workflows</p>}
    {workflows.map(workflow => <button key={workflow.definition.name} type="button" role="menuitem" onClick={() => onPick(workflow)} className="flex min-h-10 w-full items-center rounded px-3 text-left text-sm text-zinc-200 hover:bg-zinc-800 focus:bg-zinc-800 focus:outline-none"><span className="truncate">{workflow.definition.title}</span></button>)}
    {warning && <p className="border-t border-zinc-800 px-3 py-2 text-xs text-amber-300">{warning}</p>}
  </>
}

export function CardContextMenu({
  card,
  position,
  workflows = [],
  workflowLoading = false,
  workflowWarning = "",
  canHandOff = false,
  reviewPasses = [],
  reviewStack = false,
  reviewUnavailable,
  reviewLoops,
  onReviewPasses,
  onOpenReviewLoop,
  macros = [],
  onMacro,
  suggested,
  macroPRs = [],
  onPRMacro,
  onManageMacros,
  onAction,
  onWorkflow,
  onClose,
}: {
  card: BoardCard
  position: { x: number; y: number }
  workflows?: CardWorkflowOption[]
  workflowLoading?: boolean
  workflowWarning?: string
  // Whether this main has a worker to hand the task off to.
  canHandOff?: boolean
  // The card's open pull requests a review can be about: one item each, so
  // a card with several asks which.
  reviewPasses?: PRMetaView[]
  // Whether the review loop workflow takes a scope. Then a card whose pull
  // requests (in stack order) are several in one repository, none held by
  // a loop, offers the whole stack first, started on the bottom one, and
  // each one alone.
  reviewStack?: boolean
  // Why no review can start (no Legato on the data folder runs the review
  // engine): said in place of the review items, which are not offered.
  reviewUnavailable?: string
  // Each pull request looked up (GET /api/review-loops), by reviewLoopKey:
  // one a loop holds offers that loop, any other a review that says
  // whether it is an agent's or a person's. A pull request not looked up
  // yet offers nothing.
  reviewLoops?: Record<string, ReviewLoopLookup> | null
  // A stack's start carries the menu's mode for the whole stack: an
  // agent's only when every layer is.
  onReviewPasses?: (pr: PRMetaView, scope?: "stack" | "pr", agentAuthor?: boolean) => void
  // Opens the loop that holds a pull request, whatever started it.
  onOpenReviewLoop?: (loop: FoundReviewLoop) => void
  // Every macro. The task's list keeps those for a task, each pull
  // request's those for a pull request, through the shared picker: the
  // favourites, and the rest after Show all.
  macros?: Macro[]
  // The macro each list puts first.
  suggested?: MacroSuggestions
  // A pick in the task's list starts the task's agent with it, or queues it
  // to the agent running.
  onMacro?: (macro: Macro) => void
  // The card's open pull requests, bottom first: one item each opens its
  // macros, and a pick starts the macro on that PR.
  macroPRs?: PRMetaView[]
  onPRMacro?: (pr: PRMetaView, macro: Macro) => void
  // Opens the Macros page.
  onManageMacros?: () => void
  onAction: (action: CardContextAction) => void
  onWorkflow?: (workflow: CardWorkflowOption) => void
  onClose: () => void
}) {
  const menuRef = useRef<HTMLDivElement>(null)
  const root = useRef<HTMLDivElement>(null)
  const compact = useSyncExternalStore(subscribeViewport, compactViewport)
  const { flyout, triggerRef: flyoutTriggerRef, trigger: flyoutTrigger, close: closeFlyout } = useFlyout(compact)
  const { restoreFocus } = useMenuLayer({ open: true, onClose, containerRef: menuRef, outsideRef: root, position })
  const pick = (run: () => void) => { restoreFocus(); onClose(); run() }
  const closeAll = () => { restoreFocus(); onClose() }
  const openPR = prsWithMacros(macros, macroPRs, onPRMacro).find(pr => prKey(pr) === flyout)
  const flyoutProps = { triggerRef: flyoutTriggerRef, root, compact, position, onBack: closeFlyout, onCloseAll: closeAll }
  return createPortal(<div ref={root} className="fixed inset-0 z-40" onPointerDown={onClose} data-testid="card-context-backdrop">
    <div ref={menuRef} role="menu" aria-label={`Actions for ${card.title}`} onPointerDown={event => event.stopPropagation()} className="fixed w-60 rounded-lg border border-zinc-700 bg-zinc-900 p-1 shadow-2xl" style={{ visibility: compact && flyout ? "hidden" : undefined }}>
      <p className="truncate border-b border-zinc-800 px-3 py-2 text-xs text-zinc-500">{card.id} · {card.title}</p>
      {cardActionItems(card, canHandOff).map(item => <button key={item.action} type="button" role="menuitem" onClick={() => pick(() => onAction(item.action))} className={`flex min-h-10 w-full items-center gap-3 rounded px-3 text-left text-sm hover:bg-zinc-800 focus:bg-zinc-800 focus:outline-none ${item.action === "delete" ? "text-red-300" : "text-zinc-200"}`}>{actionIcons[item.action]}<span>{item.label}</span></button>)}
      {!card.away && <>
        <ReviewSection reviewPasses={reviewPasses} reviewStack={reviewStack} reviewUnavailable={reviewUnavailable} reviewLoops={reviewLoops} onReviewPasses={onReviewPasses} onOpenReviewLoop={onOpenReviewLoop} pick={pick} />
        <MacroSection macros={macros} suggested={suggested} onMacro={onMacro} macroPRs={macroPRs} onPRMacro={onPRMacro} onManageMacros={onManageMacros} trigger={flyoutTrigger} pick={pick} />
        <button type="button" role="menuitem" {...flyoutTrigger("workflows")} className="mt-1 flex min-h-10 w-full items-center gap-3 border-t border-zinc-800 px-3 pt-1 text-left text-sm text-zinc-200 hover:bg-zinc-800 focus:bg-zinc-800 focus:outline-none"><Workflow size={16} /><span>Workflows</span><ChevronRight className="ml-auto" size={15} /></button>
      </>}
    </div>
    {flyout === "workflows" && <MenuFlyout key="workflows" label={`Workflows for ${card.title}`} title="Workflows" {...flyoutProps}>
      <WorkflowList workflows={workflows} loading={workflowLoading} warning={workflowWarning} onPick={workflow => pick(() => onWorkflow?.(workflow))} />
    </MenuFlyout>}
    {openPR && onPRMacro && <MenuFlyout key={flyout} label={`Macros for #${openPR.pr_number}`} title={`Macros · #${openPR.pr_number}`} {...flyoutProps}>
      <CardMacros macros={macros} target="pr" suggested={suggested?.[prIsMine(openPR) ? "my_pr" : "others_pr"]} onPick={macro => pick(() => onPRMacro(openPR, macro))} />
    </MenuFlyout>}
  </div>, document.body)
}
