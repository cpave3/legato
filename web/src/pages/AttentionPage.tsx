import { Link } from "react-router-dom"
import { AlertTriangle, Bell, Check, CheckCircle, ChevronRight, ExternalLink, GitPullRequest, ScanEye, type LucideIcon } from "lucide-react"
import { useState } from "react"
import { useAttention } from "../hooks/useAttention"
import { useServer } from "../hooks/useServer"
import { useSettings } from "../hooks/useSettings"
import { SetupChecklistButton } from "../components/SetupChip"
import { executeAttentionAction, dismissAttentionItem, type AttentionAction, type AttentionItem } from "../lib/attention"
import { MacroPickerButton } from "../components/MacroPicker"
import { externalHref, legatoHref } from "../lib/navigation"
import { LoadingState } from "../components/ui/LoadingState"
import { InlineNotice } from "../components/ui/InlineNotice"
import { Button } from "../components/ui/Button"
import { MarkdownBody } from "../components/MarkdownBody"
import { AskReeve } from "../components/AskReeve"
import { reeveBrief } from "../lib/reeve"

function formatRelativeTime(iso: string): string {
  const date = new Date(iso)
  const now = new Date()
  const diffMins = Math.floor((now.getTime() - date.getTime()) / 60000)
  if (diffMins < 1) return "just now"
  if (diffMins < 60) return `${diffMins}m ago`
  const diffHours = Math.floor(diffMins / 60)
  if (diffHours < 24) return `${diffHours}h ago`
  return `${Math.floor(diffHours / 24)}d ago`
}

function navigationPath(item: AttentionItem): string | null {
  return legatoHref(item.navigation)
}

// The signals that block a pull request's merge: Legato's own, and a CI
// case Argus follows.
const mergeSignals = new Set(["pr_merge_blocked", "argus_case"])

// An integration's name as the label of a link to its own page.
function producerLabel(producer: string): string {
  return producer.charAt(0).toUpperCase() + producer.slice(1)
}

// The signals of work ready for the person to look at and bring home: a
// review tour, and a task done on a worker.
const reviewSignals = new Set(["review_ready", "handoff_done", "prs_unreviewed"])

// Where a navigate action with an address of its own goes: outside Legato
// (in a new tab), or a path of this web UI.
function actionHref(action: AttentionAction): { external: string } | { path: string } | null {
  if (!action.href) return null
  if (action.href.startsWith("/")) return { path: action.href }
  const external = externalHref({ kind: "external", target: action.href })
  return external ? { external } : null
}

// A detail longer than this is folded until the person asks for more.
function isLongDetail(detail: string): boolean {
  return detail.length > 280 || detail.split("\n").filter((line) => line.trim() !== "").length > 3
}

function AttentionDetail({ detail }: { detail: string }) {
  const [open, setOpen] = useState(false)
  const long = isLongDetail(detail)
  return (
    <div className="mt-1 text-xs text-zinc-500">
      <div className={long && !open ? "max-h-16 overflow-hidden" : undefined}>
        <MarkdownBody body={detail} skipHtml disableImages />
      </div>
      {long && (
        <button type="button" onClick={() => setOpen(!open)} className="mt-1 min-h-11 text-xs text-zinc-400 hover:text-zinc-200">
          {open ? "Less" : "More"}
        </button>
      )}
    </div>
  )
}

type AttentionSection = {
  id: "intervention" | "review" | "merge"
  title: string
  description?: string
  emptyLabel: string
  icon: LucideIcon
  matches: (item: AttentionItem) => boolean
}

const sections: AttentionSection[] = [
  {
    id: "intervention",
    title: "Needs intervention",
    description: "Decisions, failed work, and agents waiting for you",
    emptyLabel: "No interventions needed",
    icon: AlertTriangle,
    matches: (item) => !reviewSignals.has(item.signal_kind) && !mergeSignals.has(item.signal_kind),
  },
  {
    id: "review",
    title: "Ready for review",
    description: "Agent work handed over for review, and tasks done on a worker",
    emptyLabel: "No work is ready for review",
    icon: ScanEye,
    matches: (item) => reviewSignals.has(item.signal_kind),
  },
  {
    id: "merge",
    title: "Merge blockers",
    emptyLabel: "No known merge blockers",
    icon: GitPullRequest,
    matches: (item) => mergeSignals.has(item.signal_kind),
  },
]

