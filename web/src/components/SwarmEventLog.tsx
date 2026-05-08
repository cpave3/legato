import { useState } from "react"
import { useServer } from "../hooks/useServer"
import { useToast } from "../hooks/useToast"
import { useSwarmInbox } from "../hooks/useSwarmEvents"
import { drainInbox } from "../lib/swarm"
import { cn } from "../lib/utils"
import { Inbox, Trash2, ChevronDown, ChevronUp } from "lucide-react"

export function SwarmEventLog({ parentId }: { parentId: string }) {
  const { baseUrl } = useServer()
  const { addToast } = useToast()
  const { entries, setEntries, peek } = useSwarmInbox(parentId)
  const [expandedId, setExpandedId] = useState<number | null>(null)
  const [draining, setDraining] = useState(false)

  const handleDrain = async () => {
    setDraining(true)
    try {
      const drained = await drainInbox(baseUrl, parentId)
      setEntries(drained)
      addToast(`Drained ${drained.length} event${drained.length === 1 ? "" : "s"}`, "success")
    } catch (err) {
      addToast(err instanceof Error ? err.message : "Failed to drain inbox", "error")
    } finally {
      setDraining(false)
    }
  }

  const toggleExpand = (id: number) => {
    setExpandedId(expandedId === id ? null : id)
  }

  return (
    <div className="flex flex-col border-t border-zinc-800 bg-zinc-950">
      <div className="flex items-center justify-between px-4 py-2 border-b border-zinc-800">
        <div className="flex items-center gap-2 text-xs font-medium uppercase tracking-wider text-zinc-500">
          <Inbox size={12} />
          Swarm Events
          {entries.length > 0 && (
            <span className="rounded-full bg-zinc-800 px-1.5 py-0.5 text-[10px] text-zinc-400">
              {entries.length}
            </span>
          )}
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={peek}
            className="rounded px-2 py-1 text-[10px] text-zinc-500 transition-colors hover:bg-zinc-800 hover:text-zinc-300"
          >
            Peek
          </button>
          <button
            onClick={handleDrain}
            disabled={draining}
            className="flex items-center gap-1 rounded px-2 py-1 text-[10px] text-red-400 transition-colors hover:bg-red-950 hover:text-red-300 disabled:opacity-40"
          >
            <Trash2 size={10} />
            {draining ? "Draining..." : "Drain"}
          </button>
        </div>
      </div>

      <div className="max-h-48 overflow-y-auto px-4 py-2">
        {entries.length === 0 ? (
          <p className="text-xs text-zinc-600 italic">No unacked events.</p>
        ) : (
          <div className="space-y-1">
            {entries.map((entry) => (
              <button
                key={entry.id}
                onClick={() => toggleExpand(entry.id)}
                className={cn(
                  "flex w-full flex-col rounded border border-zinc-800 bg-zinc-900 px-2.5 py-1.5 text-left transition-colors hover:bg-zinc-800/50"
                )}
              >
                <div className="flex items-center gap-2">
                  <span className="text-[10px] font-mono text-zinc-500">#{entry.id}</span>
                  <span className={cn(
                    "rounded px-1 py-0.5 text-[10px] font-medium uppercase",
                    entry.kind === "progress" && "bg-emerald-950 text-emerald-300",
                    entry.kind === "built" && "bg-indigo-950 text-indigo-300",
                    entry.kind === "question" && "bg-yellow-950 text-yellow-300",
                    entry.kind === "died" && "bg-red-950 text-red-300",
                    entry.kind === "cap_deferred" && "bg-zinc-800 text-zinc-400",
                    entry.kind === "scope_warning" && "bg-amber-950 text-amber-300",
                    entry.kind === "all_idle" && "bg-blue-950 text-blue-300",
                    entry.kind === "finished" && "bg-purple-950 text-purple-300"
                  )}>
                    {entry.kind}
                  </span>
                  {entry.worker && (
                    <span className="text-xs text-zinc-400">{entry.worker}</span>
                  )}
                  {expandedId === entry.id ? (
                    <ChevronUp size={12} className="ml-auto text-zinc-500" />
                  ) : (
                    <ChevronDown size={12} className="ml-auto text-zinc-500" />
                  )}
                </div>
                {expandedId === entry.id && (
                  <div className="mt-1.5 border-t border-zinc-800 pt-1.5 text-xs text-zinc-300 whitespace-pre-wrap">
                    {entry.payload}
                  </div>
                )}
                {expandedId !== entry.id && (
                  <div className="mt-0.5 pl-[calc(1.5rem+2px)] text-xs text-zinc-500 truncate">
                    {entry.payload.slice(0, 80)}{entry.payload.length > 80 ? "…" : ""}
                  </div>
                )}
              </button>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}
