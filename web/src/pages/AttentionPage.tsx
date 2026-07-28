import { Link } from "react-router-dom"
import { Bell, CheckCircle, ChevronRight } from "lucide-react"
import { useState } from "react"
import { useAttention } from "../hooks/useAttention"
import { useServer } from "../hooks/useServer"
import { executeAttentionAction, dismissAttentionItem, type AttentionItem } from "../lib/attention"

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
  switch (item.navigation.kind) {
    case "plan": return `/plans/${encodeURIComponent(item.navigation.target)}`
    case "review": return `/review/${encodeURIComponent(item.navigation.target)}`
    case "workflow_run": return `/workflows/runs/${encodeURIComponent(item.navigation.target)}`
    case "task": return `/board`
    default: return null
  }
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

  if (loading) return <div className="p-8 text-zinc-500">Loading…</div>
  if (error) return <div className="p-8 text-red-400">{error}</div>

  const sorted = [...(items ?? [])].sort((a, b) => {
    const order = ["critical", "high", "normal", "low"]
    return order.indexOf(a.urgency) - order.indexOf(b.urgency)
  })

  return (
    <div className="flex flex-1 flex-col overflow-hidden">
      <div className="border-b border-zinc-800 px-6 py-4">
        <h1 className="flex items-center gap-2 text-lg font-semibold">
          <Bell size={20} className="text-amber-400" />
          Attention
          {sorted.length > 0 && <span className="text-sm font-normal text-zinc-500">{sorted.length} item{sorted.length === 1 ? "" : "s"}</span>}
        </h1>
        {actionError && <p className="mt-2 text-sm text-red-400">{actionError}</p>}
      </div>
      <div className="flex-1 overflow-y-auto">
        {sorted.length === 0 ? (
          <div className="flex h-full flex-col items-center justify-center gap-3 text-zinc-600">
            <CheckCircle size={48} className="text-emerald-600" />
            <p className="text-lg">Nothing needs your attention right now</p>
          </div>
        ) : (
          <ul className="divide-y divide-zinc-800">
            {sorted.map((item) => {
              const navPath = navigationPath(item)
              const executeAction = item.actions.find((a) => a.kind === "execute")
              return (
                <li key={item.id} className="px-4 py-4 hover:bg-zinc-800/30 sm:px-6">
                  <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between sm:gap-4">
                    <div className="min-w-0 flex-1">
                      <div className="flex flex-wrap items-center gap-2">
                        <h2 className="truncate font-medium">{item.title}</h2>
                        <span className="rounded bg-zinc-800 px-1.5 py-0.5 text-xs text-zinc-400">{item.producer}</span>
                        <span className={`rounded px-1.5 py-0.5 text-xs ${item.urgency === "high" || item.urgency === "critical" ? "bg-red-500/20 text-red-400" : "bg-zinc-800 text-zinc-500"}`}>{item.urgency}</span>
                        <span className="text-xs text-zinc-600">{formatRelativeTime(item.updated_at)}</span>
                      </div>
                      <p className="mt-1 text-sm text-zinc-400">{item.reason}</p>
                      {item.detail && <p className="mt-1 text-xs text-zinc-600">{item.detail}</p>}
                    </div>
                    <div className="flex shrink-0 flex-wrap items-center gap-2">
                      {executeAction && (
                        <button
                          disabled={pending === item.id}
                          onClick={() => handleAction(item, executeAction.id, executeAction.requires_confirmation ?? false)}
                          className="min-h-11 rounded-lg bg-indigo-500 px-3 text-sm font-medium disabled:opacity-50"
                        >
                          {executeAction.label}
                        </button>
                      )}
                      <button
                        disabled={pending === item.id}
                        onClick={() => handleDismiss(item)}
                        className="min-h-11 rounded-lg border border-zinc-700 px-3 text-sm text-zinc-400 hover:bg-zinc-800 disabled:opacity-50"
                      >
                        Dismiss
                      </button>
                      {navPath && (
                        <Link to={navPath} className="flex min-h-11 items-center rounded-lg border border-zinc-700 px-3 text-sm text-zinc-300 hover:bg-zinc-800">
                          Open <ChevronRight size={16} className="ml-1" />
                        </Link>
                      )}
                    </div>
                  </div>
                </li>
              )
            })}
          </ul>
        )}
      </div>
    </div>
  )
}
