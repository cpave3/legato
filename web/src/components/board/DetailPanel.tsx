import type { CardDetail } from "../../lib/board-types"
import { X } from "lucide-react"

interface DetailPanelProps {
  card: CardDetail
  onClose: () => void
  onEditTitle: () => void
  onEditDescription: () => void
  onMove: () => void
  onDelete: () => void
  onLinkPR: () => void
  onCopyDescription: () => void
  onCopyFull: () => void
  onOpenURL: () => void
  onCancelSwarm: () => void
}

export function DetailPanel({
  card,
  onClose,
  onEditTitle,
  onEditDescription,
  onMove,
  onDelete,
  onLinkPR,
  onCopyDescription,
  onCopyFull,
  onOpenURL,
  onCancelSwarm,
}: DetailPanelProps) {
  const isLocal = !card.provider

  return (
    <div
      className="fixed inset-0 z-50 flex items-start justify-center bg-black/60 pt-12 pb-8 px-4"
      onClick={onClose}
    >
      <div
        className="flex max-h-full w-full max-w-2xl flex-col rounded-lg border border-zinc-700 bg-zinc-900 shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="flex items-start justify-between border-b border-zinc-800 px-5 py-3">
          <div className="min-w-0">
            <div className="flex items-center gap-2 text-xs text-zinc-500">
              <span className="font-mono">{card.id}</span>
              {card.status && (
                <span className="rounded bg-zinc-800 px-1.5 py-0.5 text-[10px] text-zinc-400">{card.status}</span>
              )}
            </div>
            <h3 className="mt-1 text-sm font-semibold text-zinc-200">{card.title}</h3>
          </div>
          <button
            onClick={onClose}
            className="rounded p-1 text-zinc-500 transition-colors hover:bg-zinc-800 hover:text-zinc-200"
          >
            <X size={16} />
          </button>
        </div>

        {/* Metadata grid */}
        <div className="grid grid-cols-2 gap-x-6 gap-y-1 border-b border-zinc-800 px-5 py-3 text-xs text-zinc-400">
          {card.priority && (
            <div>
              <span className="text-zinc-600">Priority:</span> {card.priority}
            </div>
          )}
          {card.remote_meta?.issue_type && (
            <div>
              <span className="text-zinc-600">Type:</span> {card.remote_meta.issue_type}
            </div>
          )}
          {card.remote_meta?.epic_name && (
            <div>
              <span className="text-zinc-600">Epic:</span> {card.remote_meta.epic_name}
            </div>
          )}
          {card.remote_meta && (card.remote_meta.labels ?? "") && (
            <div>
              <span className="text-zinc-600">Labels:</span> {card.remote_meta.labels}
            </div>
          )}
          {card.remote_meta?.url && (
            <div className="col-span-2 truncate">
              <span className="text-zinc-600">URL:</span>{" "}
              <a href={card.remote_meta.url} target="_blank" rel="noreferrer" className="text-indigo-400 hover:underline">
                {card.remote_meta.url}
              </a>
            </div>
          )}
          {card.pr_meta && (
            <>
              {card.pr_meta.pr_number > 0 && (
                <div className="col-span-2">
                  <span className="text-zinc-600">PR:</span>{" "}
                  <a href={card.pr_meta.pr_url} target="_blank" rel="noreferrer" className="text-indigo-400 hover:underline">
                    #{card.pr_meta.pr_number}
                    {card.pr_meta.is_draft && " (draft)"}
                    {card.pr_meta.state === "MERGED" && " (merged)"}
                  </a>
                  {card.pr_meta.review_decision === "APPROVED" && (
                    <span className="ml-2 text-[#1D9E75]">Approved</span>
                  )}
                  {card.pr_meta.review_decision === "CHANGES_REQUESTED" && (
                    <span className="ml-2 text-red-400">Changes Requested</span>
                  )}
                  {card.pr_meta.check_status === "pass" && (
                    <span className="ml-2 text-[#1D9E75]">CI Pass</span>
                  )}
                  {card.pr_meta.check_status === "fail" && (
                    <span className="ml-2 text-red-400">CI Fail</span>
                  )}
                  {card.pr_meta.check_status === "pending" && (
                    <span className="ml-2 text-[#E5C07B]">CI Pending</span>
                  )}
                  {card.pr_meta.comment_count > 0 && (
                    <span className="ml-2 text-zinc-500">{card.pr_meta.comment_count} comments</span>
                  )}
                </div>
              )}
              {card.pr_meta.branch && !card.pr_meta.pr_number && (
                <div className="col-span-2 text-zinc-500">Branch: {card.pr_meta.branch} — No PR found</div>
              )}
            </>
          )}
        </div>

        {/* Action bar */}
        <div className="flex flex-wrap items-center gap-1 border-b border-zinc-800 px-5 py-2">
          {isLocal && (
            <button onClick={onEditTitle} className="rounded px-2 py-1 text-[10px] text-zinc-400 hover:bg-zinc-800 hover:text-zinc-200">
              Edit title (t)
            </button>
          )}
          {isLocal && (
            <button onClick={onEditDescription} className="rounded px-2 py-1 text-[10px] text-zinc-400 hover:bg-zinc-800 hover:text-zinc-200">
              Edit desc (e)
            </button>
          )}
          <button onClick={onMove} className="rounded px-2 py-1 text-[10px] text-zinc-400 hover:bg-zinc-800 hover:text-zinc-200">
            Move (m)
          </button>
          <button onClick={onDelete} className="rounded px-2 py-1 text-[10px] text-zinc-400 hover:bg-zinc-800 hover:text-red-300">
            Delete (D)
          </button>
          <button onClick={onLinkPR} className="rounded px-2 py-1 text-[10px] text-zinc-400 hover:bg-zinc-800 hover:text-zinc-200">
            Link PR (p)
          </button>
          <button onClick={onCopyDescription} className="rounded px-2 py-1 text-[10px] text-zinc-400 hover:bg-zinc-800 hover:text-zinc-200">
            Copy desc (y)
          </button>
          <button onClick={onCopyFull} className="rounded px-2 py-1 text-[10px] text-zinc-400 hover:bg-zinc-800 hover:text-zinc-200">
            Copy full (Y)
          </button>
          {(card.remote_meta?.url || card.pr_meta?.pr_url) && (
            <button onClick={onOpenURL} className="rounded px-2 py-1 text-[10px] text-zinc-400 hover:bg-zinc-800 hover:text-zinc-200">
              Open URL (o)
            </button>
          )}
          {(card.swarm_stats || card.swarm_working_dir) && (
            <button
              onClick={onCancelSwarm}
              className="rounded px-2 py-1 text-[10px] text-red-400 hover:bg-zinc-800 hover:text-red-300"
            >
              Cancel swarm
            </button>
          )}
        </div>

        {/* Scrollable body */}
        <div className="flex-1 overflow-y-auto px-5 py-3">
          {/* Description */}
          {card.description_md ? (
            <pre className="whitespace-pre-wrap text-xs leading-relaxed text-zinc-400">{card.description_md}</pre>
          ) : (
            <p className="text-xs text-zinc-600 italic">No description</p>
          )}

          {/* Swarm sub-task tree — TODO: backend does not yet return subtasks on /api/tasks/<id> */}
          {/* {card.subtasks && card.subtasks.length > 0 && (
            <div className="mt-4">
              <div className="flex items-center gap-1 text-xs font-bold text-[#AFA9EC]">
                <Diamond size={12} /> Swarm
              </div>
              ...
            </div>
          )} */}
        </div>

        {/* Status bar */}
        <div className="border-t border-zinc-800 px-5 py-1.5 text-[10px] text-zinc-600">
          esc=back · j/k=scroll · y=copy desc · Y=copy full · o=open · m=move · D=delete
          {isLocal && " · t=edit title · e=edit desc"}
        </div>
      </div>
    </div>
  )
}
