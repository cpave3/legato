import { useEffect, useState } from "react"
import { ArrowRight, CheckCircle2, RefreshCw, Trash2 } from "lucide-react"
import { Link } from "react-router-dom"
import { useReviewQueue } from "../hooks/useReview"
import { useServer } from "../hooks/useServer"
import { bulkDeleteReviews, type ReviewQueueItem } from "../lib/review"

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

function reviewLabel(item: ReviewQueueItem): string {
  return item.name || item.title
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}

export function ReviewQueuePage() {
  const { baseUrl } = useServer()
  const [showCapturing, setShowCapturing] = useState(false)
  const { data, loading, error, refresh } = useReviewQueue(showCapturing)
  const items = data ?? []
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [confirming, setConfirming] = useState(false)
  const [removing, setRemoving] = useState(false)
  const [removeError, setRemoveError] = useState<string | null>(null)
  const selectedItems = items.filter((item) => selected.has(item.tour_id))
  const allSelected = items.length > 0 && selectedItems.length === items.length

  useEffect(() => {
    const visible = new Set(items.map((item) => item.tour_id))
    setSelected((current) => {
      const next = new Set([...current].filter((id) => visible.has(id)))
      return next.size === current.size ? current : next
    })
  }, [items])

  function toggleReview(tourId: string) {
    setRemoveError(null)
    setSelected((current) => {
      const next = new Set(current)
      if (next.has(tourId)) next.delete(tourId)
      else next.add(tourId)
      return next
    })
  }

  function toggleAll() {
    setRemoveError(null)
    setSelected(allSelected ? new Set() : new Set(items.map((item) => item.tour_id)))
  }

  async function removeSelected() {
    setRemoving(true)
    setRemoveError(null)
    try {
      await bulkDeleteReviews(baseUrl, selectedItems.map((item) => item.tour_id))
      setSelected(new Set())
      setConfirming(false)
      await refresh()
    } catch (cause) {
      setRemoveError(errorMessage(cause))
      setConfirming(false)
    } finally {
      setRemoving(false)
    }
  }

  const selectedCount = selectedItems.length
  const reviewNoun = selectedCount === 1 ? "review" : "reviews"

  return (
    <div className="flex h-full flex-col overflow-hidden bg-[#0a0a0f] text-zinc-200">
      <header className="flex items-center justify-between border-b border-zinc-800 px-4 py-3 md:px-6 md:py-4">
        <div>
          <h1 className="text-lg font-semibold">Review queue</h1>
          <p className="mt-0.5 text-xs text-zinc-500">Walk through agent changes before completing work.</p>
        </div>
        <div className="flex items-center gap-3">
          <label className="flex min-h-10 cursor-pointer items-center gap-2 text-xs text-zinc-400">
            <input type="checkbox" checked={showCapturing} onChange={(event) => {
              setShowCapturing(event.target.checked)
              setSelected(new Set())
              setConfirming(false)
            }} className="size-4 accent-indigo-500" />
            Show incomplete captures
          </label>
          <button onClick={() => void refresh()} className="rounded p-2 text-zinc-500 hover:bg-zinc-900 hover:text-zinc-200" title="Refresh review queue">
            <RefreshCw size={16} />
          </button>
        </div>
      </header>

      <div className="flex-1 overflow-y-auto p-3 md:p-6">
        {loading && !data && <p className="text-sm text-zinc-500">Loading review queue…</p>}
        {error && <div role="alert" className="rounded border border-red-900 bg-red-950/40 p-3 text-sm text-red-300">{error}</div>}
        {removeError && <div role="alert" className="mx-auto mb-3 max-w-4xl rounded border border-red-900 bg-red-950/40 p-3 text-sm text-red-300">{removeError}</div>}
        {data?.length === 0 && (
          <div className="flex flex-col items-center gap-2 py-20 text-zinc-500">
            <CheckCircle2 size={28} />
            <p className="text-sm">Nothing needs review.</p>
          </div>
        )}
        {items.length > 0 && (
          <div className="mx-auto mb-3 flex max-w-4xl flex-wrap items-center justify-between gap-3 rounded border border-zinc-800 bg-zinc-950 px-3 py-2">
            <label className="flex min-h-10 cursor-pointer items-center gap-3 text-sm text-zinc-300">
              <input type="checkbox" aria-label="Select all reviews" checked={allSelected} onChange={toggleAll} className="size-4 accent-indigo-500" />
              <span>{selectedCount > 0 ? `${selectedCount} selected` : "Select all"}</span>
            </label>
            <button
              type="button"
              disabled={selectedCount === 0 || removing}
              onClick={() => setConfirming(true)}
              className="flex min-h-10 items-center gap-2 rounded bg-red-600 px-3 text-sm font-medium text-white disabled:cursor-not-allowed disabled:opacity-40"
            >
              <Trash2 size={15} /> Remove selected
            </button>
          </div>
        )}
        <div className="mx-auto grid max-w-4xl gap-3">
          {items.map((item) => (
            <article key={item.tour_id} className={`flex items-start gap-3 rounded border bg-zinc-950 p-3 transition-colors ${selected.has(item.tour_id) ? "border-indigo-600" : "border-zinc-800"}`}>
              <input
                type="checkbox"
                aria-label={`Select ${reviewLabel(item)}`}
                checked={selected.has(item.tour_id)}
                onChange={() => toggleReview(item.tour_id)}
                className="mt-1 size-4 shrink-0 accent-indigo-500"
              />
              <Link to={`/review/${encodeURIComponent(item.tour_id)}`} className="group min-w-0 flex-1 rounded p-1 transition-colors hover:bg-zinc-900">
                <div className="flex flex-col items-start justify-between gap-3 sm:flex-row sm:gap-4">
                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="font-mono text-xs text-zinc-500">{item.task_id}</span>
                      <span className={`rounded px-1.5 py-0.5 text-[10px] uppercase ${item.status === "capturing" ? "bg-violet-950 text-violet-300" : "bg-zinc-800 text-zinc-400"}`}>
                        {item.status === "capturing" ? "Incomplete — not marked ready" : item.status}
                      </span>
                    </div>
                    <h2 className="mt-1 font-semibold text-zinc-100">{reviewLabel(item)}</h2>
                    {item.summary && <p className="mt-2 text-sm text-zinc-400">{item.summary}</p>}
                    <time dateTime={item.activity_at} title={formatAbsoluteTime(item.activity_at)} className="mt-1 block text-xs text-zinc-500">
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
            </article>
          ))}
        </div>
      </div>

      {confirming && (
        <div className="fixed inset-0 z-50 flex items-end bg-black/70 p-2 sm:items-center sm:justify-center">
          <div role="dialog" aria-modal="true" aria-labelledby="bulk-remove-title" className="w-full rounded-xl border border-zinc-700 bg-zinc-900 p-5 shadow-xl sm:max-w-md">
            <h2 id="bulk-remove-title" className="text-lg font-semibold">Remove {selectedCount} {reviewNoun}?</h2>
            <p className="mt-3 text-sm text-zinc-300">The selected review packets and their comments will be permanently removed. Tasks and code will remain.</p>
            <div className="mt-5 flex gap-2">
              <button type="button" onClick={() => setConfirming(false)} disabled={removing} className="min-h-11 flex-1 rounded border border-zinc-700 text-sm">Cancel</button>
              <button type="button" onClick={() => void removeSelected()} disabled={removing} className="min-h-11 flex-1 rounded bg-red-600 text-sm font-medium text-white disabled:opacity-60">
                {removing ? "Removing…" : `Remove ${selectedCount} ${reviewNoun}`}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
