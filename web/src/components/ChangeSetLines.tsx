import { useEffect, useState } from "react"
import { fetchChangeSetLines, type ChangeSetLine } from "../lib/reeve"
import { cn } from "../lib/utils"

// lineState is a change set's state as its line says it: waiting, or done
// with how many of how many ran.
function lineState(set: ChangeSetLine): { text: string; tone: string } {
  switch (set.state) {
    case "pending":
      return { text: "waiting for your approval", tone: "text-lw-needs" }
    case "queued":
    case "running":
      return { text: "running", tone: "text-lw-live" }
    case "restarting":
      return { text: `done, ${set.ran} of ${set.calls} ran; restarting`, tone: "text-lw-live" }
    case "done":
      return { text: `done, ${set.ran} of ${set.calls} ran`, tone: "text-lw-done" }
    case "failed":
      return { text: `failed, ${set.ran} of ${set.calls} ran`, tone: "text-lw-error" }
    case "denied":
      return { text: "denied", tone: "text-lw-text-2" }
    default:
      return { text: set.state, tone: "text-lw-faint" }
  }
}

// ChangeSetLines shows the Reeve session's change sets as compact lines,
// read again whenever refresh changes (the agent's activity).
export function ChangeSetLines({ baseUrl, taskId, refresh }: { baseUrl: string; taskId: string; refresh?: string }) {
  const [sets, setSets] = useState<ChangeSetLine[]>([])
  useEffect(() => {
    let live = true
    fetchChangeSetLines(baseUrl, taskId).then((s) => { if (live) setSets(s) }).catch(() => {})
    return () => { live = false }
  }, [baseUrl, taskId, refresh])
  if (sets.length === 0) return null
  return (
    <ul aria-label="Change sets" className="flex shrink-0 flex-col gap-0.5 border-b border-[#222228] bg-lw-ground px-4 py-1.5 text-xs">
      {sets.slice(-4).map((set) => {
        const state = lineState(set)
        return (
          <li key={set.id} className="flex min-w-0 items-center gap-1.5">
            <span aria-hidden="true" className="text-[#a78bfa]">◆</span>
            <span className="shrink-0 text-lw-text-2">Change set</span>
            <span className="min-w-0 truncate text-lw-text">{set.reason}</span>
            <span aria-hidden="true" className="text-lw-faint">·</span>
            <span className={cn("shrink-0", state.tone)}>{state.text}</span>
          </li>
        )
      })}
    </ul>
  )
}
