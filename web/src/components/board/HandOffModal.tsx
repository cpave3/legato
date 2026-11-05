import { useEffect, useId, useState, type FormEvent } from "react"
import { Link } from "react-router-dom"

import { Button } from "../ui/Button"
import { Dialog, DialogBody, DialogClose, DialogFooter, DialogHeader, DialogPanel, DialogTitle } from "../ui/Dialog"
import { apiFetch } from "../../lib/api"
import { agentKindLabel } from "../../lib/agentKind"
import { handOffTask, type HandOffRequest } from "../../lib/away"
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

// Where the work can start. Starting from a branch already pushed to GitHub
// slots in here as a second choice, with a field for its name.
function startChoices(taskId: string): { value: StartFrom; label: string; note: string }[] {
  return [{ value: "new-branch", label: "A new branch", note: `legato/${taskId.toLowerCase()}` }]
}

const fieldLabel = "text-xs text-lw-text-2"
const control = "min-h-10 w-full rounded-md border border-lw-line-strong bg-lw-ground px-3 text-sm text-lw-text outline-none focus:border-lw-accent"

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
      <div className="flex overflow-hidden rounded-md border border-lw-line-strong bg-lw-ground">
        {options.map((option) => (
          <label key={option.value} className={`flex flex-1 basis-0 cursor-pointer items-start gap-2.5 border-r border-lw-line px-3 py-2 last:border-r-0 ${value === option.value ? "bg-lw-selected" : ""}`}>
            <input type="radio" name={name} value={option.value} checked={value === option.value} onChange={() => onChange(option.value)} className="mt-1 accent-lw-accent" />
            <span className="flex min-w-0 flex-col gap-px">
              <span className="text-sm font-medium text-lw-text">{option.label}</span>
              <span className="truncate font-lw-mono text-xs text-lw-text-2">{option.note}</span>
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
  const offered = repositories.filter((repository) => repository.repo)
  const [repo, setRepo] = useState(offered[0]?.repo ?? "")
  const [tool, setTool] = useState("")
  const [error, setError] = useState<string | null>(null)
  const [sending, setSending] = useState(false)

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

  // The repositories are read with the settings, perhaps after the form opened.
  const firstOffered = offered[0]?.repo ?? ""
  useEffect(() => {
    if (open && repo === "" && firstOffered) setRepo(firstOffered)
  }, [open, repo, firstOffered])

  const agentHere = card.agent_active
  const canSend = !sending && !agentHere && worker !== "" && repo.trim().includes("/")

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    if (!canSend) return
    setSending(true)
    setError(null)
    try {
      const away = await handOffTask(baseUrl, card.id, { worker, purpose, kind: startFrom, repo: repo.trim(), agent_kind: tool })
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
      <DialogPanel className="max-w-lg font-lw">
        <form onSubmit={submit} className="contents">
          <DialogHeader><DialogTitle>Hand off “{card.title}”</DialogTitle><DialogClose disabled={sending} /></DialogHeader>
          <DialogBody className="flex flex-col gap-4">
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
            <label className="flex flex-col gap-1.5">
              <span className={fieldLabel}>Repository</span>
              {offered.length > 0 ? (
                <select aria-label="Repository" value={repo} onChange={(event) => setRepo(event.target.value)} className={control}>
                  {offered.map((repository) => <option key={repository.repo} value={repository.repo}>{repository.name === repository.repo ? repository.repo : `${repository.name} · ${repository.repo}`}</option>)}
                </select>
              ) : (
                <input aria-label="Repository" value={repo} onChange={(event) => setRepo(event.target.value)} placeholder="owner/name on GitHub" className={`${control} font-lw-mono`} />
              )}
            </label>
            <label className="flex flex-col gap-1.5">
              <span className={fieldLabel}>Agent tool</span>
              <select aria-label="Agent tool" value={tool} onChange={(event) => setTool(event.target.value)} className={control}>
                {tools.length === 0 && <option value="">The worker's default</option>}
                {tools.map((kind) => <option key={kind} value={kind}>{agentKindLabel(kind)}</option>)}
              </select>
            </label>
            <p className="text-xs text-lw-text-2">
              The agent works on its own in {chosen?.name ?? "the worker"}'s sandbox
              {purpose === "make-change" ? " and can push its branches and open draft PRs" : " and pushes nothing"}.
              This card is read-only here until it comes back.
            </p>
            {error && <p role="alert" className="rounded-md border border-lw-danger-line bg-lw-error-soft px-3 py-2 text-xs text-lw-danger">{error}</p>}
          </DialogBody>
          <DialogFooter>
            <Button type="button" variant="outline" size="sm" disabled={sending} onClick={onClose}>Cancel</Button>
            <Button type="submit" size="sm" disabled={!canSend}>{sending ? "Handing off…" : "Hand off"}</Button>
          </DialogFooter>
        </form>
      </DialogPanel>
    </Dialog>
  )
}
