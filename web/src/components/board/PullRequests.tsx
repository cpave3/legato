import { useState, type CSSProperties } from "react"
import { ChevronDown, ChevronUp, Layers } from "lucide-react"
import type { BoardCard as BoardCardType, PRMetaView } from "../../lib/board-types"
import { prStatus, prToneClass } from "../../lib/prStatus"
import { prRange, stackSummary, type SegmentKind, type StackLayer, type StackSummary } from "../../lib/stackSummary"
import { cn } from "../../lib/utils"
import { Mark } from "../PullRequestMarks"
import { PRActionButton } from "./PRMacros"

// A pull request's number, linked to GitHub, struck through when closed;
// someone else's names its repo.
function PRNumber({ pr }: { pr: PRMetaView }) {
  const label = `${pr.pinned ? pr.repo : ""}#${pr.pr_number}`
  const closed = pr.state.toUpperCase() === "CLOSED"
  const tone = cn("font-lw-mono", closed ? "text-lw-text-2 line-through" : "text-lw-link")
  if (!pr.pr_url) return <span className={tone}>{label}</span>
  return <a href={pr.pr_url} target="_blank" rel="noreferrer" onClick={event => event.stopPropagation()} onDoubleClick={event => event.stopPropagation()} className={cn(tone, "hover:underline")}>{label}</a>
}

// One pull request in one line: its number, the word for its state and a
// detail. One that needs the operator is a box on the wash of its state,
// with its one action.
function PullRequestRow({ card, pr, heldBelow = false }: { card: BoardCardType; pr: PRMetaView; heldBelow?: boolean }) {
  const status = prStatus(pr, heldBelow)
  if (!status.needsYou || !status.action) {
    return (
      <div aria-label={`Pull request #${pr.pr_number}`} className="flex flex-wrap items-center gap-x-1.5 pt-0.5 text-xs leading-4 text-lw-text-2">
        <PRNumber pr={pr} />
        <span className={prToneClass(status.tone)}>{status.word}</span>
        {status.detail.map(text => <span key={text} className="contents"><span aria-hidden="true">·</span><span>{text}</span></span>)}
      </div>
    )
  }
  const error = status.tone === "error"
  // Someone else's PR says its checks under its review; the operator's own
  // says nothing more there.
  const below = status.action === "others_pr" ? status.detail.filter(text => !text.includes("comment")).join(" · ") : ""
  const above = status.action === "others_pr" ? status.detail.filter(text => text.includes("comment")) : status.detail
  return (
    <div aria-label={`Pull request #${pr.pr_number}`} data-needs-you={status.tone} className={cn("mt-1 flex flex-col gap-1 rounded-md border px-2 py-[7px] text-xs leading-4", error ? "border-lw-error/25 bg-lw-error-soft" : "border-lw-needs/25 bg-lw-needs-soft")}>
      <div className={cn("flex flex-wrap items-center gap-x-1.5", prToneClass(status.tone))}>
        <span aria-hidden="true">{error ? "✕" : "◆"}</span>
        <PRNumber pr={pr} />
        <span>{[status.word, ...above].join(" · ")}</span>
      </div>
      <div className="flex items-center justify-between gap-2 text-lw-text-2">
        <span>{below}</span>
        <PRActionButton card={card} pr={pr} action={status.action} />
      </div>
    </div>
  )
}

const hatch: CSSProperties = { backgroundImage: "repeating-linear-gradient(135deg, var(--color-lw-needs) 0 2px, transparent 2px 4px)" }

const segmentClass: Record<SegmentKind, string> = {
  merged: "bg-lw-done",
  ready: "bg-lw-live",
  held: "border border-lw-live",
  failing: "bg-lw-error",
  running: "border border-lw-needs",
  draft: "border border-dashed border-lw-line-strong",
  waiting: "border border-lw-line-strong",
  closed: "bg-lw-line",
}

const dotClass: Record<SegmentKind, string> = {
  merged: "bg-lw-done",
  ready: "bg-lw-live",
  held: "border-[1.5px] border-lw-live bg-lw-surface",
  failing: "bg-lw-error",
  running: "border-[1.5px] border-lw-needs bg-lw-surface",
  draft: "border-[1.5px] border-dashed border-lw-line-strong bg-lw-surface",
  waiting: "border-[1.5px] border-lw-line-strong bg-lw-surface",
  closed: "border-[1.5px] border-lw-line-strong bg-lw-surface",
}

// A segment per layer, bottom first, in the colour of its state; the bar
// says its counts in words.
function StackBar({ summary }: { summary: StackSummary }) {
  return (
    <div role="img" aria-label={summary.barLabel} className="flex h-1.5 gap-0.5">
      {summary.layers.map(layer => <span key={`${layer.pr.repo}#${layer.pr.pr_number}`} title={`#${layer.pr.pr_number} ${layer.status.word}`} data-segment={layer.kind} className={cn("flex-1 rounded-[2px]", segmentClass[layer.kind])} style={layer.kind === "running" ? hatch : undefined} />)}
    </div>
  )
}

