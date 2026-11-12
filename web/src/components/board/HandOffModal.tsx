import { useEffect, useId, useState, type FormEvent } from "react"
import { Link } from "react-router-dom"

import { Button } from "../ui/Button"
import { Dialog, DialogBody, DialogClose, DialogFooter, DialogHeader, DialogPanel, DialogTitle } from "../ui/Dialog"
import { apiFetch } from "../../lib/api"
import { agentKindLabel } from "../../lib/agentKind"
import { handOffTask, type HandOffRequest, workerReviewPasses, type ReviewPassesAdvert, type ReviewOverride } from "../../lib/away"
import type { AwayOn, BoardCard } from "../../lib/board-types"
import { listWorkers, type Worker } from "../../lib/workers"
import type { RepositoryPreset } from "../RepositoryPresetSelect"

// The Hand off form: one form whichever way a task is handed off (its card's
// menu, its task panel). It asks for the worker, the purpose, where the work
// starts, the repository and the agent tool, and shows a refusal in the
// server's words. See docs/ai/web-ui.md § Handing off a task.

interface HandOffModalProps {
  baseUrl: string
  open: boolean
  card: BoardCard
  // The repositories Legato offers, as the spawn dialog lists them; those
  // with a GitHub name can be handed off.
  repositories: RepositoryPreset[]
  onClose: () => void
  onHandedOff: (away: AwayOn) => void
}

type Purpose = HandOffRequest["purpose"]
type StartFrom = HandOffRequest["kind"]

const purposes: { value: Purpose; label: string; note: string }[] = [
  { value: "make-change", label: "Make a change", note: "Pushes its branches, opens draft PRs" },
  { value: "find-out", label: "Find out", note: "Answers in notes, pushes nothing" },
]

// Where the work can start: a new branch named after the task, or a branch
// already pushed to GitHub, named in a field below. The worker fetches and
// refuses a branch GitHub does not have.
function startChoices(taskId: string): { value: StartFrom; label: string; note: string }[] {
  return [
    { value: "new-branch", label: "A new branch", note: `legato/${taskId.toLowerCase()}` },
    { value: "pushed-branch", label: "A pushed branch", note: "Work you began, on GitHub" },
  ]
}

const fieldLabel = "text-xs text-lw-text-2"
// 16px on a phone: a smaller field makes iOS zoom the page when it is focused.
const control = "min-h-11 w-full rounded-md border border-lw-line-strong bg-lw-ground px-3 text-base text-lw-text outline-none focus:border-lw-accent sm:min-h-10 sm:text-sm"

function Choices<T extends string>({ name, legend, value, options, onChange }: {
  name: string
  legend: string
  value: T
  options: { value: T; label: string; note: string }[]
  onChange: (value: T) => void
}) {
  return (
    <fieldset className="flex flex-col gap-1.5">
      <legend className={`${fieldLabel} mb-1.5`}>{legend}</legend>
      <div className="flex flex-col overflow-hidden rounded-md border border-lw-line-strong bg-lw-ground sm:flex-row">
        {options.map((option) => (
          <label key={option.value} className={`flex min-h-11 flex-1 basis-0 cursor-pointer items-start gap-2.5 border-b border-lw-line px-3 py-2.5 last:border-b-0 sm:border-b-0 sm:border-r sm:last:border-r-0 ${value === option.value ? "bg-lw-selected" : ""}`}>
            <input type="radio" name={name} value={option.value} checked={value === option.value} onChange={() => onChange(option.value)} className="mt-1 accent-lw-accent" />
            <span className="flex min-w-0 flex-col gap-px">
              <span className="text-sm font-medium text-lw-text">{option.label}</span>
              <span className="break-words font-lw-mono text-xs text-lw-text-2">{option.note}</span>
            </span>
          </label>
        ))}
      </div>
    </fieldset>
  )
}

