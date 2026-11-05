import { useState } from "react"

import { awayActions, doneOnWorker, recallTask, returnAllDone, returnTask } from "../../lib/awayReturn"
import type { BoardCard, BoardWorker } from "../../lib/board-types"
import { Button } from "../ui/Button"
import { Dialog, DialogBody, DialogClose, DialogFooter, DialogHeader, DialogPanel, DialogTitle } from "../ui/Dialog"

// The actions that bring away tasks home, on an Away lane row, in an away
// task's panel, and on a worker group's header: Return, Recall (asking
// first when its agent may be at work) and Return all done. The board
// refreshes by itself once a task is home. See docs/ai/web-ui.md
// § Returning and recalling.

// onNotice is told what happened: a task back home (error false), or why
// a call was refused (error true).
type Notice = (text: string, error: boolean) => void

const actionButton = "rounded-md border border-lw-line px-2 py-1 text-xs font-medium text-lw-text hover:bg-lw-raised disabled:opacity-50"

export function AwayRowActions({ baseUrl, card, onNotice }: { baseUrl: string; card: BoardCard; onNotice?: Notice }) {
  const [busy, setBusy] = useState(false)
  const [confirming, setConfirming] = useState(false)
  const [error, setError] = useState("")
  const away = card.away
  if (!away) return null
  const actions = awayActions(away)
  if (!actions.return && !actions.recall) return null

  const bring = async (recall: boolean) => {
    setBusy(true)
    setError("")
    try {
      const back = await (recall ? recallTask : returnTask)(baseUrl, card.id)
      onNotice?.(`${card.id} is back in ${back.column}`, false)
    } catch (e) {
      const text = e instanceof Error ? e.message : String(e)
      setError(text)
      onNotice?.(text, true)
    } finally {
      setBusy(false)
      setConfirming(false)
    }
  }

  return (
    <div className="flex flex-wrap items-center gap-1" onClick={(e) => e.stopPropagation()}>
      {actions.return && <button type="button" disabled={busy} onClick={() => bring(false)} className={actionButton}>Return</button>}
      {actions.recall && <button type="button" disabled={busy} onClick={() => (actions.confirmRecall ? setConfirming(true) : bring(true))} className={actionButton}>Recall</button>}
      {error && <span role="alert" className="basis-full text-xs text-lw-error">{error}</span>}
      <Dialog open={confirming} onClose={() => setConfirming(false)} dismissible={!busy}>
        <DialogPanel className="max-w-sm">
          <DialogHeader><DialogTitle>Recall {card.id}?</DialogTitle><DialogClose disabled={busy} /></DialogHeader>
          <DialogBody className="space-y-2">
            <p className="text-xs text-zinc-400">{card.title}</p>
            <p className="text-sm text-zinc-300">Its agent on {away.worker} may still be working. Recalling stops it and brings back what there is so far: its notes, branches and edits.</p>
          </DialogBody>
          <DialogFooter>
            <Button variant="ghost" size="sm" disabled={busy} onClick={() => setConfirming(false)}>Cancel</Button>
            <Button variant="warning" size="sm" disabled={busy} onClick={() => bring(true)}>Recall now</Button>
          </DialogFooter>
        </DialogPanel>
      </Dialog>
    </div>
  )
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
