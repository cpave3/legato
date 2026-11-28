import type { NativeToolItem } from "./nativeAgentSync"
import { toolCallParts } from "./toolCallView"

// What a subagent's card says of its spawn call, with the defaults for what
// the call leaves out.
export function subagentCallView(tool: NativeToolItem) {
  const { args, result } = toolCallParts(tool)
  return {
    purpose: tool.subagent?.purpose || String(args.purpose || "Subagent"),
    prompt: String(args.prompt || ""),
    agent: String(args.agent || "default"),
    model: String(args.model || "inherited"),
    mode: String(args.mode || "default"),
    steps: result.steps as number | undefined,
    toolCalls: result.tool_calls_count as number | undefined,
  }
}

// The colour of a subagent's status label.
export function subagentStatusClass(status: string): string {
  if (status === "error") return "text-red-400"
  if (status === "finished") return "text-green-400"
  return "text-indigo-300"
}

// What a card with no activity says instead.
export function subagentNoActivity(status: string): string {
  return status === "spawning" || status === "running" ? "Waiting for activity…" : "No live activity captured"
}

// "2 steps · 1 tool call": what a finished subagent did, empty when its
// result says neither.
export function subagentCounts(steps: number | undefined, toolCalls: number | undefined): string {
  const parts: string[] = []
  if (steps !== undefined) parts.push(`${steps} step${steps === 1 ? "" : "s"}`)
  if (toolCalls !== undefined) parts.push(`${toolCalls} tool call${toolCalls === 1 ? "" : "s"}`)
  return parts.join(" · ")
}