const urgencyOrder = ["critical", "high", "normal", "low"]

function sortedItems(items: AttentionItem[]): AttentionItem[] {
  return [...items].sort((a, b) => {
    const urgency = urgencyOrder.indexOf(a.urgency) - urgencyOrder.indexOf(b.urgency)
    return urgency !== 0 ? urgency : b.updated_at.localeCompare(a.updated_at)
  })
}

type AttentionCardProps = {
  item: AttentionItem
  pending: boolean
  onAction: (item: AttentionItem, actionId: string, confirmed: boolean, text?: string) => void
  onDismiss: (item: AttentionItem) => void
}

// CopyButton puts a copy action's text on the clipboard; it calls nothing.
function CopyButton({ action }: { action: AttentionAction }) {
  const [copied, setCopied] = useState(false)
  return (
    <button
      type="button"
      onClick={() => {
        void navigator.clipboard?.writeText(action.text ?? "").then(() => setCopied(true), () => setCopied(false))
      }}
      title={action.text}
      className="flex min-h-11 items-center rounded-lg border border-zinc-700 px-3 text-sm text-zinc-200 hover:bg-zinc-800"
    >
      {action.label}{copied && <Check size={14} className="ml-1.5" aria-hidden="true" />}
    </button>
  )
}

// ReplyBox is an action that needs the operator's words: a line of text
// and the action's button, which sends it.
function ReplyBox({ action, pending, onSend }: { action: AttentionAction; pending: boolean; onSend: (text: string) => void }) {
  const [text, setText] = useState("")
  const send = () => {
    if (!text.trim()) return
    onSend(text.trim())
    setText("")
  }
  return (
    <form
      className="flex w-full items-center gap-2 sm:w-auto"
      onSubmit={(event) => {
        event.preventDefault()
        send()
      }}
    >
      <input
        aria-label={action.label}
        value={text}
        onChange={(event) => setText(event.target.value)}
        disabled={pending}
        placeholder="Your answer"
        className="min-h-11 min-w-0 flex-1 rounded-lg border border-zinc-700 bg-zinc-950 px-3 text-sm text-zinc-100 sm:w-56"
      />
      <button
        type="submit"
        disabled={pending || !text.trim()}
        className="min-h-11 rounded-lg bg-indigo-500 px-3 text-sm font-medium text-[var(--theme-canvas,#fff)] hover:bg-indigo-400 disabled:opacity-50"
      >
        {action.label}
      </button>
    </form>
  )
}

