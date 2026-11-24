import type { BackgroundJob } from "../hooks/useWebSocket"
import { toolDisplayName } from "../lib/mcpToolName"
import type { NativeSubagentState } from "../lib/nativeAgentSync"
import { cn } from "../lib/utils"
import type { DeckTile } from "./TileDeck"

// The newest activity a tile shows.
const tileActivityLimit = 60

// SubagentActivity is what a subagent did, newest last, as its tile shows it.
export function SubagentActivity({ subagent }: { subagent?: NativeSubagentState }) {
  const activities = (subagent?.activities ?? []).slice(-tileActivityLimit)
  if (activities.length === 0) {
    return <p className="m-0 text-xs text-lw-faint">{subagent?.status === "running" || !subagent ? "Waiting for activity…" : "No activity was captured."}</p>
  }
  return (
    <ol className="m-0 flex list-none flex-col gap-1 p-0 text-xs">
      {activities.map((activity) => {
        if (activity.toolName) {
          const rest = activity.text.startsWith(`${activity.toolName}: `) ? activity.text.slice(activity.toolName.length + 2) : ""
          return (
            <li key={activity.key} className="flex min-w-0 items-baseline gap-1.5 text-lw-text-2">
              <span aria-hidden="true" className="font-lw-mono text-[11px] text-lw-faint">›</span>
              <span title={activity.toolName} className="shrink-0 font-lw-mono text-[11.5px] text-lw-text">{toolDisplayName(activity.toolName)}</span>
              <span className="min-w-0 truncate">{rest}</span>
            </li>
          )
        }
        const failed = activity.text.startsWith("Tool error: ")
        return <li key={activity.key} className={cn("whitespace-pre-wrap break-words", failed ? "text-lw-error" : "text-lw-text")}>{activity.text}</li>
      })}
    </ol>
  )
}

// SubagentShells lists the shells a subagent started that still run, which
// it waits on once its own turn has ended.
export function SubagentShells({ shells }: { shells: BackgroundJob[] }) {
  return (
    <ul aria-label="Shells" className="m-0 mt-1.5 flex list-none flex-col gap-0.5 border-t border-lw-line p-0 pt-1.5 text-xs text-lw-text-2">
      {shells.map((shell) => (
        <li key={shell.id} title={`Started ${new Date(shell.started_at).toLocaleTimeString()}`} className="flex min-w-0 items-baseline gap-1.5">
          <span aria-hidden="true" className="font-lw-mono text-[11px] text-lw-faint">$</span>
          <span className="min-w-0 truncate font-lw-mono text-[11.5px]">{shell.label}</span>
        </li>
      ))}
    </ul>
  )
}

// SubagentLink stands in the conversation for a subagent shown as a tile:
// it names it and opens its tile. A background shell's call has one too.
export function SubagentLink({ tile, highlighted, onOpen, onHighlight, kind = "subagent" }: {
  tile: DeckTile
  kind?: "subagent" | "shell"
  highlighted: boolean
  onOpen: () => void
  onHighlight: (id: string | null) => void
}) {
  const tone = tile.state === "running" ? "bg-lw-live" : tile.state === "done" ? "bg-lw-done" : tile.state === "error" ? "bg-lw-error"
    : tile.state === "killed" || tile.state === "interrupted" ? "bg-lw-text-2" : "bg-lw-needs"
  return (
    <button
      type="button"
      aria-label={`Show ${tile.title} in tiles`}
      onClick={onOpen}
      onMouseEnter={() => onHighlight(tile.id)}
      onMouseLeave={() => onHighlight(null)}
      className={cn("mx-4 my-1 flex w-[calc(100%-2rem)] min-w-0 items-center gap-2 rounded-lg border px-3 py-1.5 text-left text-[13px] transition-colors",
        highlighted ? "border-lw-accent bg-lw-accent-soft" : "border-lw-line-strong bg-lw-surface hover:border-lw-accent")}
    >
      <span aria-hidden="true" className={cn("size-1.5 shrink-0 rounded-full", tone)} />
      <span className="shrink-0 font-lw-mono text-[11px] uppercase text-lw-text-2">{kind}</span>
      <span className="min-w-0 flex-1 truncate text-lw-text">{tile.title}</span>
      <span className="shrink-0 text-xs text-lw-link">in tiles<span aria-hidden="true"> →</span></span>
    </button>
  )
}
