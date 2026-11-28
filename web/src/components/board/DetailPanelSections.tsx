import type { ComponentProps, ReactNode } from "react"
import { ChevronRight, X } from "lucide-react"

import type { CardDetail, PRMetaView } from "../../lib/board-types"
import { handedOffFrom, providerLabel } from "../../lib/providers"
import { MarkdownBody } from "../MarkdownBody"
import { Button } from "../ui/Button"
import { EarlierSessions } from "./EarlierSessions"
import { TaskAgentAction } from "./TaskAgentAction"

// The task detail panel's sections, one each. See DetailPanel.tsx.

export type AgentActionProps = ComponentProps<typeof TaskAgentAction>

export const sectionHeadingClass = "text-[10px] font-semibold uppercase tracking-wider text-zinc-500"
const sidebarButtonClass = "w-full justify-start"
const dangerButtonClass = `${sidebarButtonClass} text-red-400 hover:text-red-300`

function Property({ label, children, wide = false }: { label: string; children: ReactNode; wide?: boolean }) {
  return <div className={wide ? "sm:col-span-2 lg:col-span-1" : ""}>
    <dt className="text-[10px] font-medium uppercase tracking-wide text-zinc-600">{label}</dt>
    <dd className="mt-0.5 break-words text-xs text-zinc-300">{children}</dd>
  </div>
}

// An optional property: shown only when the provider gave a value.
function MetaProperty({ label, value, wide = false }: { label: string; value: string | undefined; wide?: boolean }) {
  if (!value) return null
  return <Property label={label} wide={wide}>{value}</Property>
}

// What the provider says of a task, in its own words: Burnlite's epic is a
// Milestone and its assignee holds the task's claim.
function RemoteProperties({ provider, meta }: { provider: string; meta: Record<string, string> }) {
  const burnlite = provider === "burnlite"
  return <>
    <MetaProperty label="Type" value={meta.issue_type} />
    <MetaProperty label={burnlite ? "Milestone" : "Epic"} value={meta.epic_name} />
    <MetaProperty label={burnlite ? "Claimed by" : "Assignee"} value={meta.assignee} />
    <MetaProperty label="Size" value={meta.size} />
    <MetaProperty label="Uncertainty" value={meta.uncertainty} />
    <MetaProperty label="Blocked by" value={meta.blocked_by} />
    <MetaProperty label="Labels" value={meta.labels} />
    <MetaProperty label="Resolution" value={meta.resolution} wide />
    {meta.url && <Property label="Provider" wide><a href={meta.url} target="_blank" rel="noreferrer" className="text-indigo-400 hover:underline">{provider ? `Open in ${providerLabel(provider)}` : "Open issue"}</a></Property>}
  </>
}

export function DetailHeader({ card, onClose }: { card: CardDetail; onClose: () => void }) {
  return (
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
  )
}

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

// A worker's card for a provider-backed task keeps a copy of the ticket's
// text taken at handoff ("jira-copy", "burnlite-copy"); it says so, by the
// provider's name and the ticket's key.
function remoteDetailsTitle(card: CardDetail): string {
  if (handedOffFrom(card.provider)) return `From ${providerLabel(card.provider)} ${card.remote_id} (read only)`
  return "Remote ticket details"
}

// The description: a local task's own text, or a provider's ticket and
// Legato's details beside it, each in its own section.
export function DescriptionBody({ card }: { card: CardDetail }) {
  if (card.provider) {
    return <div className="mt-3 space-y-3">
      <DescriptionSection kind="remote" title={remoteDetailsTitle(card)} markdown={card.remote_description_md} emptyLabel="No remote ticket details" />
      <DescriptionSection kind="local" title="Legato details" markdown={card.local_description_md} emptyLabel="No Legato details" />
    </div>
  }
  if (!card.description_md) return <p className="mt-3 text-sm italic text-zinc-600">No description</p>
  return <pre className="mt-3 whitespace-pre-wrap font-sans text-sm leading-6 text-zinc-400">{card.description_md}</pre>
}

