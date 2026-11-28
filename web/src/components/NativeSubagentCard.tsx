import { memo, useState } from "react"
import type { SubagentNode } from "../lib/agentGlance"
import type { NativeToolItem as ToolCallItem } from "../lib/nativeAgentSync"
import { subagentCallView, subagentCounts, subagentNoActivity, subagentStatusClass } from "../lib/subagentCard"
import { SubagentShells } from "./tiles/SubagentParts"
import { subagentLive, type SubagentLive } from "./tiles/subagentTiles"

// SubagentCard is a subagent's call in the conversation. While Legato lists
// the subagent among the agent's background jobs it is live, whatever the
// conversation says: waiting on its shells once its own turn has ended.
export const SubagentCard = memo(function SubagentCard({ tool, nodes }: { tool: ToolCallItem; nodes: SubagentNode[] }) {
  const [expanded, setExpanded] = useState(true)
  const view = subagentCallView(tool)
  const live = subagentLive(tool, nodes)

  return (
    <div data-testid={`subagent-card-${tool.key}`} className="mx-4 my-2 overflow-hidden rounded-lg border border-indigo-700/60 bg-indigo-950/20">
      <button className="flex w-full items-center gap-2 px-3 py-2 text-left" onClick={() => setExpanded(!expanded)}>
        <span className="rounded bg-[var(--theme-selection,color-mix(in_oklab,var(--color-indigo-600)_70%,transparent))] px-1.5 py-0.5 font-mono text-xs text-[var(--theme-text,white)]">subagent</span>
        <span className="truncate text-sm font-medium text-indigo-100">{view.purpose}</span>
        <span className={`ml-auto text-xs ${subagentStatusClass(live.status)}`}>{live.label}</span>
        <span className="text-xs text-zinc-500">{expanded ? "▼" : "▶"}</span>
      </button>
      {expanded && <SubagentDetails tool={tool} view={view} live={live} />}
    </div>
  )
})

// The opened card: how the subagent was started, what it has done, and its
// shells.
function SubagentDetails({ tool, view, live }: { tool: ToolCallItem; view: ReturnType<typeof subagentCallView>; live: SubagentLive }) {
  const { status, node } = live
  const activities = tool.subagent?.activities
  const counts = subagentCounts(view.steps, view.toolCalls)
  return (
    <div className="border-t border-indigo-900/60 px-3 py-2">
      <div className="mb-2 flex flex-wrap gap-1.5 text-[11px]">
        <span className="rounded bg-zinc-800 px-1.5 py-0.5 text-zinc-300">agent <span className="font-mono text-zinc-100">{view.agent}</span></span>
        <span className="rounded bg-zinc-800 px-1.5 py-0.5 text-zinc-300">model <span className="font-mono text-zinc-100">{view.model}</span></span>
        <span className="rounded bg-zinc-800 px-1.5 py-0.5 text-zinc-300">mode <span className="font-mono text-zinc-100">{view.mode}</span></span>
      </div>
      {view.prompt && <div className="mb-2 whitespace-pre-wrap rounded bg-zinc-900/70 px-2 py-1.5 text-xs text-zinc-300">{view.prompt}</div>}
      {tool.error && <div className="text-xs text-red-300">{tool.error}</div>}
      {activities?.length ? (
        <div className="space-y-1 border-l border-indigo-700/50 pl-3">
          {activities.map((activity) => <div key={activity.key} className="text-xs text-zinc-300">{activity.text}</div>)}
        </div>
      ) : (
        <div className="text-xs text-zinc-500">
          {subagentNoActivity(status)}
        </div>
      )}
      {node && node.shells.length > 0 && <SubagentShells shells={node.shells} />}
      {counts && (
        <div className="mt-2 text-[11px] text-zinc-500">
          {counts}
        </div>
      )}
    </div>
  )
}
