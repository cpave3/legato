import type { CardDetail } from "../../lib/board-types"
import { X } from "lucide-react"
import { TaskActivityTabs } from "./TaskActivityTabs"
import { TaskAgentAction } from "./TaskAgentAction"
import { Button } from "../ui/Button"

interface DetailPanelProps {
  card: CardDetail
  onClose: () => void
  onEditTitle: () => void
  onEditDescription: () => void
  onMove: () => void
  onDelete: () => void
  onLinkPR: () => void
  onRefreshPR?: () => void
  onRediscoverPR?: () => void
  onCopyDescription: () => void
  onCopyFull: () => void
  onOpenURL: () => void
  onCancelSwarm: () => void
  onCreateWorktree?: () => void
  worktreesEnabled?: boolean
}

export function DetailPanel({
  card,
  onClose,
  onEditTitle,
  onEditDescription,
  onMove,
  onDelete,
  onLinkPR,
  onRefreshPR,
  onRediscoverPR,
  onCopyDescription,
  onCopyFull,
  onOpenURL,
  onCancelSwarm,
  onCreateWorktree = () => {},
  worktreesEnabled = false,
}: DetailPanelProps) {
  const isLocal = !card.provider
  return (
    <div
      className="fixed inset-0 z-40 flex items-end justify-center bg-black/60 md:items-start md:px-4 md:pb-8 md:pt-12"
      onClick={onClose}
    >
      <div
        className="flex h-[calc(100dvh-env(safe-area-inset-top))] w-full max-w-2xl flex-col rounded-t-xl border border-zinc-700 bg-zinc-900 shadow-2xl md:h-auto md:max-h-full md:rounded-lg"
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
          <Button variant="ghost" size="icon-sm" aria-label="Close task details" onClick={onClose}>
            <X size={16} />
          </Button>
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
          {card.worktree && (
            <>
              <div><span className="text-zinc-600">Worktree:</span> {card.worktree.path}</div>
              <div><span className="text-zinc-600">Worktree branch:</span> {card.worktree.branch}</div>
            </>
          )}
          {card.code_context && (
            <>
              <div><span className="text-zinc-600">Repository:</span> {card.code_context.repo}</div>
              {card.code_context.branch && <div><span className="text-zinc-600">Branch:</span> {card.code_context.branch}</div>}
            </>
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
              {!card.pr_meta.pr_number && card.pr_meta.sync_outcome && (
                <div className="col-span-2 text-zinc-500">PR sync: {card.pr_meta.sync_outcome.replaceAll("_", " ")}{card.pr_meta.sync_error && ` — ${card.pr_meta.sync_error}`}</div>
              )}
            </>
          )}
        </div>

        {/* Action bar */}
        <div data-task-actions className="flex flex-wrap items-center gap-2 border-b border-zinc-800 px-3 py-2 md:px-5">
          <TaskAgentAction taskId={card.id} worktreePath={card.worktree?.path}/>
          {worktreesEnabled && !card.worktree && (
            <Button variant="outline" size="sm" onClick={onCreateWorktree}>Create worktree</Button>
          )}
          {isLocal && <Button variant="outline" size="sm" title="Shortcut: t" onClick={onEditTitle}>Edit title</Button>}
          {isLocal && <Button variant="outline" size="sm" title="Shortcut: e" onClick={onEditDescription}>Edit description</Button>}
          <Button variant="ghost" size="sm" title="Shortcut: m" onClick={onMove}>Move</Button>
          <Button variant="ghost" size="sm" title="Shortcut: p" onClick={onLinkPR}>Link PR</Button>
          {(card.pr_meta?.pr_number ?? 0) > 0 && <Button variant="ghost" size="sm" onClick={onRefreshPR}>Refresh PR</Button>}
          {card.code_context?.repo && <Button variant="ghost" size="sm" onClick={onRediscoverPR}>Rediscover PR</Button>}
          <Button variant="ghost" size="sm" title="Shortcut: y" onClick={onCopyDescription}>Copy description</Button>
          <Button variant="ghost" size="sm" title="Shortcut: Shift+y" onClick={onCopyFull}>Copy full task</Button>
          {(card.remote_meta?.url || card.pr_meta?.pr_url) && <Button variant="ghost" size="sm" title="Shortcut: o" onClick={onOpenURL}>Open URL</Button>}
          {(card.swarm_stats || card.swarm_working_dir) && <Button variant="ghost" size="sm" className="text-red-400 hover:text-red-300" onClick={onCancelSwarm}>Cancel swarm</Button>}
          <Button variant="ghost" size="sm" title="Shortcut: Shift+d" className="text-red-400 hover:text-red-300" onClick={onDelete}>Delete task</Button>
        </div>

        {/* Scrollable body */}
        <div className="flex-1 overflow-y-auto px-5 py-3">
          {/* Description */}
          {card.description_md ? (
            <pre className="whitespace-pre-wrap text-xs leading-relaxed text-zinc-400">{card.description_md}</pre>
          ) : (
            <p className="text-xs text-zinc-600 italic">No description</p>
          )}
          <TaskActivityTabs taskId={card.id} notes={card.notes??[]}/>

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
