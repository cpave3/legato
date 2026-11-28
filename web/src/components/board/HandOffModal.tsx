import { useEffect, useId, useState, type FormEvent } from "react"

import { Dialog, DialogBody, DialogClose, DialogHeader, DialogPanel, DialogTitle } from "../ui/Dialog"
import { handOffTask, type HandOffRequest } from "../../lib/away"
import type { AwayOn, BoardCard } from "../../lib/board-types"
import { chosenWorkerView, handOffReady, handOffRequest } from "../../lib/handoffForm"
import { handoffTools, missingOnWorker, workerOnlyRepositories } from "../../lib/workers"
import type { RepositoryPreset } from "../RepositoryPresetSelect"
import { Choices, control, fieldLabel, HandOffBlockers, HandOffError, HandOffFooter, HandOffNote, RepositoryField, ReviewPassesSection, ToolField, WorkerField, WorkerWarnings } from "./HandOffFields"
import { errorText, useFollowedTool, useHandOffSources, useWorkerReviewPasses } from "./useHandOffSources"

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

export function HandOffModal({ baseUrl, open, card, repositories, onClose, onHandedOff }: HandOffModalProps) {
  const id = useId()
  const [worker, setWorker] = useState("")
  const [purpose, setPurpose] = useState<Purpose>("make-change")
  const [startFrom, setStartFrom] = useState<StartFrom>("new-branch")
  const [branch, setBranch] = useState("")
  const offered = repositories.filter((repository) => repository.repo)
  const [repo, setRepo] = useState("")
  const [error, setError] = useState<string | null>(null)
  const [sending, setSending] = useState(false)
  const { advert, review, setReview } = useWorkerReviewPasses(open, baseUrl, worker)
  const { workers, localTools, here } = useHandOffSources(open, baseUrl, setWorker, setError)

  // No repository is chosen for the person: work sent to the wrong one is
  // easy to miss, so the form waits for a choice every time it opens.
  useEffect(() => {
    if (!open) setRepo("")
  }, [open])

  const { advert: advertOf, name: workerName, repoBlocked } = chosenWorkerView(workers, worker, repo)
  // The tools the chosen worker can run unattended, its default first; the
  // choice follows the worker, whichever list arrived first, until the
  // person chooses.
  const tools = handoffTools(advertOf, localTools)
  const { tool, choose: chooseTool } = useFollowedTool(open, tools)

  const agentHere = card.agent_active
  const pushed = startFrom === "pushed-branch"
  const canSend = !sending && !agentHere && handOffReady({ worker, repo, kind: startFrom, branch }, repoBlocked)
  const makesChange = purpose === "make-change"

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    if (!canSend) return
    setSending(true)
    setError(null)
    try {
      const away = await handOffTask(baseUrl, card.id, handOffRequest({ worker, purpose, kind: startFrom, repo, branch, tool, advert, review }))
      onHandedOff(away)
      onClose()
    } catch (reason) {
      setError(errorText(reason))
    } finally {
      setSending(false)
    }
  }

  return (
    <Dialog open={open} onClose={onClose} dismissible={!sending}>
      <DialogPanel className="flex max-h-[calc(100dvh-2rem)] max-w-lg flex-col font-lw">
        <form onSubmit={submit} className="flex min-h-0 flex-1 flex-col">
          <DialogHeader><DialogTitle>Hand off “{card.title}”</DialogTitle><DialogClose disabled={sending} /></DialogHeader>
          <DialogBody className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto overscroll-contain">
            <HandOffBlockers agentHere={agentHere} workers={workers} />
            <WorkerField worker={worker} workers={workers} onChange={setWorker} />
            <Choices name={`${id}-purpose`} legend="Purpose" value={purpose} options={purposes} onChange={setPurpose} />
            <Choices name={`${id}-start`} legend="Start from" value={startFrom} options={startChoices(card.id)} onChange={setStartFrom} />
            {pushed && (
              <label className="flex flex-col gap-1.5">
                <span className={fieldLabel}>Branch</span>
                <input aria-label="Branch" value={branch} onChange={(event) => setBranch(event.target.value)} placeholder="the branch's name on GitHub" autoComplete="off" spellCheck={false} className={`${control} font-lw-mono`} />
                <span className="text-xs text-lw-text-2">The agent reads its commits first, then only adds commits to it and reuses its PR. A branch not on GitHub fails the handoff.</span>
              </label>
            )}
            <RepositoryField repo={repo} offered={offered} workerOnly={workerOnlyRepositories(advertOf, offered.map((r) => r.repo))} advert={advertOf} workerName={workerName} onChange={setRepo} />
            <ToolField tool={tool} tools={tools} onChoose={chooseTool} />
            <WorkerWarnings repoBlocked={repoBlocked} repo={repo} lacks={missingOnWorker(advertOf, here)} workerName={workerName} />
            {advert && makesChange && <ReviewPassesSection advert={advert} review={review} onChange={setReview} />}
            <HandOffNote workerName={workerName} makesChange={makesChange} />
            <HandOffError error={error} />
          </DialogBody>
          <HandOffFooter sending={sending} canSend={canSend} onClose={onClose} />
        </form>
      </DialogPanel>
    </Dialog>
  )
}
