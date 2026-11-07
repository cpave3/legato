import { useEffect, useRef, useState } from "react"
import { MoreHorizontal } from "lucide-react"

import { anotherPass, awayActions, doneOnWorker, messageAuthor, recallTask, returnAllDone, returnTask, reviewStuck, takeBackTask } from "../../lib/awayReturn"
import type { BoardCard, BoardWorker } from "../../lib/board-types"
import { removeWarning, removeWorker, removedText } from "../../lib/workers"
import { Button } from "../ui/Button"
import { Dialog, DialogBody, DialogClose, DialogFooter, DialogHeader, DialogPanel, DialogTitle } from "../ui/Dialog"

// The actions that bring away tasks home, on an Away lane row, in an away
// task's panel, and on a worker group's header: Return, Recall (asking
// first when its agent may be at work), Take back without results (always
// asking), Return all done, and the group's ⋯ menu with Remove. The board
// refreshes by itself once a task is home. See docs/ai/web-ui.md
// § Returning and recalling.

// onNotice is told what happened: a task back home (error false), or why
// a call was refused (error true).
type Notice = (text: string, error: boolean) => void

const actionButton = "rounded-md border border-lw-line px-2 py-1 text-xs font-medium text-lw-text hover:bg-lw-raised disabled:opacity-50"

