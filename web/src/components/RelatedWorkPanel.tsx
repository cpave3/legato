import { useEffect, useRef, type ReactNode } from "react"
import { Link } from "react-router-dom"
import { X } from "lucide-react"
import { cn } from "../lib/utils"
import type { CardDetail } from "../lib/board-types"
import type { RelatedWorkItem } from "../lib/task-activity"
import type { LegatoNavigationKind } from "../lib/navigation"
import { relatedStatusChip, relatedStatusLabel, relatedStatusNote, relatedStatusText, splitRelated } from "../lib/relatedWork"
import { ChecksMark, ReviewMark } from "./PullRequestMarks"

const KIND_LABEL: Record<LegatoNavigationKind, string> = {
  plan: "Plan",
  review: "Review",
  workflow_run: "Workflow",
  task: "Task",
}

interface RelatedWorkPanelProps {
  items: RelatedWorkItem[]
  // The task the agent works for, once its details have loaded.
  card: CardDetail | null
  cardId: string
  onClose: () => void
  // A phone, or a window too narrow for a column beside the chat, gets the
  // panel as a full-screen sheet.
  mobile: boolean
}

function Group({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div role="group" aria-label={label} className="flex flex-col gap-1.5">
      <span aria-hidden="true" className="px-1 text-[11px] font-semibold tracking-[1px] text-lw-faint uppercase">{label}</span>
      {children}
    </div>
  )
}

const row = "flex flex-col gap-0.5 border-b border-[#222228] px-3 py-2.5 text-lw-text no-underline last:border-b-0 hover:bg-lw-raised"
const rowHead = "flex items-center justify-between gap-2 text-xs text-lw-text-2"

// RelatedWorkPanel is the column beside an agent's chat that lists the work
// of its task: what is still under way, then the card on the board, its pull
// request, and the plans, reviews and workflow runs that came of it.
export function RelatedWorkPanel({ items, card, cardId, onClose, mobile }: RelatedWorkPanelProps) {
  const { inProgress, linked } = splitRelated(items)
  const pr = card?.pr_meta && card.pr_meta.pr_number > 0 ? card.pr_meta : null
  const container = useRef<HTMLElement>(null)

  // A sheet that covers the page takes the keyboard with it.
  useEffect(() => {
    if (mobile) container.current?.focus()
  }, [mobile])

  const body = (
    <>
      <div className="flex shrink-0 items-center justify-between border-b border-[#222228] py-2.5 pl-4 pr-2.5">
        <h2 className="m-0 text-[13px] font-semibold">Related work</h2>
        <button type="button" onClick={onClose} aria-label="Close related work" className="flex size-7 items-center justify-center rounded-md text-lw-text-2 hover:bg-lw-raised hover:text-lw-text">
          <X size={16} />
        </button>
      </div>
      <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto p-3">
        {inProgress.length > 0 && (
          <Group label="In progress">
            {inProgress.map((item) => {
              const note = relatedStatusNote(item)
              return (
                <Link key={`${item.kind}:${item.id}`} to={item.href} className="flex flex-col gap-1.5 rounded-lg border border-[#2d3a33] bg-lw-surface px-3 py-2.5 text-lw-text no-underline hover:bg-lw-raised">
                  <span className="flex items-center justify-between gap-2">
                    <span className="min-w-0 truncate font-medium">{item.title}</span>
                    <span className={cn("shrink-0 rounded px-[7px] font-lw-mono text-[11px] uppercase", relatedStatusChip(item.status))}>
                      <span aria-hidden="true">● </span>{relatedStatusLabel(item.status)}
                    </span>
                  </span>
                  <span className="text-xs text-lw-text-2">{KIND_LABEL[item.kind]}{note && ` · ${note}`}</span>
                </Link>
              )
            })}
          </Group>
        )}
        <Group label="Linked">
          <div className="flex flex-col overflow-hidden rounded-lg border border-lw-line bg-lw-surface">
            <Link to={`/board?highlight_task=${encodeURIComponent(cardId)}`} className={row} title="Show this task on the board">
              <span className={rowHead}>
                <span>Card · <span className="font-lw-mono">{cardId}</span></span>
                {card?.status && <span className="text-lw-link">{card.status}</span>}
              </span>
              {card && <span className="truncate">{card.title}</span>}
            </Link>
            {pr && (
              <a href={pr.pr_url} target="_blank" rel="noreferrer" className={row}>
                <span className={rowHead}>
                  <span>Pull request · <span className="font-lw-mono">#{pr.pr_number}</span>{pr.is_draft && " · draft"}</span>
                  <ChecksMark status={pr.check_status} />
                </span>
                <span className="truncate font-lw-mono text-xs">{pr.branch || pr.repo}</span>
                <span className="flex flex-wrap items-center gap-x-1.5 text-xs text-lw-text-2">
                  <ReviewMark decision={pr.review_decision} />
                  {pr.comment_count > 0 && <><span aria-hidden="true" className="text-lw-faint">·</span><span>{pr.comment_count} {pr.comment_count === 1 ? "comment" : "comments"}</span></>}
                </span>
              </a>
            )}
            {linked.map((item) => (
              <Link key={`${item.kind}:${item.id}`} to={item.href} className={row}>
                <span className={rowHead}>
                  <span>{KIND_LABEL[item.kind]}</span>
                  <span className={relatedStatusText(item.status)}>{relatedStatusLabel(item.status)}</span>
                </span>
                <span className="truncate">{item.title}</span>
              </Link>
            ))}
          </div>
        </Group>
        <span className="px-1 text-xs text-lw-faint">Plans, reviews and PRs this agent creates appear here.</span>
      </div>
    </>
  )

  if (mobile) {
    return (
      <section
        ref={container}
        role="dialog"
        aria-label="Related work"
        tabIndex={-1}
        onKeyDown={(event) => { if (event.key === "Escape") onClose() }}
        className="fixed inset-0 z-50 flex flex-col bg-lw-ground font-lw text-[13px] text-lw-text outline-none"
        // A sheet spans the screen, so it sets aside index.css's margin for
        // dialogs on a phone.
        style={{ maxWidth: "none", maxHeight: "none", paddingTop: "env(safe-area-inset-top)", paddingBottom: "env(safe-area-inset-bottom)" }}
      >
        {body}
      </section>
    )
  }

  return (
    <aside ref={container} aria-label="Related work" className="flex w-80 shrink-0 flex-col border-l border-[#222228] bg-[#141417] font-lw text-[13px] text-lw-text">
      {body}
    </aside>
  )
}
