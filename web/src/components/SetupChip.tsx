import { useRef, useState } from "react"
import { Link } from "react-router-dom"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { Check, Circle, X } from "lucide-react"
import { useModalLayer } from "../hooks/useModalLayer"
import { fetchSetup, setSetupDismissed, type SetupSummary } from "../lib/setup"
import { queryKeys } from "../lib/query"

// SetupChip is "Setup n/N" while the server says there is setup left (or a
// required check fails); it opens the checklist. summary comes from
// /api/settings, so the chip costs no request until it is opened.
export function SetupChip({ baseUrl, summary }: { baseUrl: string; summary: SetupSummary | undefined }) {
  const [open, setOpen] = useState(false)
  if (!summary?.show && !open) return null
  return (
    <>
      {summary?.show && (
        <button type="button" onClick={() => setOpen(true)}
          className="rounded-full border border-lw-accent/50 bg-lw-accent-soft px-3 py-1 text-xs font-medium text-lw-accent-text hover:bg-lw-raised">
          Setup {summary.done}/{summary.total}
        </button>
      )}
      {open && <SetupChecklistDialog baseUrl={baseUrl} onClose={() => setOpen(false)} />}
    </>
  )
}

function SetupChecklistDialog({ baseUrl, onClose }: { baseUrl: string; onClose: () => void }) {
  const queryClient = useQueryClient()
  const { data, error } = useQuery({ queryKey: queryKeys.setup(baseUrl), queryFn: () => fetchSetup(baseUrl) })
  const [hiding, setHiding] = useState(false)
  const dialogRef = useRef<HTMLDivElement>(null)
  useModalLayer({ open: true, onClose, containerRef: dialogRef })

  const hide = async () => {
    setHiding(true)
    try {
      queryClient.setQueryData(queryKeys.setup(baseUrl), await setSetupDismissed(baseUrl, true))
      await queryClient.invalidateQueries({ queryKey: queryKeys.settings(baseUrl) })
      onClose()
    } finally {
      setHiding(false)
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-end justify-center bg-black/60 sm:items-center sm:p-4" onClick={onClose}>
      <div ref={dialogRef} role="dialog" aria-modal="true" aria-labelledby="setup-title" onClick={(e) => e.stopPropagation()}
        className="flex max-h-[calc(100dvh-1rem)] w-full flex-col overflow-hidden rounded-t-2xl border-t border-lw-line-strong bg-lw-surface text-[15px] leading-5 text-lw-text shadow-[0_24px_60px_rgba(0,0,0,.5)] sm:w-[480px] sm:rounded-xl sm:border sm:text-[13px]">
        <div className="flex shrink-0 items-center justify-between gap-3 border-b border-lw-line px-5 py-3.5">
          <div>
            <h2 id="setup-title" className="text-base font-semibold sm:text-sm">Setup {data ? `${data.done}/${data.total}` : ""}</h2>
            <p className="text-xs text-lw-text-2">What is left before Legato has what it needs. Each takes you to its place in Settings.</p>
          </div>
          <button onClick={onClose} aria-label="Close" className="flex h-11 w-11 items-center justify-center rounded-md text-lw-text-2 hover:bg-lw-raised hover:text-lw-text sm:h-8 sm:w-8">
            <X size={16} />
          </button>
        </div>
        <div className="flex min-h-0 flex-col gap-3 overflow-y-auto overscroll-contain px-5 py-4">
          {error && <p role="alert" className="text-xs text-lw-error">{error instanceof Error ? error.message : String(error)}</p>}
          <ul className="flex flex-col gap-1">
            {data?.items.map((item) => (
              <li key={item.id}>
                <Link to={`/settings#${item.settings ?? ""}`} onClick={onClose}
                  className="flex items-center gap-2 rounded-md px-2 py-2 hover:bg-lw-raised">
                  {item.done ? <Check size={14} className="text-lw-done" aria-label="done" /> : <Circle size={14} className="text-lw-faint" aria-label="to do" />}
                  <span className={item.done ? "text-lw-text-2 line-through" : ""}>{item.title}</span>
                  {item.optional && <span className="ml-auto text-xs text-lw-faint">optional</span>}
                </Link>
              </li>
            ))}
          </ul>
        </div>
        <div className="flex shrink-0 justify-end border-t border-lw-line px-5 py-3">
          <button type="button" disabled={hiding} onClick={() => void hide()}
            className="rounded px-3 py-1.5 text-sm text-lw-text-2 hover:bg-lw-raised disabled:opacity-50">
            Hide the checklist
          </button>
        </div>
      </div>
    </div>
  )
}