const checkBadges: Record<string, { label: string; className: string }> = {
  pass: { label: "CI passing", className: "text-emerald-400" },
  fail: { label: "CI failing", className: "text-red-400" },
  pending: { label: "CI pending", className: "text-amber-300" },
}

// A pull request's state, review and checks, as short words.
function PullRequestBadges({ pr }: { pr: PRMetaView }) {
  const check = checkBadges[pr.check_status]
  return (
    <div className="mt-1 flex flex-wrap gap-x-2 text-[10px]">
      {pr.state === "MERGED" && <span>merged</span>}
      {pr.state === "CLOSED" && <span>closed</span>}
      {pr.review_decision === "APPROVED" && <span className="text-emerald-400">approved</span>}
      {pr.review_decision === "CHANGES_REQUESTED" && <span className="text-red-400">changes requested</span>}
      {check && <span className={check.className}>{check.label}</span>}
      {pr.comment_count > 0 && <span>{pr.comment_count} comments</span>}
    </div>
  )
}

function PullRequestList({ pullRequests }: { pullRequests: PRMetaView[] }) {
  const several = pullRequests.length > 1
  return (
    <Property label={several ? "Pull requests" : "Pull request"} wide>
      <ul aria-label="Pull requests" className="m-0 flex list-none flex-col gap-2 p-0">
        {pullRequests.map(pr => <li key={`${pr.repo}#${pr.pr_number}`}>
          <a href={pr.pr_url} target="_blank" rel="noreferrer" className="text-indigo-400 hover:underline">#{pr.pr_number}{pr.is_draft && " (draft)"}</a>
          {several && pr.branch && <span className="ml-2 font-mono text-[10px] text-zinc-500">{pr.branch}</span>}
          <PullRequestBadges pr={pr} />
        </li>)}
      </ul>
    </Property>
  )
}

export function PropertiesSection({ card, pullRequests }: { card: CardDetail; pullRequests: PRMetaView[] }) {
  return (
    <section aria-labelledby="task-properties-heading">
      <h3 id="task-properties-heading" className={sectionHeadingClass}>Properties</h3>
      <dl className="mt-3 grid grid-cols-2 gap-x-4 gap-y-3 lg:grid-cols-1">
        <RemoteProperties provider={card.provider} meta={card.remote_meta ?? {}} />
        {card.worktree && <><Property label="Worktree" wide>{card.worktree.path}</Property><Property label="Branch">{card.worktree.branch}</Property></>}
        {card.code_context && <><Property label="Repository">{card.code_context.repo}</Property><MetaProperty label="Code branch" value={card.code_context.branch} /></>}
        {pullRequests.length > 0
          ? <PullRequestList pullRequests={pullRequests} />
          // How syncing the pull request went, said only while none is linked.
          : card.pr_meta?.sync_outcome && <Property label="PR sync" wide>{card.pr_meta.sync_outcome.replaceAll("_", " ")}{card.pr_meta.sync_error && ` — ${card.pr_meta.sync_error}`}</Property>}
      </dl>
    </section>
  )
}

