import { subagentStatus, type NativeConversationItem, type NativeToolItem } from "./nativeAgentSync"
import type { DeckTile } from "../components/TileDeck"
import { SubagentActivity } from "../components/SubagentParts"

// How the native panel shows its subagents in a TileDeck: one tile for each
// call that spawned one, in the order they were spawned.

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

export function subagentTiles(items: NativeConversationItem[]): DeckTile[] {
  return items.flatMap((item) => isSubagentCall(item.tool) ? [subagentTile(item.tool)] : [])
}

function subagentTile(tool: NativeToolItem): DeckTile {
  const subagent = tool.subagent
  const status = subagentStatus(tool)
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
    startedAt: subagent?.startedAt,
    endedAt: subagent?.endedAt,
    count: tools > 0 ? `${tools} ${tools === 1 ? "tool" : "tools"}` : undefined,
    summary: tool.error || (last ? last.text : status === "spawning" ? "Spawning…" : status === "running" ? "Starting…" : "No live activity captured"),
    detail: prompt ? <p className="m-0 whitespace-pre-wrap rounded-md border border-lw-line bg-[#121216] px-2.5 py-1.5 text-xs text-lw-text-2">{prompt}</p> : undefined,
    body: tool.error ? <p className="m-0 text-xs text-lw-error">{tool.error}</p> : <SubagentActivity subagent={subagent} />,
  }
}
