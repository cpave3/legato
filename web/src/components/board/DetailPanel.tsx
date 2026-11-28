import { useRef, type KeyboardEvent } from "react"
import { useModalLayer } from "../ui/useModalLayer"

import { taskPullRequests, type AwayOn, type CardDetail } from "../../lib/board-types"
import { Button } from "../ui/Button"
import { TaskActivityTabs } from "./TaskActivityTabs"
import { DescriptionBody, DetailBanner, DetailHeader, MoreActionsSection, PropertiesSection, WorkAndEditSections } from "./DetailPanelSections"

interface DetailPanelProps {
  card: CardDetail
  suspended?: boolean
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
  // Set while the task is away on a worker: it is shown read-only.
  away?: AwayOn
}

// A nested agent dialog handles its own Escape before this bubbles.
function keepNestedEscape(event: KeyboardEvent<HTMLDivElement>) {
  if (event.key === "Escape" && (event.target as HTMLElement).closest('[role="dialog"]') !== event.currentTarget) event.stopPropagation()
}

export function DetailPanel({
  card,
  suspended = false,
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
  away,
}: DetailPanelProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  useModalLayer({ open: !suspended, onClose, containerRef })
  const isLocal = !card.provider
  const pullRequests = taskPullRequests(card)
  const agentActionProps = {
    taskId: card.id,
    worktreePath: card.worktree?.path,
    rememberedWorkingDir: card.code_context?.working_dir,
    resumableAgentKinds: card.resumable_agent_kinds,
  }

  return (
    <div className="fixed inset-0 z-40 flex items-end justify-center bg-black/60 md:items-center md:p-6" onClick={onClose}>
      <div
        ref={containerRef}
        tabIndex={-1}
        role="dialog"
        aria-modal="true"
        aria-labelledby="task-detail-title"
        className="flex h-[calc(100dvh-env(safe-area-inset-top))] w-full max-w-5xl flex-col overflow-hidden rounded-t-xl border border-zinc-700 bg-zinc-900 shadow-2xl md:h-[min(52rem,calc(100dvh-3rem))] md:rounded-xl"
        onClick={(event) => event.stopPropagation()}
        onKeyDown={keepNestedEscape}
      >
        <DetailHeader card={card} onClose={onClose} />
        <DetailBanner away={away} agentActionProps={agentActionProps} />

        <div data-task-detail-layout className="grid min-h-0 flex-1 grid-cols-1 overflow-y-auto lg:grid-cols-[minmax(0,1fr)_18rem] lg:overflow-hidden">
          <main aria-label="Task content" className="px-5 py-5 md:px-6 lg:min-h-0 lg:overflow-y-auto">
            <section aria-labelledby="task-description-heading">
              <div className="flex items-center justify-between gap-3">
                <h3 id="task-description-heading" className="text-sm font-semibold text-zinc-200">Description{away && <span className="ml-2 text-xs font-normal text-zinc-500">read-only while away</span>}</h3>
                {!away && <Button variant="ghost" size="sm" title="Shortcut: e" onClick={onEditDescription}>{isLocal ? "Edit" : "Edit Legato details"}</Button>}
              </div>
              <DescriptionBody card={card} />
            </section>
            <div className="mt-6 border-t border-zinc-800 pt-5">
              <TaskActivityTabs taskId={card.id} notes={card.notes ?? []}/>
            </div>
          </main>

          <aside aria-label="Task properties and actions" className="border-t border-zinc-800 bg-zinc-950/35 px-5 py-5 lg:min-h-0 lg:overflow-y-auto lg:border-l lg:border-t-0">
            <PropertiesSection card={card} pullRequests={pullRequests} />

            <div data-task-actions className="mt-6 space-y-5 border-t border-zinc-800 pt-5">
              {!away && <WorkAndEditSections card={card} agentActionProps={agentActionProps} worktreesEnabled={worktreesEnabled} onCreateWorktree={onCreateWorktree} onMove={onMove} onEditTitle={onEditTitle} onEditDescription={onEditDescription} />}
              <MoreActionsSection card={card} away={Boolean(away)} hasPullRequests={pullRequests.length > 0} onLinkPR={onLinkPR} onRefreshPR={onRefreshPR} onRediscoverPR={onRediscoverPR} onCopyDescription={onCopyDescription} onCopyFull={onCopyFull} onOpenURL={onOpenURL} onCancelSwarm={onCancelSwarm} onDelete={onDelete} />
            </div>
          </aside>
        </div>
      </div>
    </div>
  )
}