// What can be done to a task here, which an away task offers none of.
export function WorkAndEditSections({ card, agentActionProps, worktreesEnabled, onCreateWorktree, onMove, onEditTitle, onEditDescription }: {
  card: CardDetail
  agentActionProps: AgentActionProps
  worktreesEnabled: boolean
  onCreateWorktree: () => void
  onMove: () => void
  onEditTitle: () => void
  onEditDescription: () => void
}) {
  const isLocal = !card.provider
  return <>
    <section aria-labelledby="task-work-heading" className="space-y-2">
      <h3 id="task-work-heading" className={sectionHeadingClass}>Work</h3>
      <div data-task-agent-action="desktop" className="hidden lg:block">
        <TaskAgentAction {...agentActionProps}/>
      </div>
      {worktreesEnabled && !card.worktree && <Button variant="outline" size="sm" className={sidebarButtonClass} onClick={onCreateWorktree}>Create worktree</Button>}
      <Button variant="outline" size="sm" className={sidebarButtonClass} title="Shortcut: m" onClick={onMove}>Move task</Button>
    </section>

    <EarlierSessions taskId={card.id} headingClass={sectionHeadingClass} />

    <section aria-labelledby="task-edit-heading" className="space-y-2">
      <h3 id="task-edit-heading" className={sectionHeadingClass}>Edit</h3>
      {isLocal && <Button variant="outline" size="sm" className={sidebarButtonClass} title="Shortcut: t" onClick={onEditTitle}>Edit title</Button>}
      <Button variant="outline" size="sm" className={sidebarButtonClass} title="Shortcut: e" onClick={onEditDescription}>{isLocal ? "Edit description" : "Edit Legato details"}</Button>
    </section>
  </>
}

// Linking a pull request, and finding a linked one's state again: refresh
// reads the linked ones, rediscover looks again in the task's repository.
function PullRequestActions({ canLink, canRefresh, canRediscover, onLinkPR, onRefreshPR, onRediscoverPR }: {
  canLink: boolean
  canRefresh: boolean
  canRediscover: boolean
  onLinkPR: () => void
  onRefreshPR?: () => void
  onRediscoverPR?: () => void
}) {
  return <>
    {canLink && <Button variant="ghost" size="sm" className={sidebarButtonClass} title="Shortcut: p" onClick={onLinkPR}>Link pull request</Button>}
    {canRefresh && <Button variant="ghost" size="sm" className={sidebarButtonClass} onClick={onRefreshPR}>Refresh pull request</Button>}
    {canRediscover && <Button variant="ghost" size="sm" className={sidebarButtonClass} onClick={onRediscoverPR}>Rediscover pull request</Button>}
  </>
}

// The More actions an away task keeps are those that change nothing here.
export function MoreActionsSection({ card, away, hasPullRequests, onLinkPR, onRefreshPR, onRediscoverPR, onCopyDescription, onCopyFull, onOpenURL, onCancelSwarm, onDelete }: {
  card: CardDetail
  away: boolean
  hasPullRequests: boolean
  onLinkPR: () => void
  onRefreshPR?: () => void
  onRediscoverPR?: () => void
  onCopyDescription: () => void
  onCopyFull: () => void
  onOpenURL: () => void
  onCancelSwarm: () => void
  onDelete: () => void
}) {
  const hasURL = Boolean(card.remote_meta?.url || card.pr_meta?.pr_url)
  const hasSwarm = Boolean(card.swarm_stats || card.swarm_working_dir)
  return (
    <section aria-labelledby="task-more-heading" className="space-y-1">
      <h3 id="task-more-heading" className={sectionHeadingClass}>More actions</h3>
      <PullRequestActions canLink={!away} canRefresh={hasPullRequests} canRediscover={Boolean(card.code_context?.repo)} onLinkPR={onLinkPR} onRefreshPR={onRefreshPR} onRediscoverPR={onRediscoverPR} />
      <Button variant="ghost" size="sm" className={sidebarButtonClass} title="Shortcut: y" onClick={onCopyDescription}>Copy description</Button>
      <Button variant="ghost" size="sm" className={sidebarButtonClass} title="Shortcut: Shift+y" onClick={onCopyFull}>Copy full task</Button>
      {hasURL && <Button variant="ghost" size="sm" className={sidebarButtonClass} title="Shortcut: o" onClick={onOpenURL}>Open in browser</Button>}
      {!away && hasSwarm && <Button variant="ghost" size="sm" className={dangerButtonClass} onClick={onCancelSwarm}>Cancel swarm</Button>}
      {!away && <Button variant="ghost" size="sm" className={dangerButtonClass} title="Shortcut: Shift+d" onClick={onDelete}>Delete task</Button>}
    </section>
  )
}
