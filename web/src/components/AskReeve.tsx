import { Dialog, DialogPanel, DialogHeader, DialogTitle, DialogBody, DialogFooter } from "./ui/Dialog"
import { useEffect, useImperativeHandle, useRef, useState, type Ref } from "react"
import { createPortal } from "react-dom"
import { Crown } from "lucide-react"
import { useInRouterContext, useNavigate } from "react-router-dom"
import { useServer } from "../hooks/useServer"
import { endReeve, fetchReeve, messageReeve, startReeve, type ReeveSession } from "../lib/reeve"
import { cn } from "../lib/utils"
import { useReeveName } from "../lib/reeveName"

// What a menu entry calls to ask, when AskReeve draws no button of its own.
export interface AskReeveHandle { ask: () => void }

interface AskReeveProps {
  brief: string
  className?: string
  label?: string
  // No button: something else asks through ref (a menu entry), and AskReeve
  // keeps its dialog and its error.
  hidden?: boolean
  ref?: Ref<AskReeveHandle>
}

// AskReeve starts the Reeve with a brief about something that went wrong,
// and opens its conversation. With a session already there, the person
// chooses: add the brief to it, or end it and start fresh. Outside a router
// (a panel drawn on its own) it offers nothing.
export function AskReeve(props: AskReeveProps) {
  return useInRouterContext() ? <AskReeveButton {...props} /> : null
}

function AskReeveButton({ brief, className, label: given, hidden, ref }: AskReeveProps) {
  const reeve = useReeveName()
  const label = given ?? `Ask ${reeve.mid}`
  const { baseUrl } = useServer()
  const navigate = useNavigate()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const [current, setCurrent] = useState<ReeveSession | null>(null)
  const open = (taskId: string) => navigate(`/agents?task=${encodeURIComponent(taskId)}`)
  const run = async (action: () => Promise<string>) => {
    setBusy(true)
    setError("")
    try {
      const taskId = await action()
      setCurrent(null)
      open(taskId)
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    } finally {
      setBusy(false)
    }
  }
  const ask = async () => {
    setError("")
    try {
      const overview = await fetchReeve(baseUrl)
      if (overview.current && overview.current.state !== "ended") {
        setCurrent(overview.current)
        return
      }
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
      return
    }
    await run(async () => (await startReeve(baseUrl, { brief })).task_id)
  }
  // One handle for the component's life, asking with the latest brief.
  const latest = useRef(ask)
  useEffect(() => { latest.current = ask })
  useImperativeHandle(ref, () => ({ ask: () => void latest.current() }), [])
  return (
    <>
      {!hidden && <button type="button" disabled={busy} onClick={() => void ask()}
        className={cn("inline-flex min-h-11 items-center gap-1.5 rounded-lg border border-[var(--theme-border,rgba(139,92,246,0.5))] px-3 text-sm text-[var(--theme-keyword,#c4b5fd)] hover:bg-[var(--theme-selection,rgba(139,92,246,0.1))] disabled:opacity-50 md:min-h-8 md:text-xs", className)}>
        <Crown size={13} aria-hidden="true" />
        {label}
      </button>}
      {error && !current && <span role="alert" className="text-xs text-lw-error">{error}</span>}
      {current && createPortal(
        <Dialog open={!!current} onClose={() => setCurrent(null)}><DialogPanel className="max-w-[420px] font-lw text-lw-text"><DialogHeader>
            <DialogTitle className="text-base font-semibold sm:text-sm">{reeve.start} is already {current.state === "suspended" ? "suspended in a conversation" : "in a conversation"}</DialogTitle></DialogHeader><DialogBody>
            <p className="text-sm text-lw-text-2 sm:text-xs">Add this to what it is doing, or end that conversation and start a fresh one about this.</p>
            {error && <p role="alert" className="text-xs text-lw-error">{error}</p>}
            </DialogBody><DialogFooter className="flex-col sm:flex-row">
              <button type="button" disabled={busy} onClick={() => setCurrent(null)} className="min-h-11 rounded-lg px-3 text-sm text-lw-text-2 hover:bg-lw-raised sm:min-h-8 sm:text-xs">Cancel</button>
              <button type="button" disabled={busy} className="min-h-11 rounded-lg border border-lw-line-strong px-3 text-sm hover:bg-lw-raised sm:min-h-8 sm:text-xs"
                onClick={() => void run(async () => { await endReeve(baseUrl, current.task_id); return (await startReeve(baseUrl, { brief })).task_id })}>
                End it and start fresh
              </button>
              <button type="button" disabled={busy} className="min-h-11 rounded-lg border border-[var(--theme-border,rgba(139,92,246,0.6))] bg-[var(--theme-selection,rgba(139,92,246,0.2))] px-3 text-sm text-[var(--theme-keyword,#ddd6fe)] hover:bg-[var(--theme-selection,rgba(139,92,246,0.3))] sm:min-h-8 sm:text-xs"
                onClick={() => void run(async () => { await messageReeve(baseUrl, brief); return current.task_id })}>
                Add to the current conversation
              </button>
            </DialogFooter>
          </DialogPanel>
        </Dialog>,
        document.body,
      )}
    </>
  )
}