export function HandOffModal({ baseUrl, open, card, repositories, onClose, onHandedOff }: HandOffModalProps) {
  const id = useId()
  const [workers, setWorkers] = useState<Worker[] | null>(null)
  const [tools, setTools] = useState<string[]>([])
  const [worker, setWorker] = useState("")
  const [purpose, setPurpose] = useState<Purpose>("make-change")
  const [startFrom, setStartFrom] = useState<StartFrom>("new-branch")
  const [branch, setBranch] = useState("")
  const offered = repositories.filter((repository) => repository.repo)
  const [repo, setRepo] = useState("")
  const [tool, setTool] = useState("")
  const [error, setError] = useState<string | null>(null)
  const [sending, setSending] = useState(false)
  // The chosen worker's review passes, and this handoff's changes to them.
  const [advert, setAdvert] = useState<ReviewPassesAdvert | null>(null)
  const [review, setReview] = useState<ReviewOverride>({})

  useEffect(() => {
    if (!open || worker === "") {
      setAdvert(null)
      return
    }
    let current = true
    setReview({})
    workerReviewPasses(baseUrl, worker).then((found) => { if (current) setAdvert(found) })
    return () => { current = false }
  }, [open, baseUrl, worker])

  useEffect(() => {
    if (!open) return
    let current = true
    setError(null)
    listWorkers(baseUrl)
      .then((list) => {
        if (!current) return
        setWorkers(list.workers)
        setWorker((chosen) => chosen || list.workers.find((w) => w.sandbox)?.instance_id || "")
      })
      .catch((reason) => { if (current) setError(reason instanceof Error ? reason.message : String(reason)) })
    // The tools this Legato drives, its default first. A handed-off agent
    // runs unattended, which a plain shell cannot.
    apiFetch(baseUrl, "/api/adapters")
      .then(async (response) => {
        if (!response.ok) return
        const data = await response.json()
        const adapters: string[] = (data.adapters ?? []).filter((adapter: string) => adapter && adapter !== "shell")
        const first: string = data.default && data.default !== "shell" ? data.default : adapters[0] ?? ""
        const ordered = first ? [first, ...adapters.filter((adapter) => adapter !== first)] : adapters
        if (!current) return
        setTools(ordered)
        setTool((chosen) => chosen || ordered[0] || "")
      })
      .catch(() => {})
    return () => { current = false }
  }, [open, baseUrl])

  // No repository is chosen for the person: work sent to the wrong one is
  // easy to miss, so the form waits for a choice every time it opens.
  useEffect(() => {
    if (!open) setRepo("")
  }, [open])

  const agentHere = card.agent_active
  const pushed = startFrom === "pushed-branch"
  const canSend = !sending && !agentHere && worker !== "" && repo.trim().includes("/") && (!pushed || branch.trim() !== "")

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    if (!canSend) return
    setSending(true)
    setError(null)
    try {
      const request: HandOffRequest = { worker, purpose, kind: startFrom, repo: repo.trim(), agent_kind: tool }
      if (advert && purpose === "make-change" && Object.keys(review).length > 0) request.review = review
      if (pushed) request.branch = branch.trim()
      const away = await handOffTask(baseUrl, card.id, request)
      onHandedOff(away)
      onClose()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    } finally {
      setSending(false)
    }
  }

  const chosen = workers?.find((w) => w.instance_id === worker)
  return (
    <Dialog open={open} onClose={onClose} dismissible={!sending}>
      <DialogPanel className="flex max-h-[calc(100dvh-2rem)] max-w-lg flex-col font-lw">
        <form onSubmit={submit} className="flex min-h-0 flex-1 flex-col">
          <DialogHeader><DialogTitle>Hand off “{card.title}”</DialogTitle><DialogClose disabled={sending} /></DialogHeader>
          <DialogBody className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto overscroll-contain">
            {agentHere && (
              <p className="rounded-md border border-lw-needs/40 bg-lw-needs-soft px-3 py-2 text-xs text-lw-needs">
                Stop this task's agent before handing it off: two agents never work on one task.
              </p>
            )}
            {workers && workers.length === 0 && (
              <p className="text-sm text-lw-text-2">
                No worker is paired. <Link to="/settings#workers" className="text-lw-link hover:underline">Add a worker</Link> in Settings › Workers first.
              </p>
            )}
            <label className="flex flex-col gap-1.5">
              <span className={fieldLabel}>Worker</span>
              <select aria-label="Worker" value={worker} onChange={(event) => setWorker(event.target.value)} className={control}>
                {worker === "" && <option value="">{workers ? "No worker can take it" : "Reading the workers…"}</option>}
                {(workers ?? []).map((w) => (
                  <option key={w.instance_id} value={w.instance_id} disabled={!w.sandbox}>
                    {w.sandbox ? w.name : `${w.name} · sandbox off: it refuses handoffs`}
                  </option>
                ))}
              </select>
            </label>
            <Choices name={`${id}-purpose`} legend="Purpose" value={purpose} options={purposes} onChange={setPurpose} />
            <Choices name={`${id}-start`} legend="Start from" value={startFrom} options={startChoices(card.id)} onChange={setStartFrom} />
            {pushed && (
              <label className="flex flex-col gap-1.5">
                <span className={fieldLabel}>Branch</span>
                <input aria-label="Branch" value={branch} onChange={(event) => setBranch(event.target.value)} placeholder="the branch's name on GitHub" autoComplete="off" spellCheck={false} className={`${control} font-lw-mono`} />
                <span className="text-xs text-lw-text-2">The agent reads its commits first, then only adds commits to it and reuses its PR. A branch not on GitHub fails the handoff.</span>
              </label>
            )}
            <label className="flex flex-col gap-1.5">
              <span className={fieldLabel}>Repository</span>
              {offered.length > 0 ? (
                <select aria-label="Repository" required value={repo} onChange={(event) => setRepo(event.target.value)} className={control}>
                  <option value="" disabled>Choose a repository…</option>
                  {offered.map((repository) => <option key={repository.repo} value={repository.repo}>{repository.name === repository.repo ? repository.repo : `${repository.name} · ${repository.repo}`}</option>)}
                </select>
              ) : (
                <input aria-label="Repository" required value={repo} onChange={(event) => setRepo(event.target.value)} placeholder="owner/name on GitHub" className={`${control} font-lw-mono`} />
              )}
            </label>
            <label className="flex flex-col gap-1.5">
              <span className={fieldLabel}>Agent tool</span>
              <select aria-label="Agent tool" value={tool} onChange={(event) => setTool(event.target.value)} className={control}>
                {tools.length === 0 && <option value="">The worker's default</option>}
                {tools.map((kind) => <option key={kind} value={kind}>{agentKindLabel(kind)}</option>)}
              </select>
            </label>
            {advert && purpose === "make-change" && <ReviewPassesSection advert={advert} review={review} onChange={setReview} />}
            <p className="text-xs text-lw-text-2">
              The agent works on its own in {chosen?.name ?? "the worker"}'s sandbox
              {purpose === "make-change" ? " and can push its branches and open draft PRs" : " and pushes nothing"}.
              This card is read-only here until it comes back.
            </p>
            {error && <p role="alert" className="rounded-md border border-lw-danger-line bg-lw-error-soft px-3 py-2 text-xs text-lw-danger">{error}</p>}
          </DialogBody>
          <DialogFooter className="shrink-0 pb-[max(0.75rem,env(safe-area-inset-bottom))]">
            <Button type="button" variant="outline" disabled={sending} onClick={onClose}>Cancel</Button>
            <Button type="submit" disabled={!canSend}>{sending ? "Handing off…" : "Hand off"}</Button>
          </DialogFooter>
        </form>
      </DialogPanel>
    </Dialog>
  )
}

