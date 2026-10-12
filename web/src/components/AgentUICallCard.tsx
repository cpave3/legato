import { useState } from "react"
import { Clapperboard, Maximize2, PanelRight } from "lucide-react"
import { useAgentUIItem } from "../hooks/useAgentUI"
import { useProposeMessage } from "../hooks/useProposeMessage"
import type { LegatoUICall } from "../lib/agentUi"
import { AgentUIItemView } from "./AgentUIItemView"

interface AgentUICallCardProps {
  // A tool call that asked Legato to show something (see legatoUICall).
  call: LegatoUICall
  agentId: string
  // Open an item in the side panel.
  onOpen?: (itemId: string, revision: number) => void
}

// AgentUICallCard is what the conversation draws in place of a tool row when
// an agent showed something: the card itself, or a line that opens an
// artifact. A call that failed is not drawn here; its error belongs in the
// ordinary tool row.
export function AgentUICallCard({ call, agentId, onOpen }: AgentUICallCardProps) {
  if (call.state === "pending") {
    return (
      <div role="status" className="mx-4 my-1.5 flex items-center gap-2 rounded-lg border border-zinc-800 bg-zinc-900/50 px-3 py-2 text-xs text-zinc-500">
        <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-indigo-400" />
        {call.tool === "show_video" ? "Preparing a video…" : call.tool === "show_artifact" ? "Preparing an artifact…" : "Preparing a card…"}
      </div>
    )
  }
  if (call.state !== "shown" || !call.itemId) return null
  const revision = call.revision ?? 0
  if (call.tool === "show_artifact" || call.tool === "show_video") {
    const video = call.tool === "show_video"
    return (
      <div className="mx-4 my-1.5">
        <button
          type="button"
          onClick={() => onOpen?.(call.itemId!, revision)}
          className="flex min-h-11 w-full max-w-md items-center gap-2 rounded-lg border border-indigo-900/70 bg-indigo-950/30 px-3 py-2 text-left text-sm text-indigo-100 transition-colors hover:border-indigo-700 hover:bg-indigo-950/60"
        >
          {video ? <Clapperboard size={16} className="shrink-0 text-indigo-300" /> : <PanelRight size={16} className="shrink-0 text-indigo-300" />}
          <span className="min-w-0 flex-1">
            <span className="block truncate font-medium">{call.title || (video ? "Video" : "Artifact")}</span>
            <span className="block text-xs text-indigo-300/70">{video ? "Video" : "Artifact"} · revision {revision} · open in side panel</span>
          </span>
        </button>
      </div>
    )
  }
  return <ShownCard call={call} itemId={call.itemId} revision={revision} agentId={agentId} onOpen={onOpen} />
}

function ShownCard({ call, itemId, revision, agentId, onOpen }: { call: LegatoUICall; itemId: string; revision: number; agentId: string; onOpen?: AgentUICallCardProps["onOpen"] }) {
  const { item, loading, error } = useAgentUIItem(itemId, revision)
  const proposeMessage = useProposeMessage(agentId)
  const [overflowing, setOverflowing] = useState(false)
  const title = item?.title || call.title

  if (loading) {
    return (
      <div role="status" className="mx-4 my-1.5 h-16 animate-pulse rounded-lg border border-zinc-800 bg-zinc-900/50" aria-label="Loading the card" />
    )
  }
  if (error || !item) {
    // The item was evicted or deleted, or this revision was trimmed. The
    // agent gave the same information as text for exactly this case.
    const fallback = call.fallbackText
    return (
      <div className="mx-4 my-1.5 rounded-lg border border-zinc-800 bg-zinc-900/50 px-3 py-2 text-sm text-zinc-300">
        {fallback && <div className="whitespace-pre-wrap break-words">{fallback}</div>}
        <div className="mt-1 text-xs text-zinc-500">The agent's card is no longer stored{fallback ? "; this is its text version." : "."}</div>
      </div>
    )
  }
  return (
    <div
      role="group"
      aria-label={`Card from the agent${title ? `: ${title}` : ""}`}
      className="mx-4 my-1.5 max-w-2xl overflow-hidden rounded-lg border border-zinc-700 bg-zinc-900"
    >
      {(title || onOpen) && (
        <div className="flex min-h-9 items-center gap-2 border-b border-zinc-800 px-3 py-1">
          <span className="min-w-0 flex-1 truncate text-xs font-medium text-zinc-300">{title}</span>
          {overflowing && <span className="shrink-0 text-[10px] text-zinc-500">scrolls</span>}
          {onOpen && (
            <button
              type="button"
              aria-label="Open in side panel"
              title="Open in side panel"
              onClick={() => onOpen(item.id, revision)}
              className="-mr-1.5 flex h-8 w-8 shrink-0 items-center justify-center rounded text-zinc-500 transition-colors hover:bg-zinc-800 hover:text-zinc-200"
            >
              <Maximize2 size={14} />
            </button>
          )}
        </div>
      )}
      <AgentUIItemView item={item} onOverflow={setOverflowing} onProposeMessage={proposeMessage} />
    </div>
  )
}
