import { useState } from "react"
import { Link } from "react-router-dom"
import { RefreshCw } from "lucide-react"
import { useShipping } from "../hooks/useShipping"
import { useServer } from "../hooks/useServer"
import { useNow } from "../hooks/useNow"
import { workflowStepAction, type ReviewLoopAction } from "../lib/workflow"
import { authorState, ciTrouble, ciWords, draftProblemWords, inFilter, outcomeWord, phaseWords, stewardWords, type ShippingFilter, type ShippingLoop } from "../lib/shipping"
import { endConfirmation, reviewLoopActionLabels } from "../lib/reviewLoopActions"
import { ago } from "../lib/workflowView"
import { Button } from "../components/ui/Button"
import { LoadingState } from "../components/ui/LoadingState"
import { useConfirm } from "../components/ui/useConfirm"
import { cn } from "../lib/utils"

// The Shipping page (LEG-706): every review loop Legato looks after,
// whichever caller opened it, with where it stands, its pull requests' CI
// and mergeability, the steward, the author and the reviewer, and the
// actions its waiting step takes. See docs/ai/web-ui.md § Shipping.

const filters: { value: ShippingFilter; label: string }[] = [
  { value: "needs", label: "Needs you" },
  { value: "active", label: "Active" },
  { value: "ended", label: "Recently ended" },
]

// The actions a row offers: those of the run page but Message the author,
// which needs the run page's message box.
const rowActions: ReviewLoopAction[] = ["another_pass", "stop_watching", "watch_again", "end"]

export function ShippingPage() {
  const shipping = useShipping()
  const now = useNow(30_000)
  const [filter, setFilter] = useState<ShippingFilter>("active")
  const loops = shipping.data?.loops ?? []
  const shown = loops.filter(loop => inFilter(loop, filter))
  const anyOpen = loops.some(loop => inFilter(loop, "active"))
  return <main className="flex min-h-0 flex-1 flex-col overflow-y-auto px-4 py-4 font-lw text-lw-text sm:px-6 md:py-6">
    <div className="mx-auto w-full max-w-5xl">
      <header className="flex items-center justify-between gap-4">
        <h1 className="text-xl font-semibold">Shipping</h1>
        <Button variant="ghost" size="icon" aria-label="Refresh shipping" onClick={() => { void shipping.refresh() }}><RefreshCw size={16} /></Button>
      </header>
      {shipping.loading && <LoadingState label="Loading review loops…" size="inline" className="mt-6" />}
      {shipping.error && <p role="alert" className="mt-4 text-sm text-lw-error">{shipping.error}</p>}
      {loops.length > 0 && <div role="group" aria-label="Show" className="mt-4 flex flex-wrap gap-2">
        {filters.map(item => {
          const count = loops.filter(loop => inFilter(loop, item.value)).length
          return <button key={item.value} type="button" aria-pressed={filter === item.value} onClick={() => setFilter(item.value)}
            className={cn("inline-flex min-h-9 items-center gap-1.5 rounded-full border px-3 text-[13px]", filter === item.value ? "border-lw-accent bg-lw-selected text-lw-text" : "border-lw-line-strong bg-lw-surface text-lw-text-2 hover:text-lw-text")}>
            {item.label} <span className={cn("font-lw-mono text-xs", item.value === "needs" && count > 0 && "text-lw-needs")}>{count}</span>
          </button>
        })}
      </div>}
      {!shipping.loading && !shipping.error && !anyOpen && filter !== "ended" && <p className="mt-6 text-sm text-lw-text-2">No review loops are open.</p>}
      {shown.length > 0 && <ul aria-label="Review loops" className="mt-4 flex flex-col gap-3">
        {shown.map(loop => <ShippingRow key={loop.loop_id} loop={loop} now={now} refresh={shipping.refresh} />)}
      </ul>}
      {filter !== "active" && loops.length > 0 && shown.length === 0 && <p className="mt-6 text-sm text-lw-text-2">{filter === "needs" ? "Nothing needs you." : "No review loop ended in the last 7 days."}</p>}
    </div>
  </main>
}

