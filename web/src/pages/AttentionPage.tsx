import { Link } from "react-router-dom"
import { AlertTriangle, Bell, CheckCircle, ChevronRight, GitPullRequest, ScanEye, type LucideIcon } from "lucide-react"
import { useState } from "react"
import { useAttention } from "../hooks/useAttention"
import { useServer } from "../hooks/useServer"
import { executeAttentionAction, dismissAttentionItem, type AttentionItem } from "../lib/attention"
import { legatoHref } from "../lib/navigation"
import { LoadingState } from "../components/ui/LoadingState"

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

type AttentionSection = {
  id: "intervention" | "review" | "merge"
  title: string
  description: string
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
    matches: (item) => item.signal_kind !== "review_ready" && item.signal_kind !== "pr_merge_blocked",
  },
  {
    id: "review",
    title: "Ready for review",
    description: "Agent work explicitly handed over for human review",
    emptyLabel: "No work is ready for review",
    icon: ScanEye,
    matches: (item) => item.signal_kind === "review_ready",
  },
  {
    id: "merge",
    title: "Merge blockers",
    description: "Known actionable blockers from tracked pull requests",
    emptyLabel: "No known merge blockers",
    icon: GitPullRequest,
    matches: (item) => item.signal_kind === "pr_merge_blocked",
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
  onAction: (item: AttentionItem, actionId: string, confirmed: boolean) => void
  onDismiss: (item: AttentionItem) => void
}

function AttentionCard({ item, pending, onAction, onDismiss }: AttentionCardProps) {
  const navPath = navigationPath(item)
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
          {item.detail && <p className="mt-1 break-words text-xs text-zinc-500">{item.detail}</p>}
        </div>
        <div className="flex shrink-0 flex-wrap items-center gap-2 sm:justify-end">
          {(item.actions ?? []).filter((action) => action.kind === "execute").map((action) => (
            <button
              key={action.id}
              type="button"
              disabled={pending}
              onClick={() => onAction(item, action.id, action.requires_confirmation ?? false)}
              className="min-h-11 rounded-lg bg-indigo-500 px-3 text-sm font-medium text-white hover:bg-indigo-400 disabled:opacity-50"
            >
              {action.label}
            </button>
          ))}
          <button
            type="button"
            disabled={pending}
            onClick={() => onDismiss(item)}
            className="min-h-11 rounded-lg px-3 text-sm text-zinc-500 hover:bg-zinc-800 hover:text-zinc-300 disabled:opacity-50"
          >
            Dismiss
          </button>
          {navPath && (item.actions ?? []).filter((action) => action.kind === "navigate").map((action) => (
            <Link key={action.id} to={navPath} className="flex min-h-11 items-center rounded-lg border border-zinc-700 px-3 text-sm text-zinc-200 hover:bg-zinc-800">
              {action.label} <ChevronRight size={16} className="ml-1" />
            </Link>
          ))}
        </div>
      </div>
    </li>
  )
}

export function AttentionPage() {
  const { data: items, loading, error, refresh } = useAttention()
  const { baseUrl } = useServer()
  const [actionError, setActionError] = useState<string | null>(null)
  const [pending, setPending] = useState<string | null>(null)

  async function handleAction(item: AttentionItem, actionId: string, confirmed: boolean) {
    setPending(item.id)
    setActionError(null)
    try {
      await executeAttentionAction(baseUrl, item.id, actionId, item.source_revision, { confirmed })
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
  if (error) return <div className="p-8 text-red-400" role="alert">{error}</div>

  const allItems = items ?? []

  return (
    <div className="flex flex-1 flex-col overflow-hidden">
      <header className="border-b border-zinc-800 px-4 py-4 sm:px-6">
        <h1 className="flex flex-wrap items-center gap-2 text-lg font-semibold">
          <Bell size={20} className="text-amber-400" />
          Attention dashboard
          <span className="text-sm font-normal text-zinc-500">{allItems.length} item{allItems.length === 1 ? "" : "s"}</span>
        </h1>
        <p className="mt-1 text-sm text-zinc-500">Work that needs you across agents, reviews, and pull requests</p>
        {actionError && <p className="mt-2 text-sm text-red-400" role="alert">{actionError}</p>}
      </header>
      <main className="flex-1 overflow-y-auto px-4 py-5 sm:px-6">
        {allItems.length === 0 && (
          <div className="mb-6 flex flex-col items-center gap-3 rounded-xl border border-emerald-900/60 bg-emerald-950/20 px-4 py-8 text-center">
            <CheckCircle size={42} className="text-emerald-500" />
            <p className="text-lg text-zinc-200">Nothing needs your attention right now</p>
            <p className="text-sm text-zinc-500">No interventions, ready reviews, or known merge blockers.</p>
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
                    <p className="mt-1 text-xs text-zinc-600">{section.description}</p>
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
      </main>
    </div>
  )
}
