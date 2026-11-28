import type { NativeToolItem } from "./nativeAgentSync"

// What a tool call card draws of each kind of call, read from its arguments
// and result. A call may leave any of them out.

// One file of a "patch" tool call: what changed in it, as a unified diff, or
// its whole content when the file is new or gone.
export interface PatchChange {
  path: string
  kind: "add" | "delete" | "update" | string
  diff: string
  movePath?: string
}

function text(value: unknown): string {
  return value as string || ""
}

// The last part of a path, or the path when it has none.
export function fileName(path: string): string {
  return path.split("/").pop() || path
}

// A call's arguments, result and summary, empty where the call has none.
export function toolCallParts(tool: NativeToolItem) {
  return { args: tool.args || {}, result: tool.result || {}, summary: tool.summary || "" }
}

export function editView(tool: NativeToolItem) {
  const { args, result } = toolCallParts(tool)
  const path = text(args.path)
  return {
    path,
    filename: fileName(path),
    oldStr: text(args.old_string),
    newStr: text(args.new_string),
    replacements: result.replacements as number | undefined,
  }
}

export function patchView(tool: NativeToolItem) {
  const { args, result, summary } = toolCallParts(tool)
  return {
    summary,
    changes: (Array.isArray(args.changes) ? args.changes : []) as PatchChange[],
    failure: result.error as string | undefined,
  }
}

export function writeView(tool: NativeToolItem) {
  const { args, result } = toolCallParts(tool)
  const path = text(args.path)
  return {
    path,
    filename: fileName(path),
    content: text(args.content),
    bytesWritten: result.bytes_written as number | undefined,
    created: result.created as boolean | undefined,
  }
}

export function bashView(tool: NativeToolItem) {
  const { args, result } = toolCallParts(tool)
  return {
    command: text(args.command),
    stdout: text(result.stdout),
    stderr: text(result.stderr),
    exitCode: result.exit_code as number | undefined,
  }
}

export function readView(tool: NativeToolItem) {
  const path = text(toolCallParts(tool).args.path)
  return { path, filename: fileName(path) }
}

// The lines of a change as a diff reads: a new file is all additions and a
// deleted one all removals.
export function patchLines(change: PatchChange): string[] {
  const lines = change.diff.replace(/\n$/, "").split("\n")
  if (change.kind === "add") return lines.map((line) => `+${line}`)
  if (change.kind === "delete") return lines.map((line) => `-${line}`)
  return lines
}

export function patchLineClass(line: string): string {
  if (line.startsWith("@@")) return "text-sky-300"
  if (line.startsWith("+")) return "bg-green-900/30 text-green-300"
  if (line.startsWith("-")) return "bg-red-900/30 text-red-300"
  return "text-zinc-400"
}
