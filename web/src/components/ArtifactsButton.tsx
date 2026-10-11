import { PanelRight } from "lucide-react"

// What a session's toolbar needs to offer its artifacts.
export interface ArtifactsButtonProps {
  // How many things the session has shown that the panel is the place for.
  count: number
  // How many of them the person has not looked at yet.
  fresh: number
  onToggle: () => void
}

// ArtifactsButton opens and closes the side panel with what an agent showed.
// It lives in the session's own toolbar, and is absent until there is
// something to open.
export function ArtifactsButton({ count, fresh, onToggle }: ArtifactsButtonProps) {
  if (count <= 0) return null
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