// The layer that needs the operator, on the wash of its state, with what it
// holds back and the card's one action.
function NeedsYouLayer({ card, layer, holdsBack, compact }: { card: BoardCardType; layer: StackLayer; holdsBack: number; compact: boolean }) {
  const error = layer.status.tone === "error"
  const box = cn("flex flex-col gap-1 rounded-md border px-2 py-[7px] text-xs leading-4", error ? "border-lw-error/25 bg-lw-error-soft" : "border-lw-needs/25 bg-lw-needs-soft")
  const action = layer.status.action && <PRActionButton card={card} pr={layer.pr} action={layer.status.action} />
  const holds = holdsBack > 0 ? `Holds back ${holdsBack} above it` : ""
  if (compact) {
    return (
      <div data-testid="stack-needs-you" data-needs-you={layer.status.tone} className={box}>
        <div className={cn("flex items-center justify-between gap-2", prToneClass(layer.status.tone))}>
          <span>{`#${layer.pr.pr_number}`}{holdsBack > 0 ? ` holds back ${holdsBack}` : ` ${layer.status.word}`}</span>
          {action}
        </div>
      </div>
    )
  }
  return (
    <div data-testid="stack-needs-you" data-needs-you={layer.status.tone} className={box}>
      <div className={cn("flex flex-wrap items-center gap-x-1.5", prToneClass(layer.status.tone))}>
        <span aria-hidden="true">{error ? "✕" : "◆"}</span>
        <PRNumber pr={layer.pr} />
        <span>{[layer.status.word, ...layer.status.detail].join(" · ")}</span>
      </div>
      <div className="flex items-center justify-between gap-2 text-lw-text-2">
        <span>{holds}</span>
        {action}
      </div>
    </div>
  )
}

// The stack opened: each repository as a group headed by its base branch,
// a row per layer on a rail, dashed above a layer that holds it back; a
// fork's layers indented under their shared base.
function StackList({ summary }: { summary: StackSummary }) {
  return (
    <div className="flex flex-col">
      {summary.groups.map((group, index) => (
        <div key={group.repo} role="list" aria-label={`${group.repo} pull requests`} className={cn("flex flex-col", index > 0 && "mt-1.5")}>
          <div className="pl-5 font-lw-mono text-[11px] leading-4 text-lw-text-2">{[group.repo, group.base].filter(Boolean).join(" · ")}</div>
          {group.layers.map((layer, i) => {
            const last = i === group.layers.length - 1
            return (
              <div key={layer.pr.pr_number} role="listitem" aria-label={`Pull request #${layer.pr.pr_number}`} data-held={layer.heldBelow || undefined} data-fork={layer.fork || undefined} className={cn("relative flex h-[22px] items-center gap-2 rounded text-xs", layer.fork ? "pl-[34px]" : "pl-5", layer.kind === "failing" && "bg-lw-error-soft")}>
                <span aria-hidden="true" className={cn("absolute left-[5px] top-0 border-l border-lw-line-strong", last ? "bottom-[11px]" : "bottom-0", layer.heldBelow && "border-dashed")} />
                {layer.fork && <span aria-hidden="true" className="absolute left-[5px] top-0 h-[11px] w-3 rounded-bl-[5px] border-b border-l border-lw-line-strong" />}
                <span aria-hidden="true" className={cn("absolute top-[7px] h-2 w-2 rounded-full", layer.fork ? "left-4" : "left-px", dotClass[layer.kind])} />
                <span className="w-[42px] shrink-0"><PRNumber pr={layer.pr} /></span>
                <span className={cn("min-w-0 flex-1 truncate", prToneClass(layer.status.tone))}>{layer.status.word}</span>
                {layer.pr.comment_count > 0 && <span className="shrink-0 text-lw-text-2">{layer.pr.comment_count === 1 ? "1 comment" : `${layer.pr.comment_count} comments`}</span>}
              </div>
            )
          })}
        </div>
      ))}
    </div>
  )
}

// Which cards' stacks are open: remembered for the session, per card.
const openStacks = new Set<string>()

function PRStack({ card, prs }: { card: BoardCardType; prs: PRMetaView[] }) {
  const [open, setOpen] = useState(() => openStacks.has(card.id))
  const summary = stackSummary(prs)
  if (summary.allMerged) return <div data-testid="pr-stack" className="mt-1"><Mark glyph="✓" tone="text-xs text-lw-done">{`Stack of ${prs.length} PRs, all merged`}</Mark></div>
  const toggle = () => {
    if (open) openStacks.delete(card.id)
    else openStacks.add(card.id)
    setOpen(!open)
  }
  return (
    <div data-testid="pr-stack" className="mt-1 flex flex-col gap-1.5">
      <div className="flex items-center justify-between gap-2 text-xs leading-4">
        <span className="inline-flex items-center gap-1.5 font-medium"><Layers size={13} aria-hidden="true" />{summary.title}</span>
        <span className="font-lw-mono text-[11px] text-lw-text-2">{summary.counts}</span>
      </div>
      {open ? <StackList summary={summary} /> : <StackBar summary={summary} />}
      {summary.needsYou && <NeedsYouLayer card={card} layer={summary.needsYou.layer} holdsBack={summary.needsYou.holdsBack} compact={open} />}
      {!open && summary.mergeable.length > 0 && <Mark glyph="✓" tone="text-xs text-lw-live"><span className="font-lw-mono">{prRange(summary.mergeable)}</span>{" ready to merge, bottom first"}</Mark>}
      {!open && summary.draftsLine && <span className="text-xs text-lw-text-2">{summary.draftsLine}</span>}
      <button type="button" aria-expanded={open} onClick={event => { event.stopPropagation(); toggle() }} onDoubleClick={event => event.stopPropagation()} className="flex h-11 items-center gap-1 self-start text-xs text-lw-text-2 hover:text-lw-text md:h-7">
        {open ? "Show less" : `Show all ${prs.length}`}
        {open ? <ChevronUp size={12} aria-hidden="true" /> : <ChevronDown size={12} aria-hidden="true" />}
      </button>
    </div>
  )
}

// A card's pull requests: one line for one, a folded stack for several.
export function CardPullRequests({ card }: { card: BoardCardType }) {
  const prs = card.pull_requests ?? []
  if (prs.length === 0) return null
  if (prs.length === 1) return <PullRequestRow card={card} pr={prs[0]} />
  return <PRStack card={card} prs={prs} />
}