// ReviewPassesSection changes a worker's review passes for one handoff:
// on or off, the reviewer's tool and its model or tier, the skill, and the
// passes allowed. Each starts at the worker's own setting.
function ReviewPassesSection({ advert, review, onChange }: { advert: ReviewPassesAdvert; review: ReviewOverride; onChange: (r: ReviewOverride) => void }) {
  const d = advert.defaults
  const enabled = review.enabled ?? d.enabled
  const agent = review.agent ?? d.agent
  const tiers = advert.adapters.find((a) => a.name === agent)?.tiers ?? []
  const set = (change: ReviewOverride) => onChange({ ...review, ...change })
  return (
    <fieldset aria-label="Review passes" className="flex flex-col gap-2 rounded-md border border-lw-line px-3 py-2.5">
      <legend className={fieldLabel}>Review passes</legend>
      <label className="flex items-center gap-2 text-sm text-lw-text">
        <input type="checkbox" aria-label="Review this handoff's pull requests" checked={enabled} onChange={(e) => set({ enabled: e.target.checked })} />
        A reviewer on another model reviews its pull requests before it is done
      </label>
      {enabled && (
        <div className="grid grid-cols-2 gap-2">
          <label className="flex flex-col gap-1">
            <span className="text-xs text-lw-text-2">Reviewer</span>
            <select aria-label="Reviewer" value={agent} onChange={(e) => set({ agent: e.target.value, tier: undefined, model: undefined })} className={control}>
              {advert.adapters.map((a) => <option key={a.name} value={a.name}>{agentKindLabel(a.name)}</option>)}
            </select>
          </label>
          {tiers.length > 0 ? (
            <label className="flex flex-col gap-1">
              <span className="text-xs text-lw-text-2">Tier</span>
              <select aria-label="Reviewer tier" value={review.tier ?? (agent === d.agent ? d.tier ?? "" : "")} onChange={(e) => set({ tier: e.target.value || undefined })} className={control}>
                <option value="">Model below</option>
                {tiers.map((t) => <option key={t} value={t}>{t}</option>)}
              </select>
            </label>
          ) : <span />}
          <label className="flex flex-col gap-1">
            <span className="text-xs text-lw-text-2">Model</span>
            <input aria-label="Reviewer model" value={review.model ?? (agent === d.agent ? d.model ?? "" : "")} onChange={(e) => set({ model: e.target.value || undefined })} placeholder="the tool's" className={control} />
          </label>
          <label className="flex flex-col gap-1">
            <span className="text-xs text-lw-text-2">Skill</span>
            <select aria-label="Review skill" value={review.skill ?? d.skill ?? ""} onChange={(e) => set({ skill: e.target.value || undefined })} className={control}>
              {!d.skill && <option value="">None</option>}
              {advert.skills.map((s) => <option key={s} value={s}>{s}</option>)}
            </select>
          </label>
          <label className="flex flex-col gap-1">
            <span className="text-xs text-lw-text-2">Passes at most</span>
            <input aria-label="Passes at most" type="number" min={1} value={review.max_passes ?? d.max_passes} onChange={(e) => set({ max_passes: Number(e.target.value) || undefined })} className={control} />
          </label>
        </div>
      )}
    </fieldset>
  )
}
