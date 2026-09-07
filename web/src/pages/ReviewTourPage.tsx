import { useEffect, useMemo, useState, type CSSProperties, type PointerEvent as ReactPointerEvent } from "react"
import { ArrowLeft, Check, Circle, Flag, List, Loader2, MessageSquare, RefreshCw, Send, X } from "lucide-react"
import { Link, useNavigate, useParams } from "react-router-dom"
import { HandoffView } from "../components/handoff/HandoffView"
import { DiffView } from "../components/DiffView"
import { MarkdownBody } from "../components/MarkdownBody"
import { ReviewMessageBody } from "../components/ReviewMessageBody"
import { GoToAgentButton } from "../components/GoToAgentButton"
import { VoiceRecorder } from "../components/VoiceRecorder"
import { Button } from "../components/ui/Button"
import { Dialog, DialogBody, DialogClose, DialogFooter, DialogHeader, DialogPanel, DialogTitle } from "../components/ui/Dialog"
import { InlineNotice } from "../components/ui/InlineNotice"
import { useReviewTour } from "../hooks/useReview"
import { useServer } from "../hooks/useServer"
import { useVoiceEnabled } from "../hooks/useVoiceEnabled"
import {
  askReviewQuestion,
  completeReview,
  createReviewFinding,
  deleteReview,
  fetchStepDiff,
  regenerateReview,
  requestFollowUpPlan,
  setStepReviewed,
  syncReviewTour,
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
  const [view, setView] = useState("diff")
  const [handoffVisited, setHandoffVisited] = useState(false)
  const [selectedStepId, setSelectedStepId] = useState<string | null>(null)
  const [diff, setDiff] = useState<FileDiff[]>([])
  const [diffLoading, setDiffLoading] = useState(false)
  const [actionError, setActionError] = useState<string | null>(null)
  const [actionNotice, setActionNotice] = useState<{ message: string; variant: "success" | "warning" } | null>(null)
  const [busy, setBusy] = useState(false)
  const [question, setQuestion] = useState("")
  const [selection, setSelection] = useState<DiffSelection | null>(null)
  const [qaWidthRem, setQAWidthRem] = useState(28)
  const [confirmingDelete, setConfirmingDelete] = useState(false)
  const [regenerating, setRegenerating] = useState(false)
  const [regenerationFeedback, setRegenerationFeedback] = useState("")
  const [findingDraft, setFindingDraft] = useState("")
  const [selectedFindingIDs, setSelectedFindingIDs] = useState<string[]>([])
  const [stepsOpen, setStepsOpen] = useState(false)
  const [qaOpen, setQAOpen] = useState(false)
  const voiceEnabled = useVoiceEnabled(baseUrl)

  const passViews = useMemo(() => data?.passes ?? [], [data?.passes])
  const activePass = passViews[passViews.length - 1]
  const steps = useMemo(() => activePass?.steps ?? data?.steps ?? [], [activePass?.steps, data?.steps])
  const selectedStep = steps.find((step) => step.id === selectedStepId) ?? steps[0]
  const reviewedCount = steps.filter((step) => step.reviewed_at && step.kind !== "commit").length
  const reviewableCount = steps.filter((step) => step.kind !== "commit").length

  const firstChapterStep = steps.find((step) => step.kind !== "commit")
  useEffect(() => {
    if (!selectedStepId && firstChapterStep) setSelectedStepId(firstChapterStep.id)
    if (selectedStepId && steps.length > 0 && !steps.some((step) => step.id === selectedStepId)) {
      setSelectedStepId(firstChapterStep?.id ?? steps[0].id)
    }
  }, [selectedStepId, steps, firstChapterStep])

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
    setActionNotice(null)
    try {
      await action()
      await refresh()
    } catch (cause) {
      setActionError(cause instanceof Error ? cause.message : String(cause))
    } finally {
      setBusy(false)
    }
  }

  async function syncChanges() {
    await runAction(() => syncReviewTour(baseUrl, decodedTourId))
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
      setActionNotice(warning
        ? { message: warning, variant: "warning" }
        : { message: "Question sent", variant: "success" })
    })
  }

  async function flagChange() {
    if (!selectedStep || !findingDraft.trim()) return
    await runAction(async () => {
      const finding = await createReviewFinding(baseUrl, decodedTourId, findingDraft.trim(), selectedStep.id, selection)
      setFindingDraft("")
      setSelection(null)
      setSelectedFindingIDs((current) => [...current, finding.id])
      setActionNotice({ message: "Change flagged", variant: "success" })
    })
  }

  async function requestPlan() {
    if (selectedFindingIDs.length === 0) return
    await runAction(async () => {
      const warning = await requestFollowUpPlan(baseUrl, decodedTourId, selectedFindingIDs)
      setSelectedFindingIDs([])
      setActionNotice(warning
        ? { message: warning, variant: "warning" }
        : { message: "Follow-up plan requested", variant: "success" })
    })
  }

  async function regenerate() {
    if (!regenerationFeedback.trim()) return
    await runAction(async () => {
      await regenerateReview(baseUrl, decodedTourId, regenerationFeedback.trim())
      setRegenerationFeedback("")
      setRegenerating(false)
      setSelectedStepId(null)
      setActionNotice({ message: "Review regeneration requested", variant: "success" })
    })
  }

  function resizeQAFromPointer(event: ReactPointerEvent<HTMLDivElement>) {
    event.preventDefault()
    const startX = event.clientX
    const startWidth = qaWidthRem
    const rootSize = parseFloat(getComputedStyle(document.documentElement).fontSize) || 16
    const move = (moveEvent: PointerEvent) => {
      setQAWidthRem(Math.min(40, Math.max(22, startWidth + (startX - moveEvent.clientX) / rootSize)))
    }
    const stop = () => {
      window.removeEventListener("pointermove", move)
      window.removeEventListener("pointerup", stop)
    }
    window.addEventListener("pointermove", move)
    window.addEventListener("pointerup", stop)
  }

  function resizeQAFromKeyboard(event: React.KeyboardEvent<HTMLDivElement>) {
    if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return
    event.preventDefault()
    setQAWidthRem((width) => Math.min(40, Math.max(22, width + (event.key === "ArrowRight" ? 2 : -2))))
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
    setActionNotice(null)
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

  const activeMessages = activePass?.messages ?? data.messages
  const activeHunkNotes = activePass?.hunk_notes ?? data.hunk_notes
  const findings = activePass?.findings ?? []
  const messages = selectedStep ? activeMessages.filter((message) => message.step_id === selectedStep.id) : []
  const hunkNotes = selectedStep ? activeHunkNotes.filter((note) => note.step_id === selectedStep.id) : []
  const matchedNoteIDs = new Set(diff.flatMap((file) => file.hunks.flatMap((hunk) =>
    hunkNotes
      .filter((note) => note.hunk_anchor === hunk.anchor && (note.file_path === file.new_path || note.file_path === file.old_path))
      .map((note) => note.id),
  )))
  const unmatchedHunkNotes = hunkNotes.filter((note) => !matchedNoteIDs.has(note.id))

  return (
    <div className="flex h-full min-h-0 flex-col bg-[#0a0a0f] text-zinc-200">
      <header className="flex flex-wrap items-center justify-between gap-2 border-b border-zinc-800 px-3 py-2 md:px-5 md:py-3">
        <div className="min-w-0">
          <div className="flex items-center gap-2 text-xs text-zinc-500">
            <Link to="/review" className="flex items-center gap-1 hover:text-zinc-200"><ArrowLeft size={13} /> Queue</Link>
            <span>·</span><span className="font-mono">{decodedTourId}</span>
          </div>
          <h1 className="mt-1 truncate text-lg font-semibold">{data.tour.name || data.tour.summary || decodedTourId}</h1>
        </div>
        <div data-review-actions className="flex w-full flex-wrap items-center gap-2 md:w-auto md:flex-nowrap md:gap-3">
          <GoToAgentButton taskId={data.tour.task_id} />
          <Button variant="outline" size="sm" disabled={busy} onClick={() => void syncChanges()} title="Reconcile this review with its Git repository">
            <RefreshCw size={13} /> Sync changes
          </Button>
          <Button variant="outline" size="sm" onClick={() => setStepsOpen(true)} className="min-h-11 lg:hidden"><List size={15}/> Steps</Button>
          <Button variant="outline" size="sm" onClick={() => setQAOpen(true)} className="min-h-11 lg:hidden"><MessageSquare size={15}/> Notes</Button>
          <span className="text-xs text-zinc-400">{reviewedCount}/{reviewableCount} reviewed</span>
          <Button variant="warning" size="sm" disabled={busy} onClick={() => setRegenerating(true)} className="hidden sm:inline-flex">
            <RefreshCw size={13} /> Regenerate review
          </Button>
          <Button variant="destructive" size="sm" disabled={busy} onClick={() => setConfirmingDelete(true)} className="hidden md:inline-flex">
            Delete review
          </Button>
          <Button size="sm" disabled={busy || reviewableCount === 0} onClick={() => void complete()} className="min-h-11 md:min-h-9">
            Complete review
          </Button>
        </div>
      </header>

      <nav aria-label="Review view" className="flex gap-2 border-b border-zinc-800 p-2">
        <Button size="sm" variant="outline" aria-pressed={view === "diff"} onClick={() => setView("diff")}>Diff review</Button>
        <Button size="sm" variant="outline" aria-pressed={view === "handoff"} onClick={() => { setHandoffVisited(true); setView("handoff") }}>Handoff</Button>
      </nav>
      <Dialog open={regenerating} onClose={() => setRegenerating(false)} dismissible={!busy}>
        <DialogPanel className="max-w-lg">
          <DialogHeader><DialogTitle>Regenerate review</DialogTitle><DialogClose disabled={busy} /></DialogHeader>
          <DialogBody className="space-y-3">
            <p className="text-xs text-zinc-400">Replace the current pass and tell the agent how to regenerate it.</p>
            <textarea aria-label="Regeneration feedback" rows={5} value={regenerationFeedback} onChange={(event) => setRegenerationFeedback(event.target.value)} className="w-full rounded border border-zinc-700 bg-zinc-950 p-3 text-sm outline-none focus:border-amber-600" placeholder="Focus on API compatibility, reduce chapter size…" />
          </DialogBody>
          <DialogFooter>
            <Button variant="outline" size="sm" disabled={busy} onClick={() => setRegenerating(false)}>Cancel</Button>
            <Button variant="warning" size="sm" disabled={busy || !regenerationFeedback.trim()} onClick={() => void regenerate()}>Regenerate</Button>
          </DialogFooter>
        </DialogPanel>
      </Dialog>

      <Dialog open={confirmingDelete} onClose={() => setConfirmingDelete(false)} dismissible={!busy}>
        <DialogPanel className="max-w-sm">
          <DialogHeader><DialogTitle>Delete review</DialogTitle><DialogClose disabled={busy} /></DialogHeader>
          <DialogBody className="text-sm text-zinc-400">Delete the review and all of its steps, questions, and notes?</DialogBody>
          <DialogFooter>
            <Button variant="outline" size="sm" disabled={busy} onClick={() => setConfirmingDelete(false)}>Cancel</Button>
            <Button variant="destructive" size="sm" disabled={busy} onClick={() => void removeReview()}>Delete review</Button>
          </DialogFooter>
        </DialogPanel>
      </Dialog>

      {actionError && <InlineNotice variant="error" className="rounded-none border-x-0 border-t-0 px-5 py-2 text-xs">{actionError}</InlineNotice>}
      {actionNotice && <InlineNotice variant={actionNotice.variant} className="rounded-none border-x-0 border-t-0 px-5 py-2 text-xs">{actionNotice.message}</InlineNotice>}

      {handoffVisited && <div hidden={view !== "handoff"} className={view === "handoff" ? "flex min-h-0 flex-1 flex-col" : "hidden"}><HandoffView active={view === "handoff"} key={`${baseUrl}:${decodedTourId}`} tourId={decodedTourId} review={data} onChapter={(id) => { setSelectedStepId(id); setView("diff") }} /></div>}
      {view === "diff" && <div
        data-review-layout
        style={{ "--qa-width": `${qaWidthRem}rem` } as CSSProperties}
        className="grid min-h-0 flex-1 grid-cols-1 lg:grid-cols-[18rem_minmax(0,1fr)_var(--qa-width)]"
      >
        <aside className={`${stepsOpen ? "fixed inset-2 z-50 block rounded-lg border shadow-2xl" : "hidden"} overflow-y-auto border-zinc-800 bg-zinc-950 p-3 lg:static lg:block lg:rounded-none lg:border-y-0 lg:border-l-0 lg:border-r`}>
          <button aria-label="Close chapters" onClick={() => setStepsOpen(false)} className="absolute right-2 top-2 flex h-11 w-11 items-center justify-center lg:hidden"><X size={18}/></button>
          {passViews.length > 0 && <div className="mb-3 space-y-3">
            {passViews.map((passView) => <section key={passView.pass.id} aria-label={`Pass ${passView.pass.number}`}>
              <div className="px-2 text-[10px] font-semibold uppercase tracking-wider text-zinc-500">Pass {passView.pass.number} · {passView.pass.status}</div>
              {passView.pass.guidance && <p className="px-2 py-1 text-[10px] text-amber-300">{passView.pass.guidance}</p>}
              {passView.pass.id !== activePass?.pass.id && <div className="space-y-1 opacity-60">{passView.steps.map((step) => <div key={step.id} className="truncate rounded px-2 py-1 text-xs text-zinc-400">{step.title}</div>)}</div>}
            </section>)}
          </div>}
          <div className="mb-2 px-2 text-[10px] font-semibold uppercase tracking-wider text-zinc-600">Current pass</div>
          <div className="space-y-1">
            {steps.filter((step) => step.kind !== "commit").map((step, index) => (
              <button key={step.id} aria-label={`Step ${index + 1}: ${step.title}`} onClick={() => { setSelectedStepId(step.id); setStepsOpen(false) }} className={cn("min-h-11 w-full rounded p-2 text-left", selectedStep?.id === step.id ? "bg-zinc-800" : "hover:bg-zinc-900")}>
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
          {steps.some((step) => step.kind === "commit") && (
            <details className="mt-3">
              <summary className="cursor-pointer px-2 py-1 text-[10px] font-semibold uppercase tracking-wider text-zinc-600 hover:text-zinc-400">Commits ({steps.filter((s) => s.kind === "commit").length})</summary>
              <div className="mt-1 space-y-1">
                {steps.filter((step) => step.kind === "commit").map((step, index) => (
                  <button key={step.id} aria-label={`Commit ${index + 1}: ${step.title}`} onClick={() => { setSelectedStepId(step.id); setStepsOpen(false) }} className={cn("min-h-11 w-full rounded p-2 text-left", selectedStep?.id === step.id ? "bg-zinc-800" : "hover:bg-zinc-900")}>
                    <div className="flex gap-2">
                      {step.reviewed_at ? <Check size={14} className="mt-0.5 shrink-0 text-emerald-400" /> : <Circle size={14} className="mt-0.5 shrink-0 text-amber-300" />}
                      <div className="min-w-0">
                        <div className="truncate text-xs font-medium text-zinc-200">{step.title}</div>
                        <div className="mt-1 flex items-center gap-2 text-[10px] uppercase text-zinc-600">
                          {step.commit_sha && <span className="font-mono">{step.commit_sha.slice(0, 8)}</span>}
                          {step.orphaned_at && <span className="text-amber-500">rewritten</span>}
                          {step.risk && <span className={riskColor[step.risk]}>Risk: {step.risk}</span>}
                        </div>
                      </div>
                    </div>
                  </button>
                ))}
              </div>
            </details>
          )}
        </aside>

        <main className="min-w-0 overflow-x-hidden overflow-y-auto p-3 md:p-5">
          {selectedStep && (
            <div className="mx-auto max-w-6xl space-y-4">
              {activePass?.captured_plan && <details className="rounded border border-indigo-900 bg-indigo-950/20 p-4">
                <summary className="cursor-pointer text-sm font-semibold text-indigo-200">Plan context: {activePass.captured_plan.title} · revision {activePass.captured_plan.revision}</summary>
                <div className="mt-4 text-zinc-300"><MarkdownBody body={activePass.captured_plan.markdown} renderPlanVisuals /></div>
              </details>}
              <div data-sticky-review-action className="sticky -top-5 z-20 -mx-5 flex justify-end border-b border-zinc-800/80 bg-[#0a0a0f]/95 px-5 py-2 backdrop-blur">
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

        <aside className={`${qaOpen ? "fixed inset-2 z-50 flex rounded-lg border shadow-2xl" : "hidden"} relative min-h-0 flex-col border-zinc-800 bg-zinc-950 p-3 lg:static lg:flex lg:rounded-none lg:border-y-0 lg:border-r-0 lg:border-l`}>
          <div className="-mx-3 -mt-3 mb-4 flex min-h-14 shrink-0 items-center justify-between border-b border-zinc-800 px-3 lg:hidden">
            <h2 className="text-sm font-semibold text-zinc-200">Review feedback</h2>
            <button aria-label="Close notes" onClick={() => setQAOpen(false)} className="flex h-11 w-11 items-center justify-center rounded text-zinc-400 hover:bg-zinc-900 hover:text-zinc-200"><X size={18}/></button>
          </div>
          <div
            role="separator"
            aria-label="Resize questions and answers"
            aria-orientation="vertical"
            aria-valuemin={352}
            aria-valuemax={640}
            aria-valuenow={Math.round(qaWidthRem * 16)}
            tabIndex={0}
            onPointerDown={resizeQAFromPointer}
            onKeyDown={resizeQAFromKeyboard}
            className="absolute inset-y-0 -left-1.5 z-30 hidden w-3 cursor-col-resize touch-none focus:bg-indigo-500/30 focus:outline-none lg:block"
          />
          <section className="mb-4 border-b border-zinc-800 pb-4">
            <h3 className="px-1 text-xs font-semibold uppercase tracking-wide text-zinc-500">Requested changes</h3>
            <div className="mt-2 space-y-2">
              {findings.map((finding) => <label key={finding.id} className="flex gap-2 rounded bg-zinc-900 p-2 text-xs text-zinc-300">
                <input type="checkbox" disabled={finding.status !== "open"} checked={selectedFindingIDs.includes(finding.id)} onChange={(event) => setSelectedFindingIDs((current) => event.target.checked ? [...current, finding.id] : current.filter((id) => id !== finding.id))} />
                <span><span className={finding.status === "resolved" ? "line-through text-zinc-600" : ""}>{finding.body}</span>{finding.file_path && <span className="mt-1 block font-mono text-[10px] text-zinc-600">{finding.file_path}</span>}</span>
              </label>)}
              <textarea aria-label="Requested change" rows={2} value={findingDraft} onChange={(event) => setFindingDraft(event.target.value)} placeholder={selection ? "Describe the change for selected lines…" : "Describe a required change…"} className="w-full rounded border border-zinc-700 bg-zinc-900 p-2 text-xs" />
              <div className="flex gap-2">
                <Button variant="warning" size="sm" disabled={busy || !selectedStep || !findingDraft.trim()} onClick={() => void flagChange()}><Flag size={12}/> Flag change</Button>
                <Button size="sm" disabled={busy || selectedFindingIDs.length === 0} onClick={() => void requestPlan()}>Request follow-up plan</Button>
              </div>
            </div>
          </section>
          <h3 className="px-1 text-xs font-semibold uppercase tracking-wide text-zinc-500">Questions & answers</h3>
          <div className="mt-3 min-h-0 flex-1 space-y-2 overflow-y-auto">
            {messages.length === 0 && <p className="px-1 text-xs text-zinc-600">No questions on this step yet.</p>}
            {messages.map((message) => (
              <div key={message.id} className={cn("rounded p-2 text-sm", message.author === "user" ? "bg-indigo-950/40 text-indigo-200" : "bg-zinc-900 text-zinc-300")}>
                <div className="mb-1 text-[10px] uppercase text-zinc-600">{message.author}</div><ReviewMessageBody body={message.body} />
              </div>
            ))}
          </div>
          {selection && (
            <div className="mt-3 flex items-center justify-between gap-2 rounded border border-indigo-800 bg-indigo-950/30 px-2 py-1.5 text-xs text-indigo-200">
              <span className="truncate">{selection.file_path} · {selection.end - selection.start + 1} selected {selection.end === selection.start ? "line" : "lines"}</span>
              <button type="button" onClick={() => setSelection(null)} className="shrink-0 text-indigo-400 hover:text-indigo-200">Clear</button>
            </div>
          )}
          <div className="mt-3 flex shrink-0 items-end gap-2 border-t border-zinc-800 pt-3">
            <textarea
              aria-label="Question for agent"
              rows={3}
              value={question}
              onChange={(event) => setQuestion(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) {
                  event.preventDefault()
                  void askQuestion()
                }
              }}
              placeholder={selection ? "Ask about selected lines…" : "Ask about this step…"}
              className="min-h-20 min-w-0 flex-1 resize-y rounded border border-zinc-700 bg-zinc-900 px-3 py-2 text-sm outline-none focus:border-indigo-600"
            />
            <div className="flex shrink-0 flex-col gap-2">
              {voiceEnabled && <VoiceRecorder baseUrl={baseUrl} transcriptionOnly onTranscription={(text) => setQuestion((current) => current ? `${current}\n${text}` : text)} />}
              <Button variant="secondary" size="icon" aria-label="Ask agent" title="Send question (Ctrl+Enter)" disabled={busy || !question.trim()} onClick={() => void askQuestion()}><Send size={15} /></Button>
            </div>
          </div>
        </aside>
      </div>}
    </div>
  )
}
