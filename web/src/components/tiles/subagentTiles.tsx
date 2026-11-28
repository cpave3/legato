import { subagentStatus, type NativeConversationItem, type NativeToolItem } from "./nativeAgentSync"
import type { DeckTile } from "../components/TileDeck"
import { SubagentActivity, SubagentShells } from "../components/SubagentParts"
import type { BackgroundJob } from "../hooks/useWebSocket"
import { subagentTree, subagentWaitsOn, type SubagentNode } from "./agentGlance"

// How the native panel shows its subagents in a TileDeck: one tile for each
// call that spawned one, in the order they were spawned.
//
// The conversation says a subagent finished when its own turn ends, even
// while shells it started still run. The agent's background jobs say what
// is still live, so a subagent listed there is still live: working,
// needing the operator, or waiting on its shells.

// SubagentLive is what a subagent call shows: its status, the words for it,
// and its entry among the agent's background jobs when Legato lists it.
export interface SubagentLive {
  status: ReturnType<typeof subagentStatus> | "waiting" | "background"
  label: string
  node?: SubagentNode
}

export function subagentLive(tool: NativeToolItem, nodes: SubagentNode[]): SubagentLive {
  const status = subagentStatus(tool)
  const id = tool.subagent?.subagentId
  const node = id ? nodes.find((each) => each.job.id === id) : undefined
  if (!node || (status !== "running" && status !== "finished")) return { status, label: status }
  switch (node.job.state) {
    case "background":
      return { status: "background", label: subagentWaitsOn(node), node }
    case "waiting":
      return { status: "waiting", label: "needs you", node }
    default:
      return { status: "running", label: "running", node }
  }
}

// The view the person chose for subagents, remembered for this browser.
export type SubagentView = "inline" | "tiles"

const viewKey = "legato:subagent-view"

export function savedSubagentView(): SubagentView {
  try {
    return localStorage.getItem(viewKey) === "tiles" ? "tiles" : "inline"
  } catch {
    return "inline"
  }
}

export function saveSubagentView(view: SubagentView) {
  try {
    localStorage.setItem(viewKey, view)
  } catch {
    // The choice holds for this page only.
  }
}

function isSubagentCall(tool: NativeToolItem | undefined): tool is NativeToolItem {
  return Boolean(tool && (tool.toolName === "spawn_agent" || tool.subagent))
}

export function subagentTiles(items: NativeConversationItem[], jobs?: BackgroundJob[]): DeckTile[] {
  const nodes = subagentTree(jobs)
  return items.flatMap((item) => isSubagentCall(item.tool) ? [subagentTile(item.tool, subagentLive(item.tool, nodes))] : [])
}

function subagentTile(tool: NativeToolItem, live: SubagentLive): DeckTile {
  const subagent = tool.subagent
  const { status, node } = live
  const args = tool.args || {}
  const purpose = subagent?.purpose || String(args.purpose || "Subagent")
  const prompt = String(args.prompt || "")
  const activities = subagent?.activities ?? []
  const tools = activities.filter((activity) => activity.toolName).length
  const meta = [args.agent, args.model].filter((part): part is string => typeof part === "string" && part !== "").join(" · ")
  const last = activities[activities.length - 1]

  return {
    id: tool.key,
    title: purpose,
    meta: meta || undefined,
    state: status === "finished" ? "done" : status,
    stateLabel: status === "background" ? live.label : undefined,
    startedAt: subagent?.startedAt,
    endedAt: node ? undefined : subagent?.endedAt,
    count: tools > 0 ? `${tools} ${tools === 1 ? "tool" : "tools"}` : undefined,
    summary: tool.error || (status === "background" ? live.label : last ? last.text : status === "spawning" ? "Spawning…" : status === "running" ? "Starting…" : "No live activity captured"),
    detail: prompt ? <p className="m-0 whitespace-pre-wrap rounded-md border border-lw-line bg-[var(--theme-canvas,#121216)] px-2.5 py-1.5 text-xs text-lw-text-2">{prompt}</p> : undefined,
    body: tool.error
      ? <p className="m-0 text-xs text-lw-error">{tool.error}</p>
      // The shells last: a closed tile keeps its end in view.
      : <><SubagentActivity subagent={subagent} />{node && node.shells.length > 0 && <SubagentShells shells={node.shells} />}</>,
  }
}
