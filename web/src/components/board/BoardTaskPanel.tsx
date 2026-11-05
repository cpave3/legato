import type { ReactNode } from "react"
import { X } from "lucide-react"
import type { BoardCard, CardDetail } from "../../lib/board-types"
import { MarkdownBody } from "../MarkdownBody"
import { agentStripLook, formatDuration, priorityTone } from "../../lib/boardCard"
import { TaskAgentAction } from "./TaskAgentAction"
import { providerLabel } from "../../lib/providers"
import { awayLook, awayTime, kindLabel, purposeLabel } from "../../lib/away"
import { AwayPullRequests } from "./AwayPullRequests"
import { cn } from "../../lib/utils"

interface BoardTaskPanelProps {
  card: BoardCard
  column: string
  // The task's full record, once it has been read; null until then.
  detail: CardDetail | null
  onClose: () => void
  onOpenDetails: () => void
  onMove: () => void
  onOpenURL: (url: string) => void
  // Hand the task off to a worker; absent when no worker is paired.
  onHandOff?: () => void
}

function Property({ label, children }: { label: string; children: ReactNode }) {
  return <>
    <dt className="text-lw-faint">{label}</dt>
    <dd className="m-0 min-w-0 break-words">{children}</dd>
  </>
}

const secondaryButton = "h-8 flex-1 basis-0 truncate rounded-md border border-lw-line-strong bg-lw-raised px-2 text-[13px] text-lw-text hover:border-[#4a4955]"

// The selected card beside the board, as the TUI's task panel: where it
// stands, the start of its description, and what can be done with it.
// An away task's panel: where it went and how, the worker's state of it as
// last heard, and a way to open it there. It changes only on the worker.
function AwayProperties({ card }: { card: BoardCard }) {
  const away = card.away!
  const look = awayLook(away)
  return (
    <section aria-label="Away" className="flex flex-col gap-2.5 border-b border-[#222228] px-[18px] py-3.5">
      <div className={cn("flex items-center gap-1.5 rounded-md px-2.5 py-1.5 text-xs", look.tint, look.tone)}>
        <span aria-hidden="true">{look.glyph}</span><span className="min-w-0 truncate">{look.label}</span>
      </div>
      <dl className="m-0 grid grid-cols-[96px_minmax(0,1fr)] gap-y-2">
        <Property label="Worker">{away.worker}</Property>
        <Property label="Purpose">{purposeLabel(away.purpose)}</Property>
        <Property label="Started from">{kindLabel(away.kind)}</Property>
        <Property label="Branch"><span className="font-lw-mono text-xs">{away.branch}</span></Property>
        <Property label="Repository"><span className="font-lw-mono text-xs">{away.repo}</span></Property>
        <Property label="Left">{away.left_column}</Property>
        <Property label="Handed off"><time dateTime={away.handed_off_at} className="text-lw-text-2">{awayTime(away.handed_off_at)}</time></Property>
      </dl>
      <div className="flex flex-col gap-1.5">
        <p className="m-0 text-[11px] font-semibold uppercase tracking-[1px] text-lw-faint">Pull requests</p>
        <AwayPullRequests prs={card.pull_requests ?? []} branches={away.branches ?? []} />
      </div>
      {away.url && <a href={away.url} target="_blank" rel="noreferrer" className="w-fit text-[13px] text-lw-link hover:underline">Open on {away.worker}</a>}
    </section>
  )
}

