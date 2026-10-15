import type { ReactNode } from "react"
import { X } from "lucide-react"
import type { BoardCard, CardDetail } from "../../lib/board-types"
import { MarkdownBody } from "../MarkdownBody"
import { agentStripLook, formatDuration, priorityTone } from "../../lib/boardCard"
import { TaskAgentAction } from "./TaskAgentAction"

interface BoardTaskPanelProps {
  card: BoardCard
  column: string
  // The task's full record, once it has been read; null until then.
  detail: CardDetail | null
  onClose: () => void
  onOpenDetails: () => void
  onMove: () => void
  onOpenURL: (url: string) => void
}

const providerNames: Record<string, string> = { jira: "Jira", github: "GitHub" }

function Property({ label, children }: { label: string; children: ReactNode }) {
  return <>
    <dt className="text-lw-faint">{label}</dt>
    <dd className="m-0 min-w-0 break-words">{children}</dd>
  </>
}

const secondaryButton = "h-8 flex-1 basis-0 truncate rounded-md border border-lw-line-strong bg-lw-raised px-2 text-[13px] text-lw-text hover:border-[#4a4955]"

// The selected card beside the board, as the TUI's task panel: where it
// stands, the start of its description, and what can be done with it.
export function BoardTaskPanel({ card, column, detail, onClose, onOpenDetails, onMove, onOpenURL }: BoardTaskPanelProps) {
  const ready = detail?.id === card.id ? detail : null
  const look = card.agent_active ? agentStripLook(card) : null
  const spent = formatDuration(card.working_seconds + card.waiting_seconds + card.error_seconds)
  const providerURL = ready?.remote_meta?.url || ""
  const prURL = ready?.pr_meta?.pr_url || ""

  return (
    <aside aria-label="Selected task" className="flex w-[360px] shrink-0 flex-col border-l border-[#222228] bg-[#141417] font-lw">
      <div className="flex flex-col gap-1.5 border-b border-[#222228] px-[18px] py-4">
        <div className="flex items-center justify-between font-lw-mono text-xs text-lw-faint">
          <span className="truncate">{card.provider || "local"} · {card.id}</span>
          <button type="button" aria-label="Close task panel" onClick={onClose} className="-mr-1.5 flex h-7 w-7 items-center justify-center rounded-md text-lw-text-2 hover:bg-lw-raised hover:text-lw-text"><X size={15} /></button>
        </div>
        <h2 className="m-0 text-base font-semibold leading-[22px]">{card.title}</h2>
      </div>

      <dl className="m-0 grid grid-cols-[96px_minmax(0,1fr)] gap-y-2 border-b border-[#222228] px-[18px] py-3.5">
        <Property label="Column">{column}</Property>
        {card.priority && <Property label="Priority"><span className={priorityTone(card.priority)}>{card.priority}</span></Property>}
        {card.issue_type && <Property label="Type">{card.issue_type}</Property>}
        <Property label="Workspace">{card.workspace_name || <span className="text-lw-text-2">Unassigned</span>}</Property>
        <Property label="Agent">{look ? <span className={look.tone}>{look.label}</span> : <span className="text-lw-text-2">None yet</span>}</Property>
        {!card.agent_active && spent && <Property label="Agent time"><span className="text-lw-text-2">{spent}</span></Property>}
        {card.pr_number > 0 && <Property label="Pull request">{prURL ? <a href={prURL} target="_blank" rel="noreferrer" className="font-lw-mono text-lw-link hover:underline">#{card.pr_number}</a> : <span className="font-lw-mono">#{card.pr_number}</span>}</Property>}
        {ready?.worktree && <Property label="Worktree"><span className="font-lw-mono text-xs">{ready.worktree.branch}</span></Property>}
      </dl>

      <div className="min-h-0 flex-1 overflow-y-auto px-[18px] py-3.5 text-lw-text-2">
        {!ready
          ? <p className="m-0 text-lw-faint">Reading the task…</p>
          : ready.description_md
            ? <MarkdownBody body={ready.description_md} />
            : <p className="m-0 italic text-lw-faint">No description</p>}
      </div>

      <div className="flex flex-col gap-2 border-t border-[#222228] px-[18px] py-3.5">
        <TaskAgentAction
          taskId={card.id}
          worktreePath={ready?.worktree?.path}
          rememberedWorkingDir={ready?.code_context?.working_dir}
          resumableAgentKinds={ready?.resumable_agent_kinds}
        />
        <div className="flex gap-2">
          <button type="button" onClick={onOpenDetails} className={secondaryButton}>Open details</button>
          <button type="button" onClick={onMove} className={secondaryButton}>Move</button>
          {providerURL
            ? <button type="button" onClick={() => onOpenURL(providerURL)} className={secondaryButton}>Open in {providerNames[card.provider] ?? "browser"}</button>
            : prURL && <button type="button" onClick={() => onOpenURL(prURL)} className={secondaryButton}>Open PR</button>}
        </div>
      </div>
    </aside>
  )
}
