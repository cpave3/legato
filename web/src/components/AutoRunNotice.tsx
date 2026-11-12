import { useEffect, useRef, useState } from "react"
import { fetchReeveAuto, type AutoRun } from "../lib/reeve"
import { useServer } from "../hooks/useServer"
import { useWebSocket } from "../hooks/useWebSocket"

// How long a set that ran without asking stays on screen.
const shownFor = 8000

// AutoRunNotice shows, for a few seconds, each Reeve change set that ran
// without asking: already done, so nothing to decide. It asks the server
// only when an approval is decided (approvals_changed, kind approved), and
// shows the set whose approval that was. Settings › Reeve lists them all.
export function AutoRunNotice() {
  const { baseUrl } = useServer()
  const { subscribe } = useWebSocket()
  const seen = useRef(new Set<string>())
  const [shown, setShown] = useState<AutoRun[]>([])

  useEffect(() => {
    if (!subscribe) return
    let live = true
    const timers: ReturnType<typeof setTimeout>[] = []
    const unsubscribe = subscribe((message) => {
      if (message.type !== "approvals_changed" || message.kind !== "approved" || !message.item_id) return
      const approvalId = message.item_id
      fetchReeveAuto(baseUrl).then((auto) => {
        const run = auto.recent.find((r) => r.approval_id === approvalId)
        if (!live || !run || seen.current.has(run.id)) return
        seen.current.add(run.id)
        setShown((current) => [run, ...current])
        timers.push(setTimeout(() => setShown((current) => current.filter((r) => r.id !== run.id)), shownFor))
      }).catch(() => {})
    })
    return () => {
      live = false
      timers.forEach(clearTimeout)
      unsubscribe()
    }
  }, [subscribe, baseUrl])

  if (shown.length === 0) return null
  return (
    <section aria-label="Ran without asking" role="status"
      className="fixed inset-x-2 top-[calc(env(safe-area-inset-top)+4.25rem)] z-40 flex flex-col gap-1.5 font-lw md:inset-x-auto md:left-4 md:top-auto md:bottom-4 md:w-[340px]">
      {shown.slice(0, 3).map((run) => (
        <div key={run.id} className="flex items-center gap-2 rounded-xl border border-lw-line bg-lw-surface px-3 py-2 text-sm shadow-xl">
          <span aria-hidden className="text-[#a78bfa]">◆</span>
          <span className="min-w-0 flex-1">
            <span className="block truncate text-lw-text">{run.reason}</span>
            <span className="block truncate text-xs text-lw-text-2">Ran without asking · {run.by.replace(/^auto: /, "")}</span>
          </span>
        </div>
      ))}
    </section>
  )
}