export function BoardTaskPanel({ card, column, detail, onClose, onOpenDetails, onMove, onOpenURL, onHandOff }: BoardTaskPanelProps) {
  const ready = detail?.id === card.id ? detail : null
  const look = card.agent_active ? agentStripLook(card) : null
  const spent = formatDuration(card.working_seconds + card.waiting_seconds + card.error_seconds)
  const providerURL = ready?.remote_meta?.url || ""
  const pullRequests = card.pull_requests ?? []
  const prURL = pullRequests[0]?.pr_url || ready?.pr_meta?.pr_url || ""

  return (
    <aside aria-label="Selected task" className="flex w-[360px] shrink-0 flex-col border-l border-[#222228] bg-[#141417] font-lw">
      <div className="flex flex-col gap-1.5 border-b border-[#222228] px-[18px] py-4">
        <div className="flex items-center justify-between font-lw-mono text-xs text-lw-faint">
          <span className="truncate">{card.provider || "local"} · {card.id}</span>
          <button type="button" aria-label="Close task panel" onClick={onClose} className="-mr-1.5 flex h-7 w-7 items-center justify-center rounded-md text-lw-text-2 hover:bg-lw-raised hover:text-lw-text"><X size={15} /></button>
        </div>
        <h2 className="m-0 text-base font-semibold leading-[22px]">{card.title}</h2>
      </div>

      {card.away && <AwayProperties card={card} />}

      <dl className="m-0 grid grid-cols-[96px_minmax(0,1fr)] gap-y-2 border-b border-[#222228] px-[18px] py-3.5">
        {!card.away && <Property label="Column">{column}</Property>}
        {card.priority && <Property label="Priority"><span className={priorityTone(card.priority)}>{card.priority}</span></Property>}
        {card.issue_type && <Property label="Type">{card.issue_type}</Property>}
        <Property label="Workspace">{card.workspace_name || <span className="text-lw-text-2">Unassigned</span>}</Property>
        {!card.away && <Property label="Agent">{look ? <span className={look.tone}>{look.label}</span> : <span className="text-lw-text-2">None yet</span>}</Property>}
        {!card.agent_active && spent && <Property label="Agent time"><span className="text-lw-text-2">{spent}</span></Property>}
        {!card.away && pullRequests.length > 0 && <Property label={pullRequests.length === 1 ? "Pull request" : "Pull requests"}>
          <ul aria-label="Pull requests" className="m-0 flex list-none flex-col gap-0.5 p-0">
            {pullRequests.map(pr => <li key={`${pr.repo}#${pr.pr_number}`} className="flex flex-wrap items-baseline gap-x-1.5">
              {pr.pr_url ? <a href={pr.pr_url} target="_blank" rel="noreferrer" className="font-lw-mono text-lw-link hover:underline">#{pr.pr_number}</a> : <span className="font-lw-mono">#{pr.pr_number}</span>}
              {pr.is_draft && <span className="text-xs text-lw-text-2">draft</span>}
              {pr.state === "MERGED" && <span className="text-xs text-lw-text-2">merged</span>}
              {pr.check_status === "fail" && <span className="text-xs text-lw-error">CI failed</span>}
              {pr.review_decision === "CHANGES_REQUESTED" && <span className="text-xs text-lw-error">changes requested</span>}
            </li>)}
          </ul>
        </Property>}
        {ready?.worktree && <Property label="Worktree"><span className="font-lw-mono text-xs">{ready.worktree.branch}</span></Property>}
      </dl>

      <div className="min-h-0 flex-1 overflow-y-auto px-[18px] py-3.5 text-lw-text-2">
        {card.away && <p className="m-0 mb-2 text-[11px] font-semibold uppercase tracking-[1px] text-lw-faint">Description · read-only while away</p>}
        {!ready
          ? <p className="m-0 text-lw-faint">Reading the task…</p>
          : ready.description_md
            ? <MarkdownBody body={ready.description_md} />
            : <p className="m-0 italic text-lw-faint">No description</p>}
      </div>

      <div className="flex flex-col gap-2 border-t border-[#222228] px-[18px] py-3.5">
        {card.away
          ? <p className="m-0 text-xs text-lw-text-2">Changes come from {card.away.worker} when the task comes back. Talk to its agent there.</p>
          : <TaskAgentAction
            taskId={card.id}
            worktreePath={ready?.worktree?.path}
            rememberedWorkingDir={ready?.code_context?.working_dir}
            resumableAgentKinds={ready?.resumable_agent_kinds}
          />}
        <div className="flex gap-2">
          <button type="button" onClick={onOpenDetails} className={secondaryButton}>Open details</button>
          {!card.away && <button type="button" onClick={onMove} className={secondaryButton}>Move</button>}
          {!card.away && onHandOff && <button type="button" onClick={onHandOff} className={secondaryButton}>Hand off…</button>}
          {providerURL
            ? <button type="button" onClick={() => onOpenURL(providerURL)} className={secondaryButton}>Open in {providerLabel(card.provider) || "browser"}</button>
            : prURL && <button type="button" onClick={() => onOpenURL(prURL)} className={secondaryButton}>Open PR</button>}
        </div>
      </div>
    </aside>
  )
}
