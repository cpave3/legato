import type { ReactNode } from "react"
import { ChevronRight, X } from "lucide-react"

import type { CardDetail } from "../../lib/board-types"
import { MarkdownBody } from "../MarkdownBody"
import { Button } from "../ui/Button"
import { TaskActivityTabs } from "./TaskActivityTabs"
import { TaskAgentAction } from "./TaskAgentAction"

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

function Property({ label, children, wide = false }: { label: string; children: ReactNode; wide?: boolean }) {
  return <div className={wide ? "sm:col-span-2 lg:col-span-1" : ""}>
    <dt className="text-[10px] font-medium uppercase tracking-wide text-zinc-600">{label}</dt>
    <dd className="mt-0.5 break-words text-xs text-zinc-300">{children}</dd>
  </div>
}

const sectionHeadingClass = "text-[10px] font-semibold uppercase tracking-wider text-zinc-500"
const sidebarButtonClass = "w-full justify-start"

function DescriptionSection({ kind, title, markdown, emptyLabel }: { kind: "remote" | "local"; title: string; markdown: string; emptyLabel: string }) {
  return <details open data-description-section={kind} className="group rounded-lg border border-zinc-800 bg-zinc-950/30">
    <summary className="flex min-h-11 cursor-pointer list-none items-center gap-2 px-3 py-2 text-sm font-semibold text-zinc-200 marker:hidden">
      <ChevronRight size={16} className="shrink-0 text-zinc-500 transition-transform group-open:rotate-90" />
      {title}
    </summary>
    <div className="border-t border-zinc-800 px-4 py-3">
      {markdown
        ? <div className="text-zinc-400"><MarkdownBody body={markdown} renderMermaid /></div>
        : <p className="text-sm italic text-zinc-600">{emptyLabel}</p>}
    </div>
  </details>
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
  const agentActionProps = {
    taskId: card.id,
    worktreePath: card.worktree?.path,
    hasChimeraSession: card.has_chimera_session,
  }

  return (
    <div className="fixed inset-0 z-40 flex items-end justify-center bg-black/60 md:items-center md:p-6" onClick={onClose}>
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="task-detail-title"
        className="flex h-[calc(100dvh-env(safe-area-inset-top))] w-full max-w-5xl flex-col overflow-hidden rounded-t-xl border border-zinc-700 bg-zinc-900 shadow-2xl md:h-[min(52rem,calc(100dvh-3rem))] md:rounded-xl"
        onClick={(event) => event.stopPropagation()}
      >
        <header className="flex shrink-0 items-start justify-between gap-4 border-b border-zinc-800 px-5 py-4 md:px-6">
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2 text-xs text-zinc-500">
              <span className="font-mono">{card.id}</span>
              {card.status && <span className="rounded-full bg-zinc-800 px-2 py-0.5 text-[10px] text-zinc-400">{card.status}</span>}
              {card.priority && <span className="rounded-full border border-zinc-700 px-2 py-0.5 text-[10px] text-zinc-400">{card.priority} priority</span>}
            </div>
            <h2 id="task-detail-title" className="mt-1.5 text-lg font-semibold leading-snug text-zinc-100">{card.title}</h2>
          </div>
          <Button variant="ghost" size="icon-sm" aria-label="Close task details" onClick={onClose}><X size={16} /></Button>
        </header>

        <div data-task-agent-action="mobile" className="shrink-0 border-b border-zinc-800 px-5 py-3 md:px-6 lg:hidden">
          <TaskAgentAction {...agentActionProps}/>
        </div>

        <div data-task-detail-layout className="grid min-h-0 flex-1 grid-cols-1 overflow-y-auto lg:grid-cols-[minmax(0,1fr)_18rem] lg:overflow-hidden">
          <main aria-label="Task content" className="px-5 py-5 md:px-6 lg:min-h-0 lg:overflow-y-auto">
            <section aria-labelledby="task-description-heading">
              <div className="flex items-center justify-between gap-3">
                <h3 id="task-description-heading" className="text-sm font-semibold text-zinc-200">Description</h3>
                <Button variant="ghost" size="sm" title="Shortcut: e" onClick={onEditDescription}>{isLocal ? "Edit" : "Edit Legato details"}</Button>
              </div>
              {isLocal
                ? card.description_md
                  ? <pre className="mt-3 whitespace-pre-wrap font-sans text-sm leading-6 text-zinc-400">{card.description_md}</pre>
                  : <p className="mt-3 text-sm italic text-zinc-600">No description</p>
                : <div className="mt-3 space-y-3">
                  <DescriptionSection kind="remote" title="Remote ticket details" markdown={card.remote_description_md} emptyLabel="No remote ticket details" />
                  <DescriptionSection kind="local" title="Legato details" markdown={card.local_description_md} emptyLabel="No Legato details" />
                </div>}
            </section>
            <div className="mt-6 border-t border-zinc-800 pt-5">
              <TaskActivityTabs taskId={card.id} notes={card.notes ?? []}/>
            </div>
          </main>

          <aside aria-label="Task properties and actions" className="border-t border-zinc-800 bg-zinc-950/35 px-5 py-5 lg:min-h-0 lg:overflow-y-auto lg:border-l lg:border-t-0">
            <section aria-labelledby="task-properties-heading">
              <h3 id="task-properties-heading" className={sectionHeadingClass}>Properties</h3>
              <dl className="mt-3 grid grid-cols-2 gap-x-4 gap-y-3 lg:grid-cols-1">
                {card.remote_meta?.issue_type && <Property label="Type">{card.remote_meta.issue_type}</Property>}
                {card.remote_meta?.epic_name && <Property label="Epic">{card.remote_meta.epic_name}</Property>}
                {card.remote_meta && (card.remote_meta.labels ?? "") && <Property label="Labels">{card.remote_meta.labels}</Property>}
                {card.remote_meta?.url && <Property label="Provider" wide><a href={card.remote_meta.url} target="_blank" rel="noreferrer" className="text-indigo-400 hover:underline">Open issue</a></Property>}
                {card.worktree && <><Property label="Worktree" wide>{card.worktree.path}</Property><Property label="Branch">{card.worktree.branch}</Property></>}
                {card.code_context && <><Property label="Repository">{card.code_context.repo}</Property>{card.code_context.branch && <Property label="Code branch">{card.code_context.branch}</Property>}</>}
                {card.pr_meta && card.pr_meta.pr_number > 0 && <Property label="Pull request" wide>
                  <a href={card.pr_meta.pr_url} target="_blank" rel="noreferrer" className="text-indigo-400 hover:underline">#{card.pr_meta.pr_number}{card.pr_meta.is_draft && " (draft)"}</a>
                  <div className="mt-1 flex flex-wrap gap-x-2 text-[10px]">
                    {card.pr_meta.state === "MERGED" && <span>merged</span>}
                    {card.pr_meta.review_decision === "APPROVED" && <span className="text-emerald-400">approved</span>}
                    {card.pr_meta.review_decision === "CHANGES_REQUESTED" && <span className="text-red-400">changes requested</span>}
                    {card.pr_meta.check_status === "pass" && <span className="text-emerald-400">CI passing</span>}
                    {card.pr_meta.check_status === "fail" && <span className="text-red-400">CI failing</span>}
                    {card.pr_meta.check_status === "pending" && <span className="text-amber-300">CI pending</span>}
                    {card.pr_meta.comment_count > 0 && <span>{card.pr_meta.comment_count} comments</span>}
                  </div>
                </Property>}
                {card.pr_meta && !card.pr_meta.pr_number && card.pr_meta.sync_outcome && <Property label="PR sync" wide>{card.pr_meta.sync_outcome.replaceAll("_", " ")}{card.pr_meta.sync_error && ` — ${card.pr_meta.sync_error}`}</Property>}
              </dl>
            </section>

            <div data-task-actions className="mt-6 space-y-5 border-t border-zinc-800 pt-5">
              <section aria-labelledby="task-work-heading" className="space-y-2">
                <h3 id="task-work-heading" className={sectionHeadingClass}>Work</h3>
                <div data-task-agent-action="desktop" className="hidden lg:block">
                  <TaskAgentAction {...agentActionProps}/>
                </div>
                {worktreesEnabled && !card.worktree && <Button variant="outline" size="sm" className={sidebarButtonClass} onClick={onCreateWorktree}>Create worktree</Button>}
                <Button variant="outline" size="sm" className={sidebarButtonClass} title="Shortcut: m" onClick={onMove}>Move task</Button>
              </section>

              <section aria-labelledby="task-edit-heading" className="space-y-2">
                <h3 id="task-edit-heading" className={sectionHeadingClass}>Edit</h3>
                {isLocal && <Button variant="outline" size="sm" className={sidebarButtonClass} title="Shortcut: t" onClick={onEditTitle}>Edit title</Button>}
                <Button variant="outline" size="sm" className={sidebarButtonClass} title="Shortcut: e" onClick={onEditDescription}>{isLocal ? "Edit description" : "Edit Legato details"}</Button>
              </section>

              <section aria-labelledby="task-more-heading" className="space-y-1">
                <h3 id="task-more-heading" className={sectionHeadingClass}>More actions</h3>
                <Button variant="ghost" size="sm" className={sidebarButtonClass} title="Shortcut: p" onClick={onLinkPR}>Link pull request</Button>
                {(card.pr_meta?.pr_number ?? 0) > 0 && <Button variant="ghost" size="sm" className={sidebarButtonClass} onClick={onRefreshPR}>Refresh pull request</Button>}
                {card.code_context?.repo && <Button variant="ghost" size="sm" className={sidebarButtonClass} onClick={onRediscoverPR}>Rediscover pull request</Button>}
                <Button variant="ghost" size="sm" className={sidebarButtonClass} title="Shortcut: y" onClick={onCopyDescription}>Copy description</Button>
                <Button variant="ghost" size="sm" className={sidebarButtonClass} title="Shortcut: Shift+y" onClick={onCopyFull}>Copy full task</Button>
                {(card.remote_meta?.url || card.pr_meta?.pr_url) && <Button variant="ghost" size="sm" className={sidebarButtonClass} title="Shortcut: o" onClick={onOpenURL}>Open in browser</Button>}
                {(card.swarm_stats || card.swarm_working_dir) && <Button variant="ghost" size="sm" className={`${sidebarButtonClass} text-red-400 hover:text-red-300`} onClick={onCancelSwarm}>Cancel swarm</Button>}
                <Button variant="ghost" size="sm" className={`${sidebarButtonClass} text-red-400 hover:text-red-300`} title="Shortcut: Shift+d" onClick={onDelete}>Delete task</Button>
              </section>
            </div>
          </aside>
        </div>
      </div>
    </div>
  )
}