export function AwayRowActions({ baseUrl, card, onNotice }: { baseUrl: string; card: BoardCard; onNotice?: Notice }) {
  const [busy, setBusy] = useState(false)
  const [confirming, setConfirming] = useState<"recall" | "take-back" | "message" | null>(null)
  const [error, setError] = useState("")
  const [message, setMessage] = useState("")
  const away = card.away
  if (!away) return null
  const stuck = reviewStuck(away)
  const actions = awayActions(away)
  if (!actions.return && !actions.recall && !actions.takeBack) return null

  const bring = async (how: "return" | "recall" | "take-back") => {
    setBusy(true)
    setError("")
    try {
      const call = how === "return" ? returnTask : how === "recall" ? recallTask : takeBackTask
      const back = await call(baseUrl, card.id)
      onNotice?.(`${card.id} is back in ${back.column}${how === "take-back" ? ", without its results" : ""}`, false)
    } catch (e) {
      const text = e instanceof Error ? e.message : String(e)
      setError(text)
      onNotice?.(text, true)
    } finally {
      setBusy(false)
      setConfirming(null)
    }
  }

  const review = async (how: "another-pass" | "message") => {
    setBusy(true)
    setError("")
    try {
      if (how === "another-pass") await anotherPass(baseUrl, card.id)
      else await messageAuthor(baseUrl, card.id, message)
      onNotice?.(how === "another-pass" ? `Another review pass of ${card.id} starts on ${away.worker}` : `Message sent to ${card.id}'s agent`, false)
      setMessage("")
      setConfirming(null)
    } catch (e) {
      const text = e instanceof Error ? e.message : String(e)
      setError(text)
      onNotice?.(text, true)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="flex flex-wrap items-center gap-1" onClick={(e) => e.stopPropagation()}>
      {actions.return && <button type="button" disabled={busy} onClick={() => bring("return")} className={actionButton}>{stuck ? "Return as is" : "Return"}</button>}
      {stuck && <button type="button" disabled={busy} onClick={() => review("another-pass")} className={actionButton}>Run another pass</button>}
      {stuck && <button type="button" disabled={busy} onClick={() => setConfirming("message")} className={actionButton}>Message the author</button>}
      {actions.recall && <button type="button" disabled={busy} onClick={() => (actions.confirmRecall ? setConfirming("recall") : bring("recall"))} className={actionButton}>Recall</button>}
      {actions.takeBack && <button type="button" disabled={busy} onClick={() => setConfirming("take-back")} className={actionButton}>Take back</button>}
      {error && <span role="alert" className="basis-full text-xs text-lw-error">{error}</span>}
      <Dialog open={confirming === "recall"} onClose={() => setConfirming(null)} dismissible={!busy}>
        <DialogPanel className="max-w-sm">
          <DialogHeader><DialogTitle>Recall {card.id}?</DialogTitle><DialogClose disabled={busy} /></DialogHeader>
          <DialogBody className="space-y-2">
            <p className="text-xs text-zinc-400">{card.title}</p>
            <p className="text-sm text-zinc-300">Its agent on {away.worker} may still be working. Recalling stops it and brings back what there is so far: its notes, branches and edits.</p>
          </DialogBody>
          <DialogFooter>
            <Button variant="ghost" size="sm" disabled={busy} onClick={() => setConfirming(null)}>Cancel</Button>
            <Button variant="warning" size="sm" disabled={busy} onClick={() => bring("recall")}>Recall now</Button>
          </DialogFooter>
        </DialogPanel>
      </Dialog>
      <Dialog open={confirming === "message"} onClose={() => setConfirming(null)} dismissible={!busy}>
        <DialogPanel className="max-w-md">
          <DialogHeader><DialogTitle>Message {card.id}'s agent</DialogTitle><DialogClose disabled={busy} /></DialogHeader>
          <DialogBody className="space-y-2">
            <p className="text-xs text-zinc-400">It reaches the agent on {away.worker} as a message from you, marked [legato main].</p>
            <textarea aria-label="Message" value={message} onChange={(e) => setMessage(e.target.value)} rows={4} className="w-full rounded-md border border-lw-line bg-lw-raised p-2 text-sm text-lw-text" />
          </DialogBody>
          <DialogFooter>
            <Button variant="ghost" size="sm" disabled={busy} onClick={() => setConfirming(null)}>Cancel</Button>
            <Button size="sm" disabled={busy || message.trim() === ""} onClick={() => review("message")}>Send</Button>
          </DialogFooter>
        </DialogPanel>
      </Dialog>
      <Dialog open={confirming === "take-back"} onClose={() => setConfirming(null)} dismissible={!busy}>
        <DialogPanel className="max-w-sm">
          <DialogHeader><DialogTitle>Take back {card.id}?</DialogTitle><DialogClose disabled={busy} /></DialogHeader>
          <DialogBody className="space-y-2">
            <p className="text-xs text-zinc-400">{card.title}</p>
            <p className="text-sm text-zinc-300">
              {takeBackReason(away.view, away.worker, away.lost)} Taking it back ends its away state here without its results: the notes,
              branches and edits made on {away.worker} stay there. {away.worker} is told to stop its agent and archive its card when it can be reached.
            </p>
          </DialogBody>
          <DialogFooter>
            <Button variant="ghost" size="sm" disabled={busy} onClick={() => setConfirming(null)}>Cancel</Button>
            <Button variant="warning" size="sm" disabled={busy} onClick={() => bring("take-back")}>Take back now</Button>
          </DialogFooter>
        </DialogPanel>
      </Dialog>
    </div>
  )
}

// takeBackReason says why the task cannot come back with its results.
function takeBackReason(view: string | undefined, worker: string, lost?: string): string {
  switch (view) {
    case "lost":
      return `${lost || `${worker} no longer has this task`}.`
    case "not_accepted":
      return `${worker} no longer accepts this main.`
    default:
      return `${worker} cannot be reached.`
  }
}

export function ReturnAllDoneButton({ baseUrl, worker, cards, onNotice }: { baseUrl: string; worker: BoardWorker; cards: BoardCard[]; onNotice?: Notice }) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const done = doneOnWorker(cards)
  if (done.length === 0) return null
  const run = async () => {
    setBusy(true)
    setError("")
    try {
      const back = await returnAllDone(baseUrl, worker.instance_id)
      onNotice?.(`${back.returned.length} ${back.returned.length === 1 ? "task" : "tasks"} back from ${worker.name}`, false)
      if (back.error) {
        setError(back.error)
        onNotice?.(back.error, true)
      }
    } catch (e) {
      const text = e instanceof Error ? e.message : String(e)
      setError(text)
      onNotice?.(text, true)
    } finally {
      setBusy(false)
    }
  }
  return (
    <>
      <button type="button" disabled={busy} onClick={run} className={actionButton}>Return all done ({done.length})</button>
      {error && <span role="alert" className="basis-full text-xs text-lw-error">{error}</span>}
    </>
  )
}

