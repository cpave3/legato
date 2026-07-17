import { ArrowRight, CheckCircle2, RefreshCw } from "lucide-react"
import { Link } from "react-router-dom"
import { useReviewQueue } from "../hooks/useReview"
import type { ReviewQueueItem } from "../lib/review"

function formatRelativeTime(iso: string): string {
  const date = new Date(iso)
  const now = new Date()
  const diffMs = now.getTime() - date.getTime()
  const diffMins = Math.floor(diffMs / 60000)
  if (diffMins < 1) return "just now"
  if (diffMins < 60) return `${diffMins}m ago`
  const diffHours = Math.floor(diffMins / 60)
  if (diffHours < 24) return `${diffHours}h ago`
  const day = date.getDate().toString().padStart(2, "0")
  const month = (date.getMonth() + 1).toString().padStart(2, "0")
  const year = date.getFullYear()
  return `${day}/${month}/${year}`
}

function formatAbsoluteTime(iso: string): string {
  const date = new Date(iso)
  return date.toLocaleString(undefined, {
    year: "numeric",
    month: "short",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  })
}

function sortQueueItems(items: ReviewQueueItem[]): ReviewQueueItem[] {
  return [...items].sort((a, b) => new Date(b.activity_at).getTime() - new Date(a.activity_at).getTime())
}

export function ReviewQueuePage() {
  const { data, loading, error, refresh } = useReviewQueue()

  return (
    <div className="flex h-full flex-col overflow-hidden bg-[#0a0a0f] text-zinc-200">
      <header className="flex items-center justify-between border-b border-zinc-800 px-6 py-4">
        <div>
          <h1 className="text-lg font-semibold">Review queue</h1>
          <p className="mt-0.5 text-xs text-zinc-500">Walk through agent changes before completing work.</p>
        </div>
        <button onClick={() => void refresh()} className="rounded p-2 text-zinc-500 hover:bg-zinc-900 hover:text-zinc-200" title="Refresh review queue">
          <RefreshCw size={16} />
        </button>
      </header>

      <div className="flex-1 overflow-y-auto p-6">
        {loading && !data && <p className="text-sm text-zinc-500">Loading review queue…</p>}
        {error && <div role="alert" className="rounded border border-red-900 bg-red-950/40 p-3 text-sm text-red-300">{error}</div>}
        {data?.length === 0 && (
          <div className="flex flex-col items-center gap-2 py-20 text-zinc-500">
            <CheckCircle2 size={28} />
            <p className="text-sm">Nothing needs review.</p>
          </div>
        )}
        <div className="mx-auto grid max-w-4xl gap-3">
          {sortQueueItems(data ?? []).map((item) => (
            <Link key={item.tour_id} to={`/review/${encodeURIComponent(item.tour_id)}`} className="group rounded border border-zinc-800 bg-zinc-950 p-4 transition-colors hover:border-indigo-700 hover:bg-zinc-900">
              <div className="flex items-start justify-between gap-4">
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="font-mono text-xs text-zinc-500">{item.task_id}</span>
                    <span className="rounded bg-zinc-800 px-1.5 py-0.5 text-[10px] uppercase text-zinc-400">{item.status}</span>
                  </div>
                  <h2 className="mt-1 font-semibold text-zinc-100">{item.name || item.title}</h2>
                  {item.summary && <p className="mt-2 text-sm text-zinc-400">{item.summary}</p>}
                  <time
                    dateTime={item.activity_at}
                    title={formatAbsoluteTime(item.activity_at)}
                    className="mt-1 block text-xs text-zinc-500"
                  >
                    {formatRelativeTime(item.activity_at)}
                  </time>
                </div>
                <div className="flex shrink-0 items-center gap-3">
                  <span className={item.unreviewed > 0 ? "text-xs font-medium text-amber-300" : "text-xs text-emerald-400"}>
                    {item.unreviewed > 0 ? `${item.unreviewed} unreviewed` : "All steps reviewed"}
                  </span>
                  <ArrowRight size={16} className="text-zinc-600 group-hover:text-indigo-300" />
                </div>
              </div>
            </Link>
          ))}
        </div>
      </div>
    </div>
  )
}