function AttentionCard({ item, pending, onAction, onDismiss }: AttentionCardProps) {
  const navPath = navigationPath(item)
  const external = externalHref(item.navigation)
  const urgent = item.urgency === "high" || item.urgency === "critical"

  return (
    <li className="rounded-xl border border-zinc-800 bg-zinc-900/60 p-4">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="font-medium text-zinc-100">{item.title}</h3>
            <span className="rounded bg-zinc-800 px-1.5 py-0.5 text-xs text-zinc-400">{item.producer}</span>
            <span className={`rounded px-1.5 py-0.5 text-xs ${urgent ? "bg-red-500/20 text-red-400" : "bg-zinc-800 text-zinc-500"}`}>{item.urgency}</span>
            <span className="text-xs text-zinc-600">{formatRelativeTime(item.updated_at)}</span>
          </div>
          <p className="mt-1 text-sm text-zinc-300">{item.reason}</p>
          {item.detail && <AttentionDetail detail={item.detail} />}
        </div>
        <div className="flex shrink-0 flex-wrap items-center gap-2 sm:justify-end">
          {(item.actions ?? []).filter((action) => action.kind === "copy").map((action) => (
            <CopyButton key={action.id} action={action} />
          ))}
          {(item.actions ?? []).filter((action) => action.kind === "execute" && action.requires_text).map((action) => (
            <ReplyBox key={action.id} action={action} pending={pending} onSend={(text) => onAction(item, action.id, action.requires_confirmation ?? false, text)} />
          ))}
          {(item.actions ?? []).filter((action) => action.kind === "execute" && action.picks === "macro").map((action) => (
            <MacroPickerButton key={action.id} label={action.label} disabled={pending} targets={["task", "pr"]}
              className="min-h-11 rounded-lg bg-indigo-500 px-3 text-sm font-medium text-[var(--theme-canvas,#fff)] hover:bg-indigo-400 disabled:opacity-50"
              onPick={(macro) => onAction(item, action.id, action.requires_confirmation ?? false, macro.name)} />
          ))}
          {(item.actions ?? []).filter((action) => action.kind === "execute" && !action.requires_text && !action.picks).map((action) => (
            <button
              key={action.id}
              type="button"
              disabled={pending}
              onClick={() => {
                if (action.confirmation && !window.confirm(action.confirmation)) return
                onAction(item, action.id, action.requires_confirmation ?? false)
              }}
              className="min-h-11 rounded-lg bg-indigo-500 px-3 text-sm font-medium text-[var(--theme-canvas,#fff)] hover:bg-indigo-400 disabled:opacity-50"
            >
              {action.label}
            </button>
          ))}
          {item.producer !== "reeve-peer" && (
            <AskReeve brief={reeveBrief({
              what: `the Attention item “${item.title}”`,
              ids: { "Attention item": item.id, task: item.task_id, source: `${item.producer} ${item.source_key}` },
              error: item.reason,
            })} />
          )}
          <button
            type="button"
            disabled={pending}
            onClick={() => onDismiss(item)}
            className="min-h-11 rounded-lg px-3 text-sm text-zinc-500 hover:bg-zinc-800 hover:text-zinc-300 disabled:opacity-50"
          >
            Dismiss
          </button>
          {(item.actions ?? []).filter((action) => action.kind === "navigate").map((action) => {
            const own = actionHref(action)
            if (own && "external" in own) {
              return (
                <a key={action.id} href={own.external} target="_blank" rel="noopener noreferrer" className="flex min-h-11 items-center rounded-lg border border-zinc-700 px-3 text-sm text-zinc-200 hover:bg-zinc-800">
                  {action.label} <ExternalLink size={14} className="ml-1.5" />
                </a>
              )
            }
            const to = own ? own.path : navPath
            if (!to) return null
            return (
              <Link key={action.id} to={to} className="flex min-h-11 items-center rounded-lg border border-zinc-700 px-3 text-sm text-zinc-200 hover:bg-zinc-800">
                {action.label} <ChevronRight size={16} className="ml-1" />
              </Link>
            )
          })}
          {external && item.task_id && (
            <Link to={`/board?highlight_task=${encodeURIComponent(item.task_id)}`} className="flex min-h-11 items-center rounded-lg border border-zinc-700 px-3 text-sm text-zinc-200 hover:bg-zinc-800">
              Open task <ChevronRight size={16} className="ml-1" />
            </Link>
          )}
          {external && (
            <a href={external} target="_blank" rel="noopener noreferrer" className="flex min-h-11 items-center rounded-lg border border-zinc-700 px-3 text-sm text-zinc-200 hover:bg-zinc-800">
              Open in {producerLabel(item.producer)} <ExternalLink size={14} className="ml-1.5" />
            </a>
          )}
        </div>
      </div>
    </li>
  )
}

