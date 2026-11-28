import type { ReviewRunDetails, ReviewRunLayer, ReviewStewardView } from "../lib/workflow"
import { ago } from "../lib/workflowView"
import { cn } from "../lib/utils"

// What the run page says of a review loop (spec LEG-559 stories 11, 12, 20;
// LEG-594): its pass and findings, the reviewer, each pull request's state
// (and, while open, whether it can merge), the steward while it watches,
// and that legato-reviews' approval still stands. See docs/ai/web-ui.md, the
// run page under Workflows.

const stateWords: Record<string, string> = { merged: "Merged", closed: "Closed, not merged" }

const mergeWords: Record<string, string> = {
  CONFLICTING: "Conflicts with its base",
  BEHIND: "Behind its base",
  UNKNOWN: "Mergeability unknown",
  MERGEABLE: "Mergeable",
}

const wakeWords: Record<string, string> = {
  pushed: "a push",
  disputed: "a disputed finding",
  merged: "a merge",
  closed: "a close",
  idle: "idleness",
  ended: "End",
}

function plural(n: number, one: string, many: string): string {
  return `${n} ${n === 1 ? one : many}`
}

// untilWords says how long until a time: "in 7 days", "in 5h", "now".
function untilWords(time: string, now: number): string {
  const seconds = (Date.parse(time) - now) / 1000
  if (!Number.isFinite(seconds) || seconds < 60) return "now"
  const minutes = Math.round(seconds / 60)
  if (minutes < 60) return `in ${minutes}m`
  const hours = Math.round(minutes / 60)
  if (hours < 48) return `in ${hours}h`
  return `in ${Math.round(hours / 24)} days`
}

function Layer({ layer }: { layer: ReviewRunLayer }) {
  const open = !layer.state || layer.state === "open"
  const state = open ? (layer.draft ? "Draft" : "Open") : stateWords[layer.state ?? ""] ?? layer.state
  const merge = open && layer.mergeable ? mergeWords[layer.mergeable] ?? layer.mergeable : ""
  const trouble = layer.mergeable === "CONFLICTING" || layer.mergeable === "BEHIND"
  const onBase = layer.checks_on_base ?? []
  const label = `#${layer.number}`
  return <li className="flex flex-wrap items-baseline gap-x-1.5">
    {layer.url ? <a className="font-lw-mono text-xs text-lw-link underline" href={layer.url} target="_blank" rel="noreferrer">{label}</a> : <span className="font-lw-mono text-xs">{label}</span>}
    {layer.title && <span className="min-w-0 truncate text-lw-text">{layer.title}</span>}
    <span aria-hidden="true">·</span><span>{state}</span>
    {merge && <><span aria-hidden="true">·</span><span className={cn(trouble && "text-lw-needs")}>{merge}</span></>}
    {open && onBase.length > 0 && <><span aria-hidden="true">·</span><span>Also failing on its base: {onBase.join(", ")}</span></>}
  </li>
}

function Steward({ steward, now }: { steward: ReviewStewardView; now: number }) {
  const wake = steward.last_wake ? wakeWords[steward.last_wake] ?? steward.last_wake : ""
  const when = steward.last_wake_at ? ago(steward.last_wake_at, now) : ""
  return <div aria-label="Steward" className="space-y-0.5">
    <p className="text-lw-text">{steward.held ? "Held: nothing is sent and nothing is reviewed until Watch again; a merge, a close or idleness still ends it." : "Watching the approved pull requests until they merge."}</p>
    <p>
      {wake && <>Last woke: {wake}{when && ` · ${when === "now" ? "just now" : `${when} ago`}`} · </>}
      Re-entries: {steward.reentries} of {steward.max_reentries}
      {steward.idle_deadline && <> · Ends idle {untilWords(steward.idle_deadline, now)}</>}
    </p>
  </div>
}

export function ReviewLoopFacts({ details, now }: { details: ReviewRunDetails; now: number }) {
  const findings = details.findings
  const layers = details.layers ?? []
  return <div className="mt-1 space-y-1.5">
    <p>
      Pass {details.pass}
      {findings && <> · {plural(findings.total, "finding", "findings")}, {findings.blocking} blocking, {findings.open} open</>}
      {details.reviewer && <> · Reviewer: {details.reviewer}{details.reviewer_model && ` (${details.reviewer_model})`}</>}
    </p>
    {details.steward && <Steward steward={details.steward} now={now} />}
    {details.approval_stands && <p role="note" className="text-lw-text">{details.approval_stands}</p>}
    {layers.length > 0 && <ul aria-label="Pull requests" className="space-y-0.5">{layers.map(layer => <Layer key={layer.number} layer={layer} />)}</ul>}
  </div>
}
