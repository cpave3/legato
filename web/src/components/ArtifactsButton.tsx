import { Mic, PanelRight } from "lucide-react"

// What a session's toolbar needs to offer its artifacts.
export interface ArtifactsButtonProps {
  // How many things the session has shown that the panel is the place for.
  count: number
  // How many of them the person has not looked at yet.
  fresh: number
  onToggle: () => void
  // A studio the agent opened for the person to record in. It is a tool,
  // not something the agent made to be read, so it is offered by itself and
  // is not one of the count.
  studio?: { onOpen: () => void }
}

// ArtifactsButton opens and closes the side panel with what an agent showed.
// It lives in the session's own toolbar, and is absent until there is
// something to open. Beside it, when the session has one, is the way into
// the studio.
export function ArtifactsButton({ count, fresh, onToggle, studio }: ArtifactsButtonProps) {
  return (
    <>
      {studio && (
        <button
          type="button"
          onClick={studio.onOpen}
          aria-label="Studio"
          title="The studio, where you record"
          className="flex shrink-0 items-center gap-1 rounded border border-zinc-700 px-2 py-1 text-xs text-zinc-400 transition-colors hover:bg-zinc-800 hover:text-zinc-200"
        >
          <Mic size={12} />
          <span className="hidden sm:inline">Studio</span>
        </button>
      )}
      {count > 0 && <ShownButton count={count} fresh={fresh} onToggle={onToggle} />}
    </>
  )
}

function ShownButton({ count, fresh, onToggle }: Pick<ArtifactsButtonProps, "count" | "fresh" | "onToggle">) {
  return (
    <button
      type="button"
      onClick={onToggle}
      aria-label={`Artifacts (${count})${fresh > 0 ? `, ${fresh} new` : ""}`}
      title="What this agent has shown"
      className={"relative flex shrink-0 items-center gap-1 rounded border px-2 py-1 text-xs transition-colors " + (fresh > 0
        ? "border-indigo-500 bg-indigo-950 text-indigo-100 hover:bg-indigo-900"
        : "border-zinc-700 text-zinc-400 hover:bg-zinc-800 hover:text-zinc-200")}
    >
      <PanelRight size={12} />
      <span className="hidden sm:inline">Artifacts</span>
      <span className="tabular-nums">{count}</span>
      {fresh > 0 && <span aria-hidden="true" className="h-1.5 w-1.5 rounded-full bg-indigo-400" />}
    </button>
  )
}
