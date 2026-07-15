import { useEffect, useMemo, useState } from "react"
import { ArrowLeft, Check, Circle, Loader2, Send } from "lucide-react"
import { Link, useNavigate, useParams } from "react-router-dom"
import { DiffView } from "../components/DiffView"
import { useReviewTour } from "../hooks/useReview"
import { useServer } from "../hooks/useServer"
import {
  askReviewQuestion,
  completeReview,
  fetchStepDiff,
  setStepReviewed,
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
  const { taskId = "" } = useParams()
  const decodedTaskId = decodeURIComponent(taskId)
  const { baseUrl } = useServer()
  const { data, loading, error, refresh } = useReviewTour(decodedTaskId)
  const navigate = useNavigate()
  const [selectedStepId, setSelectedStepId] = useState<string | null>(null)
  const [diff, setDiff] = useState<FileDiff[]>([])
  const [diffLoading, setDiffLoading] = useState(false)
  const [actionError, setActionError] = useState<string | null>(null)
  const [actionInfo, setActionInfo] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [question, setQuestion] = useState("")

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
    if (!selectedStepId) return
    let current = true
    setDiffLoading(true)
    setActionError(null)
    fetchStepDiff(baseUrl, decodedTaskId, selectedStepId)
      .then((files) => { if (current) setDiff(files) })
      .catch((cause) => { if (current) setActionError(cause instanceof Error ? cause.message : String(cause)) })
      .finally(() => { if (current) setDiffLoading(false) })
    return () => { current = false }
  }, [baseUrl, decodedTaskId, selectedStepId])

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
    await runAction(() => setStepReviewed(baseUrl, decodedTaskId, step.id, !step.reviewed_at))
  }

  async function askQuestion() {
    if (!selectedStep || !question.trim()) return
    const text = question.trim()
    await runAction(async () => {
      const warning = await askReviewQuestion(baseUrl, decodedTaskId, selectedStep.id, text)
      setQuestion("")
      setActionInfo(warning ?? "Question sent")
    })
  }

  async function complete() {
    setBusy(true)
    setActionError(null)
    setActionInfo(null)
    try {
      await completeReview(baseUrl, decodedTaskId)
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

  return (
    <div className="flex h-full min-h-0 flex-col bg-[#0a0a0f] text-zinc-200">
      <header className="flex items-center justify-between gap-4 border-b border-zinc-800 px-5 py-3">
        <div className="min-w-0">
          <div className="flex items-center gap-2 text-xs text-zinc-500">
            <Link to="/review" className="flex items-center gap-1 hover:text-zinc-200"><ArrowLeft size={13} /> Queue</Link>
            <span>·</span><span className="font-mono">{decodedTaskId}</span>
          </div>
          <h1 className="mt-1 truncate text-lg font-semibold">{data.tour.summary || decodedTaskId}</h1>
        </div>
        <div className="flex shrink-0 items-center gap-3">
          <span className="text-xs text-zinc-400">{reviewedCount}/{steps.length} reviewed</span>
          <button disabled={busy || steps.length === 0} onClick={() => void complete()} className="rounded bg-indigo-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-indigo-500 disabled:opacity-50">
            Complete review
          </button>
        </div>
      </header>

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
              <section className="rounded border border-zinc-800 bg-zinc-950 p-4">
                <div className="flex items-start justify-between gap-4">
                  <div>
                    <h2 className="font-semibold text-zinc-100">{selectedStep.title}</h2>
                    {selectedStep.commit_sha && <div className="mt-1 font-mono text-[10px] text-zinc-600">{selectedStep.commit_sha.slice(0, 12)}</div>}
                    {selectedStep.orphaned_at && <div className="mt-3 rounded border border-amber-800 bg-amber-950/30 px-3 py-2 text-xs text-amber-200">History rewritten — this step is no longer on the current branch.</div>}
                    {selectedStep.narration && <p className="mt-3 whitespace-pre-wrap text-sm leading-6 text-zinc-400">{selectedStep.narration}</p>}
                  </div>
                  <button disabled={busy} onClick={() => void toggleReviewed(selectedStep)} className={cn("shrink-0 rounded border px-3 py-1.5 text-xs", selectedStep.reviewed_at ? "border-zinc-700 text-zinc-400 hover:text-zinc-200" : "border-emerald-800 bg-emerald-950/30 text-emerald-300 hover:bg-emerald-950/60")}>
                    {selectedStep.reviewed_at ? "Mark unreviewed" : "Mark step reviewed"}
                  </button>
                </div>
              </section>

              {diffLoading ? <div className="flex justify-center py-10 text-zinc-600"><Loader2 className="animate-spin" size={20} /></div> : <DiffView files={diff} />}

            </div>
          )}
        </main>

        <aside className="flex min-h-0 flex-col border-l border-zinc-800 bg-zinc-950 p-3">
          <h3 className="px-1 text-xs font-semibold uppercase tracking-wide text-zinc-500">Questions & answers</h3>
          <div className="mt-3 min-h-0 flex-1 space-y-2 overflow-y-auto">
            {messages.length === 0 && <p className="px-1 text-xs text-zinc-600">No questions on this step yet.</p>}
            {messages.map((message) => (
              <div key={message.id} className={cn("rounded p-2 text-sm", message.author === "user" ? "bg-indigo-950/40 text-indigo-200" : "bg-zinc-900 text-zinc-300")}>
                <div className="mb-1 text-[10px] uppercase text-zinc-600">{message.author}</div>{message.body}
              </div>
            ))}
          </div>
          <div className="mt-3 flex shrink-0 gap-2 border-t border-zinc-800 pt-3">
            <input aria-label="Question for agent" value={question} onChange={(event) => setQuestion(event.target.value)} onKeyDown={(event) => { if (event.key === "Enter") void askQuestion() }} placeholder="Ask about this step…" className="min-w-0 flex-1 rounded border border-zinc-700 bg-zinc-900 px-3 py-2 text-sm outline-none focus:border-indigo-600" />
            <button aria-label="Ask agent" disabled={busy || !question.trim()} onClick={() => void askQuestion()} className="rounded bg-zinc-800 px-3 text-zinc-300 hover:bg-zinc-700 disabled:opacity-40"><Send size={15} /></button>
          </div>
        </aside>
      </div>
    </div>
  )
}
