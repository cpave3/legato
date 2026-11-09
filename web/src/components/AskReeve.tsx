import { useState } from "react"
import { createPortal } from "react-dom"
import { Crown } from "lucide-react"
import { useInRouterContext, useNavigate } from "react-router-dom"
import { useServer } from "../hooks/useServer"
import { endReeve, fetchReeve, messageReeve, startReeve, type ReeveSession } from "../lib/reeve"
import { cn } from "../lib/utils"

interface AskReeveProps { brief: string; className?: string; label?: string }

// AskReeve starts the Reeve with a brief about something that went wrong,
// and opens its conversation. With a session already there, the person
// chooses: add the brief to it, or end it and start fresh. Outside a router
// (a panel drawn on its own) it offers nothing.
export function AskReeve(props: AskReeveProps) {
  return useInRouterContext() ? <AskReeveButton {...props} /> : null
}

function AskReeveButton({ brief, className, label = "Ask the Reeve" }: AskReeveProps) {
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
  return (
    <>
      <button type="button" disabled={busy} onClick={() => void ask()}
        className={cn("inline-flex min-h-11 items-center gap-1.5 rounded-lg border border-[#8b5cf6]/50 px-3 text-sm text-[#c4b5fd] hover:bg-[#8b5cf6]/10 disabled:opacity-50 md:min-h-8 md:text-xs", className)}>
        <Crown size={13} aria-hidden="true" />
        {label}
      </button>
      {error && <span role="alert" className="text-xs text-lw-error">{error}</span>}
      {current && createPortal(
        <div className="fixed inset-0 z-50 flex items-end justify-center bg-black/60 sm:items-center" onClick={() => setCurrent(null)}>
          <div role="dialog" aria-modal="true" aria-labelledby="ask-reeve-title" onClick={(e) => e.stopPropagation()}
            className="flex w-full flex-col gap-3 rounded-t-2xl border border-lw-line-strong bg-lw-surface p-5 font-lw text-lw-text sm:w-[420px] sm:rounded-xl">
            <h2 id="ask-reeve-title" className="text-base font-semibold sm:text-sm">The Reeve is already {current.state === "suspended" ? "suspended in a conversation" : "in a conversation"}</h2>
            <p className="text-sm text-lw-text-2 sm:text-xs">Add this to what it is doing, or end that conversation and start a fresh one about this.</p>
            <div className="flex flex-col gap-2 sm:flex-row sm:justify-end">
              <button type="button" disabled={busy} onClick={() => setCurrent(null)} className="min-h-11 rounded-lg px-3 text-sm text-lw-text-2 hover:bg-lw-raised sm:min-h-8 sm:text-xs">Cancel</button>
              <button type="button" disabled={busy} className="min-h-11 rounded-lg border border-lw-line-strong px-3 text-sm hover:bg-lw-raised sm:min-h-8 sm:text-xs"
                onClick={() => void run(async () => { await endReeve(baseUrl, current.task_id); return (await startReeve(baseUrl, { brief })).task_id })}>
                End it and start fresh
              </button>
              <button type="button" disabled={busy} className="min-h-11 rounded-lg border border-[#8b5cf6]/60 bg-[#8b5cf6]/20 px-3 text-sm text-[#ddd6fe] hover:bg-[#8b5cf6]/30 sm:min-h-8 sm:text-xs"
                onClick={() => void run(async () => { await messageReeve(baseUrl, brief); return current.task_id })}>
                Add to the current conversation
              </button>
            </div>
            {error && <p role="alert" className="text-xs text-lw-error">{error}</p>}
          </div>
        </div>,
        document.body,
      )}
    </>
  )
}
