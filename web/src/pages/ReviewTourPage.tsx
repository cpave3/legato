import { useEffect, useMemo, useState } from "react"
import { ArrowLeft, Check, Circle, Loader2, Send } from "lucide-react"
import { Link, useNavigate, useParams } from "react-router-dom"
import { DiffView } from "../components/DiffView"
import { useReviewTour } from "../hooks/useReview"
import { useServer } from "../hooks/useServer"
import {
  askReviewQuestion,
  completeReview,
  deleteReview,
  fetchStepDiff,
  setStepReviewed,
  type DiffSelection,
  type FileDiff,
  type ReviewStep,
} from "../lib/review"
import { cn } from "../lib/utils"

const riskColor: Record<string, string> = {
  low: "text-emerald-400",
  medium: "text-amber-300",
  high: "text-red-400",
  unsure: "text-violet-300",
}

export function ReviewTourPage() {
  const { tourId = "" } = useParams()
  const decodedTourId = decodeURIComponent(tourId)
  const { baseUrl } = useServer()
  const { data, loading, error, refresh } = useReviewTour(decodedTourId)
  const navigate = useNavigate()
  const [selectedStepId, setSelectedStepId] = useState<string | null>(null)
  const [diff, setDiff] = useState<FileDiff[]>([])
  const [diffLoading, setDiffLoading] = useState(false)
  const [actionError, setActionError] = useState<string | null>(null)
  const [actionInfo, setActionInfo] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [question, setQuestion] = useState("")
  const [selection, setSelection] = useState<DiffSelection | null>(null)
  const [confirmingDelete, setConfirmingDelete] = useState(false)

  const steps = useMemo(() => data?.steps ?? [], [data?.steps])
  const selectedStep = steps.find((step) => step.id === selectedStepId) ?? steps[0]
  const reviewedCount = steps.filter((step) => step.reviewed_at).length

  useEffect(() => {
    if (!selectedStepId && steps[0]) setSelectedStepId(steps[0].id)
    if (selectedStepId && steps.length > 0 && !steps.some((step) => step.id === selectedStepId)) {
      setSelectedStepId(steps[0].id)
    }
  }, [selectedStepId, steps])

  useEffect(() => {
    setSelection(null)
    if (!selectedStepId) return
    let current = true
    setDiffLoading(true)
    setActionError(null)
    fetchStepDiff(baseUrl, decodedTourId, selectedStepId)
      .then((files) => { if (current) setDiff(files) })
      .catch((cause) => { if (current) setActionError(cause instanceof Error ? cause.message : String(cause)) })
      .finally(() => { if (current) setDiffLoading(false) })
    return () => { current = false }
  }, [baseUrl, decodedTourId, selectedStepId])

  async function runAction(action: () => Promise<void>) {
    setBusy(true)
    setActionError(null)
    setActionInfo(null)
    try {
      await action()
      await refresh()
    } catch (cause) {
      setActionError(cause instanceof Error ? cause.message : String(cause))
    } finally {
      setBusy(false)
    }
  }

  async function toggleReviewed(step: ReviewStep) {
    await runAction(() => setStepReviewed(baseUrl, decodedTourId, step.id, !step.reviewed_at))
  }

  async function askQuestion() {
    if (!selectedStep || !question.trim()) return
    const text = question.trim()
    await runAction(async () => {
      const warning = await askReviewQuestion(baseUrl, decodedTourId, selectedStep.id, text, selection)
      setQuestion("")
      setSelection(null)
      setActionInfo(warning ?? "Question sent")
    })
  }

  async function removeReview() {
    setBusy(true)
    setActionError(null)
    try {
      await deleteReview(baseUrl, decodedTourId)
      navigate("/review")
    } catch (cause) {
      setActionError(cause instanceof Error ? cause.message : String(cause))
      setConfirmingDelete(false)
      setBusy(false)
    }
  }

  async function complete() {
    setBusy(true)
    setActionError(null)
    setActionInfo(null)
    try {
      await completeReview(baseUrl, decodedTourId)
      navigate("/review")
    } catch (cause) {
      setActionError(cause instanceof Error ? cause.message : String(cause))
      setBusy(false)
    }
  }

  if (loading && !data) return <div className="p-6 text-sm text-zinc-500">Loading review…</div>
  if (error && !data) return <div role="alert" className="m-6 rounded border border-red-900 bg-red-950/40 p-3 text-red-300">{error}</div>
  if (!data) return null

  const messages = selectedStep ? data.messages.filter((message) => message.step_id === selectedStep.id) : []
  const hunkNotes = selectedStep ? data.hunk_notes.filter((note) => note.step_id === selectedStep.id) : []
  const matchedNoteIDs = new Set(diff.flatMap((file) => file.hunks.flatMap((hunk) =>
    hunkNotes
      .filter((note) => note.hunk_anchor === hunk.anchor && (note.file_path === file.new_path || note.file_path === file.old_path))
      .map((note) => note.id),
  )))
  const unmatchedHunkNotes = hunkNotes.filter((note) => !matchedNoteIDs.has(note.id))

  return (
    <div className="flex h-full min-h-0 flex-col bg-[#0a0a0f] text-zinc-200">
      <header className="flex items-center justify-between gap-4 border-b border-zinc-800 px-5 py-3">
        <div className="min-w-0">
          <div className="flex items-center gap-2 text-xs text-zinc-500">
            <Link to="/review" className="flex items-center gap-1 hover:text-zinc-200"><ArrowLeft size={13} /> Queue</Link>
            <span>·</span><span className="font-mono">{decodedTourId}</span>
          </div>
          <h1 className="mt-1 truncate text-lg font-semibold">{data.tour.name || data.tour.summary || decodedTourId}</h1>
        </div>
        <div className="flex shrink-0 items-center gap-3">
          <span className="text-xs text-zinc-400">{reviewedCount}/{steps.length} reviewed</span>
          <button disabled={busy} onClick={() => setConfirmingDelete(true)} className="rounded border border-red-900 px-3 py-1.5 text-xs font-medium text-red-300 hover:bg-red-950/50 disabled:opacity-50">
            Delete review
          </button>
          <button disabled={busy || steps.length === 0} onClick={() => void complete()} className="rounded bg-indigo-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-indigo-500 disabled:opacity-50">
            Complete review
          </button>
        </div>
      </header>

      {confirmingDelete && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
          <div role="dialog" aria-modal="true" aria-labelledby="delete-review-title" className="w-full max-w-sm rounded-lg border border-zinc-700 bg-zinc-900 shadow-2xl">
            <div className="border-b border-zinc-800 px-5 py-3">
              <h2 id="delete-review-title" className="text-sm font-semibold text-zinc-200">Delete review</h2>
            </div>
            <div className="px-5 py-4 text-sm text-zinc-400">Delete the review and all of its steps, questions, and notes?</div>
            <div className="flex justify-end gap-2 border-t border-zinc-800 px-5 py-3">
              <button disabled={busy} onClick={() => setConfirmingDelete(false)} className="rounded border border-zinc-700 px-3 py-1.5 text-xs text-zinc-400 hover:bg-zinc-800 disabled:opacity-50">Cancel</button>
              <button disabled={busy} onClick={() => void removeReview()} className="rounded bg-red-600 px-3 py-1.5 text-xs text-white hover:bg-red-500 disabled:opacity-50">Delete review</button>
            </div>
          </div>
        </div>
      )}

      {actionError && <div role="alert" className="border-b border-red-900 bg-red-950/50 px-5 py-2 text-xs text-red-300">{actionError}</div>}
      {actionInfo && <div role="status" className="border-b border-amber-900 bg-amber-950/40 px-5 py-2 text-xs text-amber-200">{actionInfo}</div>}

      <div className="grid min-h-0 flex-1 grid-cols-1 lg:grid-cols-[18rem_minmax(0,1fr)_20rem]">
        <aside className="overflow-y-auto border-r border-zinc-800 bg-zinc-950 p-3">
          <div className="mb-2 px-2 text-[10px] font-semibold uppercase tracking-wider text-zinc-600">Review steps</div>
          <div className="space-y-1">
            {steps.map((step, index) => (
              <button key={step.id} aria-label={`Step ${index + 1}: ${step.title}`} onClick={() => setSelectedStepId(step.id)} className={cn("w-full rounded p-2 text-left", selectedStep?.id === step.id ? "bg-zinc-800" : "hover:bg-zinc-900")}>
                <div className="flex gap-2">
                  {step.reviewed_at ? <Check size={14} className="mt-0.5 shrink-0 text-emerald-400" /> : <Circle size={14} className="mt-0.5 shrink-0 text-amber-300" />}
                  <div className="min-w-0">
                    <div className="truncate text-xs font-medium text-zinc-200">{step.title}</div>
                    <div className="mt-1 flex items-center gap-2 text-[10px] uppercase text-zinc-600">
                      <span>{step.kind}</span>
                      {step.risk && <span className={riskColor[step.risk]}>Risk: {step.risk}</span>}
                    </div>
                  </div>
                </div>
              </button>
            ))}
          </div>
        </aside>

        <main className="min-w-0 overflow-y-auto p-5">
          {selectedStep && (
            <div className="mx-auto max-w-6xl space-y-4">
              <div data-sticky-review-action className="sticky top-0 z-20 flex justify-end border-b border-zinc-800/80 bg-[#0a0a0f]/95 py-2 backdrop-blur">
                <button disabled={busy} onClick={() => void toggleReviewed(selectedStep)} className={cn("rounded border px-3 py-1.5 text-xs shadow-lg", selectedStep.reviewed_at ? "border-zinc-700 bg-zinc-900 text-zinc-400 hover:text-zinc-200" : "border-emerald-800 bg-emerald-950 text-emerald-300 hover:bg-emerald-900")}>
                  {selectedStep.reviewed_at ? "Mark unreviewed" : "Mark step reviewed"}
                </button>
              </div>
              <section className="rounded border border-zinc-800 bg-zinc-950 p-4">
                <div className="flex items-start justify-between gap-4">
                  <div>
                    <h2 className="font-semibold text-zinc-100">{selectedStep.title}</h2>
                    {selectedStep.commit_sha && <div className="mt-1 font-mono text-[10px] text-zinc-600">{selectedStep.commit_sha.slice(0, 12)}</div>}
                    {selectedStep.orphaned_at && <div className="mt-3 rounded border border-amber-800 bg-amber-950/30 px-3 py-2 text-xs text-amber-200">History rewritten — this step is no longer on the current branch.</div>}
                    {selectedStep.narration && <p className="mt-3 whitespace-pre-wrap text-sm leading-6 text-zinc-400">{selectedStep.narration}</p>}
                  </div>
                </div>
              </section>

              {diffLoading ? <div className="flex justify-center py-10 text-zinc-600"><Loader2 className="animate-spin" size={20} /></div> : <DiffView key={selectedStep.id} files={diff} hunkNotes={hunkNotes} selection={selection} onSelectionChange={setSelection} />}

              {!diffLoading && unmatchedHunkNotes.length > 0 && (
                <section role="alert" aria-label="Unmatched hunk notes" className="rounded border border-amber-800 bg-amber-950/30 p-4 text-amber-100">
                  <h3 className="text-sm font-semibold">Unmatched hunk notes</h3>
                  <p className="mt-1 text-xs text-amber-300">These notes no longer match a hunk in the selected step.</p>
                  <div className="mt-3 space-y-2">
                    {unmatchedHunkNotes.map((note) => (
                      <div key={note.id} className="rounded bg-amber-950/50 px-3 py-2 text-sm">
                        <div className="mb-1 font-mono text-[10px] text-amber-400">{note.file_path}</div>
                        {note.body}
                      </div>
                    ))}
                  </div>
                </section>
              )}
            </div>
          )}
        </main>

        <aside className="flex min-h-0 flex-col border-l border-zinc-800 bg-zinc-950 p-3">
          <h3 className="px-1 text-xs font-semibold uppercase tracking-wide text-zinc-500">Questions & answers</h3>
          <div className="mt-3 min-h-0 flex-1 space-y-2 overflow-y-auto">
            {messages.length === 0 && <p className="px-1 text-xs text-zinc-600">No questions on this step yet.</p>}
            {messages.map((message) => (
              <div key={message.id} className={cn("rounded p-2 text-sm", message.author === "user" ? "bg-indigo-950/40 text-indigo-200" : "bg-zinc-900 text-zinc-300")}>
                <div className="mb-1 text-[10px] uppercase text-zinc-600">{message.author}</div><div className="whitespace-pre-wrap">{message.body}</div>
              </div>
            ))}
          </div>
          {selection && (
            <div className="mt-3 flex items-center justify-between gap-2 rounded border border-indigo-800 bg-indigo-950/30 px-2 py-1.5 text-xs text-indigo-200">
              <span className="truncate">{selection.file_path} · {selection.end - selection.start + 1} selected {selection.end === selection.start ? "line" : "lines"}</span>
              <button type="button" onClick={() => setSelection(null)} className="shrink-0 text-indigo-400 hover:text-indigo-200">Clear</button>
            </div>
          )}
          <div className="mt-3 flex shrink-0 gap-2 border-t border-zinc-800 pt-3">
            <input aria-label="Question for agent" value={question} onChange={(event) => setQuestion(event.target.value)} onKeyDown={(event) => { if (event.key === "Enter") void askQuestion() }} placeholder={selection ? "Ask about selected lines…" : "Ask about this step…"} className="min-w-0 flex-1 rounded border border-zinc-700 bg-zinc-900 px-3 py-2 text-sm outline-none focus:border-indigo-600" />
            <button aria-label="Ask agent" disabled={busy || !question.trim()} onClick={() => void askQuestion()} className="rounded bg-zinc-800 px-3 text-zinc-300 hover:bg-zinc-700 disabled:opacity-40"><Send size={15} /></button>
          </div>
        </aside>
      </div>
    </div>
  )
}