function taskLink(loop: ShippingLoop): string {
  return `/board?highlight_task=${encodeURIComponent(loop.task?.id ?? "")}`
}

function ShippingRow({ loop, now, refresh }: { loop: ShippingLoop; now: number; refresh: () => Promise<void> }) {
  const { baseUrl } = useServer()
  const { confirm, dialog } = useConfirm()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const active = loop.open || loop.phase === "waiting_for_merge"
  const actions = loop.run_id && loop.step ? (loop.actions ?? []).filter(action => rowActions.includes(action)) : []
  const ci = ciWords(loop)
  const draftProblem = draftProblemWords(loop)
  const steward = stewardWords(loop, now)
  const title = loop.task?.title || loop.task?.id || loop.repo

  async function act(action: ReviewLoopAction) {
    if (!loop.run_id || !loop.step) return
    if (action === "end" && !(await confirm(endConfirmation(Boolean(loop.steward))))) return
    setError("")
    setBusy(true)
    try {
      await workflowStepAction(baseUrl, loop.run_id, loop.step, action)
      await refresh()
    } catch (c) {
      setError(c instanceof Error ? c.message : String(c))
    } finally {
      setBusy(false)
    }
  }

  return <li className={cn("rounded-lg border bg-lw-surface p-3 text-sm md:grid md:grid-cols-[minmax(0,2fr)_minmax(0,2fr)_minmax(0,1.5fr)] md:gap-4",
    loop.needs_you ? "border-lw-needs" : "border-lw-line")}>
    <div className="min-w-0">
      {loop.task ? <Link to={taskLink(loop)} className="font-medium text-lw-text underline-offset-2 hover:underline">{title}</Link> : <span className="font-medium">{title}</span>}
      <p className="mt-0.5 flex flex-wrap items-baseline gap-x-1.5 text-xs text-lw-text-2">
        <span className="font-lw-mono">{loop.repo}</span>
        {loop.layers.map(layer => <a key={layer.number} href={layer.url} target="_blank" rel="noreferrer" title={layer.title}
          className={cn("font-lw-mono text-lw-link underline", layer.draft && "opacity-80")}>#{layer.number}</a>)}
      </p>
      <p className="mt-0.5 text-xs text-lw-faint">{loop.caller}</p>
    </div>
    <div className="mt-2 min-w-0 md:mt-0">
      <p className={cn("font-medium", loop.needs_you ? "text-lw-needs" : active ? "text-lw-text" : "text-lw-text-2")}>{active ? phaseWords(loop) : outcomeWord(loop.outcome)}</p>
      {loop.phase === "stuck" && loop.reason && <p className="text-xs text-lw-text-2">{loop.reason}</p>}
      {ci && <p className={cn("text-xs", ciTrouble(loop) ? "text-lw-needs" : "text-lw-text-2")}>{ci}</p>}
      {active && draftProblem && <p className="text-xs text-lw-needs">{draftProblem}</p>}
      {steward && <p className="text-xs text-lw-text-2">{steward}</p>}
      {!active && loop.closed_at && <p className="text-xs text-lw-text-2">{ago(loop.closed_at, now) === "now" ? "just now" : `${ago(loop.closed_at, now)} ago`}</p>}
    </div>
    <div className="mt-2 min-w-0 text-xs text-lw-text-2 md:mt-0">
      {loop.author && <p>Author: {loop.author.task} · {authorState(loop.author.state)}</p>}
      {loop.reviewer && <p>Reviewer: {loop.reviewer}</p>}
      <div className="mt-2 flex flex-wrap items-center gap-2">
        {loop.link && <Button asChild variant="outline" size="sm"><Link to={loop.link}>{loop.kind === "handoff" && !loop.run_id ? "Open card" : "Open run"}</Link></Button>}
        {active && actions.map(action => <Button key={action} size="sm" variant={action === "end" ? "ghost" : "outline"} disabled={busy} onClick={() => { void act(action) }}>
          {reviewLoopActionLabels[action]}
        </Button>)}
      </div>
      {error && <p role="alert" className="mt-1 text-lw-error">{error}</p>}
    </div>
    {dialog}
  </li>
}
