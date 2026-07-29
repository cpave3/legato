import { useState } from "react"
import { Link } from "react-router-dom"
import { ClipboardList, ListChecks, Workflow as WorkflowIcon, X, Layers } from "lucide-react"
import type { RelatedWorkItem } from "../lib/task-activity"
import type { LegatoNavigationKind } from "../lib/navigation"

const KIND_ICON: Record<LegatoNavigationKind, typeof ClipboardList> = {
  plan: ClipboardList,
  review: ListChecks,
  workflow_run: WorkflowIcon,
  task: ClipboardList,
}

const KIND_LABEL: Record<LegatoNavigationKind, string> = {
  plan: "Plan",
  review: "Review",
  workflow_run: "Workflow",
  task: "Task",
}

const STATUS_COLORS: Record<string, string> = {
  proposed: "text-amber-300",
  changes_requested: "text-amber-300",
  approved: "text-emerald-400",
  rejected: "text-red-400",
  completed: "text-emerald-400",
  ready: "text-amber-300",
  reviewed: "text-emerald-400",
  capturing: "text-zinc-400",
  running: "text-indigo-300",
  succeeded: "text-emerald-400",
  failed: "text-red-400",
  waiting_agent: "text-amber-300",
  waiting_manual: "text-amber-300",
  pending: "text-zinc-500",
  cancelled: "text-zinc-500",
}

function statusClass(status: string): string {
  return STATUS_COLORS[status] ?? "text-zinc-400"
}

function statusLabel(status: string): string {
  return status.replace(/_/g, " ")
}

interface RelatedWorkStripProps {
  items: RelatedWorkItem[]
}

/**
 * Slide-out drawer of related plans, reviews, and workflow runs. A small
 * trigger button stays fixed in the top-right corner so it costs no layout
 * space until opened. The drawer slides in from the right with a backdrop.
 */
export function RelatedWorkStrip({ items }: RelatedWorkStripProps) {
  const [open, setOpen] = useState(false)

  if (items.length === 0) return null

  return (
    <>
      <button
        onClick={() => setOpen(true)}
        aria-label={`Related work (${items.length})`}
        className="absolute right-3 top-3 z-30 flex items-center gap-1.5 rounded-full border border-zinc-700 bg-zinc-900 px-3 py-1.5 text-xs text-zinc-300 shadow-lg transition-colors hover:bg-zinc-800 hover:text-zinc-100"
        data-related-work
      >
        <Layers size={14} className="shrink-0 text-zinc-400" />
        <span>{items.length}</span>
      </button>

      {open && (
        <div className="fixed inset-0 z-50 flex justify-end bg-black/60" onClick={() => setOpen(false)} data-related-work-backdrop>
          <div
            role="dialog"
            aria-modal="true"
            aria-label="Related work"
            className="flex h-full w-full max-w-sm flex-col border-l border-zinc-800 bg-zinc-950 shadow-2xl"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="flex items-center justify-between border-b border-zinc-800 px-4 py-3">
              <h2 className="flex items-center gap-2 text-sm font-semibold text-zinc-200">
                <Layers size={16} className="text-zinc-400" />
                Related work
              </h2>
              <button
                onClick={() => setOpen(false)}
                aria-label="Close"
                className="rounded p-1 text-zinc-500 transition-colors hover:bg-zinc-800 hover:text-zinc-200"
              >
                <X size={18} />
              </button>
            </div>
            <div className="min-h-0 flex-1 overflow-y-auto p-3">
              <ul className="space-y-2">
                {items.map((item) => {
                  const Icon = KIND_ICON[item.kind]
                  return (
                    <li key={`${item.kind}:${item.id}`}>
                      <Link
                        to={item.href}
                        onClick={() => setOpen(false)}
                        className="group flex items-center gap-2.5 rounded-lg border border-zinc-800 bg-zinc-900 px-3 py-2.5 text-sm transition-colors hover:border-zinc-600 hover:bg-zinc-800"
                      >
                        <Icon aria-label={KIND_LABEL[item.kind]} size={15} className="shrink-0 text-zinc-400 group-hover:text-zinc-200" />
                        <span className="min-w-0 flex-1 truncate text-zinc-200">{item.title}</span>
                        <span className={`shrink-0 text-xs ${statusClass(item.status)}`}>{statusLabel(item.status)}</span>
                      </Link>
                    </li>
                  )
                })}
              </ul>
            </div>
          </div>
        </div>
      )}
    </>
  )
}
