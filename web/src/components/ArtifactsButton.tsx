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
          className="flex h-[30px] shrink-0 items-center gap-1.5 rounded-md border border-lw-line-strong bg-lw-raised px-3 text-xs text-lw-text transition-colors hover:border-[#4a4955]"
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
      className={"relative flex h-[30px] shrink-0 items-center gap-1.5 rounded-md border px-3 text-xs transition-colors " + (fresh > 0
        ? "border-lw-accent bg-lw-selected text-lw-text hover:bg-lw-selected"
        : "border-lw-line-strong bg-lw-raised text-lw-text hover:border-[#4a4955]")}
    >
      <PanelRight size={12} />
      <span className="hidden sm:inline">Artifacts</span>
      <span className="tabular-nums">{count}</span>
      {fresh > 0 && <span aria-hidden="true" className="h-1.5 w-1.5 rounded-full bg-lw-link" />}
    </button>
  )
}