// WorkerGroupMenu is a worker group's ⋯ menu: Open on worker, and Remove,
// which warns how many away tasks the worker holds, recalls what it can,
// takes back the rest, forgets the worker and tells it to unpair this main.
export function WorkerGroupMenu({ baseUrl, worker, cards, onNotice }: { baseUrl: string; worker: BoardWorker; cards: BoardCard[]; onNotice?: Notice }) {
  const [open, setOpen] = useState(false)
  const [removing, setRemoving] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const box = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!open) return
    const close = (event: MouseEvent) => {
      if (box.current && !box.current.contains(event.target as Node)) setOpen(false)
    }
    const escape = (event: KeyboardEvent) => {
      if (event.key === "Escape") setOpen(false)
    }
    document.addEventListener("mousedown", close)
    document.addEventListener("keydown", escape)
    return () => {
      document.removeEventListener("mousedown", close)
      document.removeEventListener("keydown", escape)
    }
  }, [open])

  const remove = async () => {
    setBusy(true)
    setError("")
    try {
      const removed = await removeWorker(baseUrl, worker.instance_id)
      setRemoving(false)
      onNotice?.(removedText(removed), false)
    } catch (e) {
      const text = e instanceof Error ? e.message : String(e)
      setError(text)
      onNotice?.(text, true)
    } finally {
      setBusy(false)
    }
  }

  const held = cards.length
  const item = "flex w-full items-center gap-2 px-3 py-1.5 text-left text-xs transition-colors"
  return (
    <div ref={box} className="relative" onClick={(e) => e.stopPropagation()}>
      <button type="button" aria-label={`More for ${worker.name}`} title={`More for ${worker.name}`} aria-haspopup="menu" aria-expanded={open}
        onClick={() => setOpen((value) => !value)} className="flex h-6 w-6 items-center justify-center rounded-md text-lw-text-2 hover:bg-lw-raised">
        <MoreHorizontal size={14} />
      </button>
      {open && (
        <div role="menu" className="absolute right-0 top-full z-30 mt-1 min-w-[180px] rounded-md border border-lw-line-strong bg-lw-surface py-1 shadow-xl">
          {worker.address && <a role="menuitem" href={worker.address} target="_blank" rel="noreferrer" onClick={() => setOpen(false)} className={item + " text-lw-text hover:bg-lw-raised"}>Open on worker</a>}
          <button type="button" role="menuitem" onClick={() => { setOpen(false); setRemoving(true) }} className={item + " text-lw-error hover:bg-lw-error-soft"}>Remove…</button>
        </div>
      )}
      {error && <span role="alert" className="text-xs text-lw-error">{error}</span>}
      <Dialog open={removing} onClose={() => setRemoving(false)} dismissible={!busy}>
        <DialogPanel className="max-w-sm">
          <DialogHeader><DialogTitle>Remove {worker.name}?</DialogTitle><DialogClose disabled={busy} /></DialogHeader>
          <DialogBody className="space-y-2">
            <p className="text-sm text-zinc-300">{removeWarning(worker.name, held)}</p>
          </DialogBody>
          <DialogFooter>
            <Button variant="ghost" size="sm" disabled={busy} onClick={() => setRemoving(false)}>Cancel</Button>
            <Button variant="warning" size="sm" disabled={busy} onClick={remove}>Remove {worker.name}</Button>
          </DialogFooter>
        </DialogPanel>
      </Dialog>
    </div>
  )
}