export function AttentionPage() {
  const { data: items, loading, error, refresh } = useAttention()
  const { baseUrl } = useServer()
  const setupIncomplete = useSettings().data?.setup_incomplete === true
  const [actionError, setActionError] = useState<string | null>(null)
  const [pending, setPending] = useState<string | null>(null)

  async function handleAction(item: AttentionItem, actionId: string, confirmed: boolean, text?: string) {
    setPending(item.id)
    setActionError(null)
    try {
      await executeAttentionAction(baseUrl, item.id, actionId, item.source_revision, text === undefined ? { confirmed } : { confirmed, text })
      await refresh()
    } catch (cause) {
      setActionError(cause instanceof Error ? cause.message : String(cause))
    } finally {
      setPending(null)
    }
  }

  async function handleDismiss(item: AttentionItem) {
    setPending(item.id)
    setActionError(null)
    try {
      await dismissAttentionItem(baseUrl, item.id, item.source_revision)
      await refresh()
    } catch (cause) {
      setActionError(cause instanceof Error ? cause.message : String(cause))
    } finally {
      setPending(null)
    }
  }

  if (loading) return <LoadingState label="Loading attention…" />

  const allItems = items ?? []

  return (
    <div className="flex flex-1 flex-col overflow-hidden">
      <header className="border-b border-zinc-800 px-4 py-4 sm:px-6">
        <h1 className="flex flex-wrap items-center gap-2 text-xl font-semibold">
          <Bell size={20} className="text-amber-400" />
          Attention dashboard
          {items !== null && <span className="text-sm font-normal text-zinc-500">{allItems.length} item{allItems.length === 1 ? "" : "s"}</span>}
        </h1>
        {actionError && <p className="mt-2 text-sm text-red-400" role="alert">{actionError}</p>}
        {error && <InlineNotice variant="error" className="mt-3">
          <p>Could not load attention: {error}</p>
          {items !== null && <p>Showing the last successful request. Items may have changed.</p>}
          <Button variant="outline" onClick={() => void refresh()}>Retry</Button>
        </InlineNotice>}
      </header>
      {items !== null && <main className="flex-1 overflow-y-auto px-4 py-5 sm:px-6">
        {!error && allItems.length === 0 && !setupIncomplete && (
          <div className="mb-6 flex flex-col items-center gap-3 rounded-xl border border-emerald-900/60 bg-emerald-950/20 px-4 py-8 text-center">
            <CheckCircle size={42} className="text-emerald-500" />
            <p className="text-lg text-zinc-200">Nothing needs your attention right now</p>
          </div>
        )}
        {!error && allItems.length === 0 && setupIncomplete && (
          <div role="status" className="mb-6 flex flex-col items-center gap-2 rounded-xl border border-lw-needs/40 bg-lw-needs-soft px-4 py-8 text-center">
            <p className="text-lg text-lw-text">Setup is not finished</p>
            <p className="max-w-md text-sm text-lw-text-2">Nothing is waiting, but part of Legato cannot work on this machine yet, so this list may be quiet only because of that.</p>
            <SetupChecklistButton baseUrl={baseUrl} label="Open the setup checklist" />
          </div>
        )}
        <div className="space-y-8">
          {sections.map((section) => {
            const sectionItems = sortedItems(allItems.filter(section.matches))
            const Icon = section.icon
            return (
              <section key={section.id} aria-labelledby={`attention-${section.id}`}>
                <div className="mb-3 flex flex-wrap items-start justify-between gap-2">
                  <div>
                    <h2 id={`attention-${section.id}`} className="flex items-center gap-2 font-semibold text-zinc-200">
                      <Icon size={18} className="text-zinc-400" />
                      {section.title}
                      <span className="rounded-full bg-zinc-800 px-2 py-0.5 text-xs font-normal text-zinc-400">{sectionItems.length}</span>
                    </h2>
                    {section.description && <p className="mt-1 text-xs text-zinc-600">{section.description}</p>}
                  </div>
                </div>
                {sectionItems.length === 0 ? (
                  <div className="flex items-center gap-2 rounded-lg border border-dashed border-zinc-800 px-4 py-3 text-sm text-zinc-600">
                    <CheckCircle size={16} className="text-emerald-700" />
                    {section.emptyLabel}
                  </div>
                ) : (
                  <ul className="space-y-3">
                    {sectionItems.map((item) => (
                      <AttentionCard
                        key={item.id}
                        item={item}
                        pending={pending === item.id}
                        onAction={handleAction}
                        onDismiss={handleDismiss}
                      />
                    ))}
                  </ul>
                )}
              </section>
            )
          })}
        </div>
      </main>}
    </div>
  )
}
