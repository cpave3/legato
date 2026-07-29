import { useState } from "react"
import { Link } from "react-router-dom"
import { ClipboardList, ListChecks, Workflow as WorkflowIcon, ChevronDown, ChevronUp } from "lucide-react"
import type { RelatedWorkItem } from "../lib/task-activity"
import type { LegatoNavigationKind } from "../lib/navigation"

const COLLAPSE_THRESHOLD = 4

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
 * Compact strip of related plans, reviews, and workflow runs shown above the
 * native agent panel. Collapses beyond COLLAPSE_THRESHOLD items to stay
 * non-intrusive on mobile.
 */
export function RelatedWorkStrip({ items }: RelatedWorkStripProps) {
  const [expanded, setExpanded] = useState(false)

  if (items.length === 0) return null

  const visible = expanded ? items : items.slice(0, COLLAPSE_THRESHOLD)
  const hiddenCount = items.length - visible.length

  return (
    <div className="border-b border-zinc-800 bg-zinc-950/50 px-3 py-2" data-related-work>
      <div className="flex flex-wrap items-center gap-2">
        {visible.map((item) => {
          const Icon = KIND_ICON[item.kind]
          return (
            <Link
              key={`${item.kind}:${item.id}`}
              to={item.href}
              className="group flex items-center gap-1.5 rounded border border-zinc-800 bg-zinc-900 px-2.5 py-1.5 text-xs transition-colors hover:border-zinc-600 hover:bg-zinc-800"
            >
              <Icon aria-label={KIND_LABEL[item.kind]} size={13} className="shrink-0 text-zinc-400 group-hover:text-zinc-200" />
              <span className="max-w-[12rem] truncate text-zinc-200">{item.title}</span>
              <span className={`shrink-0 ${statusClass(item.status)}`}>{statusLabel(item.status)}</span>
            </Link>
          )
        })}
        {hiddenCount > 0 && (
          <button
            onClick={() => setExpanded(true)}
            className="flex items-center gap-1 rounded px-2 py-1 text-xs text-zinc-500 transition-colors hover:bg-zinc-800 hover:text-zinc-300"
            aria-label={`Show all ${items.length} items`}
          >
            <ChevronDown size={13} />
            +{hiddenCount} more
          </button>
        )}
        {expanded && items.length > COLLAPSE_THRESHOLD && (
          <button
            onClick={() => setExpanded(false)}
            className="flex items-center gap-1 rounded px-2 py-1 text-xs text-zinc-500 transition-colors hover:bg-zinc-800 hover:text-zinc-300"
            aria-label="Collapse"
          >
            <ChevronUp size={13} />
            Less
          </button>
        )}
      </div>
    </div>
  )
}
