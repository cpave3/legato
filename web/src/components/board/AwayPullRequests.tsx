import type { PRMetaView } from "../../lib/board-types"
import { prReadiness } from "../../lib/awayPullRequests"
import { cn } from "../../lib/utils"
import { ChecksMark, ReviewMark } from "../PullRequestMarks"

// The PRs of a task away on a worker, in stack order, as the main tracks
// them itself from the branches the worker pushed: each one's number, draft
// or ready, checks and review. A branch whose PR the main has not found yet
// is listed after them. See docs/ai/pr-tracking.md § Away tasks.
export function AwayPullRequests({ prs, branches }: { prs: PRMetaView[]; branches: string[] }) {
  const withPR = new Set(prs.map(pr => pr.branch))
  const waiting = branches.filter(branch => !withPR.has(branch))
  if (prs.length === 0 && waiting.length === 0) return <p className="m-0 text-xs text-lw-faint">No branch pushed yet</p>
  return (
    <ul aria-label="Pull requests" className="m-0 flex list-none flex-col gap-1.5 p-0 text-xs">
      {prs.map(pr => {
        const readiness = prReadiness(pr)
        return (
          <li key={`${pr.repo}#${pr.pr_number}`} className="flex flex-col gap-0.5">
            <span className="flex flex-wrap items-baseline gap-x-2 text-lw-text-2">
              {pr.pr_url
                ? <a href={pr.pr_url} target="_blank" rel="noreferrer" className="font-lw-mono text-[13px] text-lw-link hover:underline">#{pr.pr_number}</a>
                : <span className="font-lw-mono text-[13px]">#{pr.pr_number}</span>}
              <span className={cn(readiness === "ready" ? "text-lw-text" : "text-lw-text-2")}>{readiness}</span>
              <ChecksMark status={pr.check_status} />
              <ReviewMark decision={pr.review_decision} />
            </span>
            <span className="truncate font-lw-mono text-[11px] text-lw-faint">{pr.branch}</span>
          </li>
        )
      })}
      {waiting.map(branch => (
        <li key={branch} className="flex flex-wrap items-baseline gap-x-2">
          <span className="font-lw-mono text-[11px] text-lw-text-2">{branch}</span>
          <span className="text-lw-faint">no PR yet</span>
        </li>
      ))}
    </ul>
  )
}
