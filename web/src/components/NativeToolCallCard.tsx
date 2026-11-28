import { memo, useState, type ReactNode } from "react"
import { Plug } from "lucide-react"
import { ReadImageRow } from "./ReadImageRow"
import { isImagePath } from "../lib/agentImage"
import { mcpToolName } from "../lib/mcpToolName"
import type { NativeToolItem as ToolCallItem } from "../lib/nativeAgentSync"
import { bashView, editView, patchLineClass, patchLines, patchView, readView, toolCallParts, writeView } from "../lib/toolCallView"

// What every kind of tool call card is given: the call, whether it shares a
// border with others, and whether it is open.
interface ToolCardProps {
  agentId: string
  tool: ToolCallItem
  grouped: boolean
  expanded: boolean
  onToggle: () => void
}

// One tool call in the conversation, drawn by the card for its kind of tool.
export const ToolCallCard = memo(function ToolCallCard({ agentId, tool, grouped }: { agentId: string; tool: ToolCallItem; grouped: boolean }) {
  // An edit opens its diff unasked only when it stands alone; in a group every
  // call starts as one line. A click overrides either default.
  const [override, setExpanded] = useState<boolean | null>(null)
  const expanded = override ?? ((tool.toolName === "edit" || tool.toolName === "patch") && !grouped)
  return toolCard({ agentId, tool, grouped, expanded, onToggle: () => setExpanded(!expanded) })
})

// The card for a call's kind of tool.
function toolCard(props: ToolCardProps): ReactNode {
  switch (props.tool.toolName) {
    case "edit": return <EditToolCard {...props} />
    case "patch": return <PatchToolCard {...props} />
    case "write": return <WriteToolCard {...props} />
    case "bash": return <BashToolCard {...props} />
    case "read": return <ReadToolCard {...props} />
    default: return <OtherToolCard {...props} />
  }
}

// The line a call is drawn as, which opens to what it did. The header is the
// call's own badge and details; its time and the arrow follow.
function ToolCardFrame({ grouped, expanded, onToggle, durationMs, title, header, bodyClass = "border-t border-zinc-800 px-3 py-2", children }: {
  grouped: boolean
  expanded: boolean
  onToggle: () => void
  durationMs?: number
  title?: string
  header: ReactNode
  bodyClass?: string
  children: ReactNode
}) {
  return (
    <div>
      <button
        className={`flex w-full items-center gap-2 px-3 text-left ${grouped ? "py-1" : "py-1.5"}`}
        title={title}
        onClick={onToggle}
      >
        {header}
        {durationMs !== undefined && <span className="text-xs text-zinc-600">{durationMs}ms</span>}
        <span className="ml-auto text-xs text-zinc-500">{expanded ? "▼" : "▶"}</span>
      </button>
      {expanded && (
        <div className={bodyClass}>
          {children}
        </div>
      )}
    </div>
  )
}

// An edit, as the diff of what it replaced.
function EditToolCard({ tool, grouped, expanded, onToggle }: ToolCardProps) {
  const { path, filename, oldStr, newStr, replacements } = editView(tool)
  return (
    <ToolCardFrame grouped={grouped} expanded={expanded} onToggle={onToggle} durationMs={tool.durationMs} header={<>
      <span className="rounded bg-[var(--theme-selection,color-mix(in_oklab,var(--color-purple-700)_60%,transparent))] px-1.5 py-0.5 font-mono text-xs text-[var(--theme-text,white)]">edit</span>
      <span className="text-xs text-zinc-300 font-mono truncate">{filename}</span>
      {replacements !== undefined && (
        <span className="shrink-0 text-xs text-zinc-500">({replacements} replacement{replacements !== 1 ? "s" : ""})</span>
      )}
    </>}>
      {path !== filename && <div className="text-xs text-zinc-500 mb-1">{path}</div>}
      <div className="font-mono text-xs whitespace-pre">
        {oldStr.split("\n").map((line, i) => (
          <div key={`old-${i}`} className="bg-red-900/30 text-red-300 px-2">
            - {line}
          </div>
        ))}
        {newStr.split("\n").map((line, i) => (
          <div key={`new-${i}`} className="bg-green-900/30 text-green-300 px-2">
            + {line}
          </div>
        ))}
      </div>
    </ToolCardFrame>
  )
}

// A change to one or more files, as the diff of each.
function PatchToolCard({ tool, grouped, expanded, onToggle }: ToolCardProps) {
  const { summary, changes, failure } = patchView(tool)
  return (
    <ToolCardFrame grouped={grouped} expanded={expanded} onToggle={onToggle} durationMs={tool.durationMs} bodyClass="space-y-2 border-t border-zinc-800 px-3 py-2" header={<>
      <span className="rounded bg-[var(--theme-selection,color-mix(in_oklab,var(--color-purple-700)_60%,transparent))] px-1.5 py-0.5 font-mono text-xs text-[var(--theme-text,white)]">patch</span>
      <span className="text-xs text-zinc-300 font-mono truncate">{summary || changes.map((change) => change.path.split("/").pop()).join(", ")}</span>
      {failure && <span className="shrink-0 text-xs text-red-400">not applied</span>}
    </>}>
      {failure && <div className="text-xs text-red-400">{failure}</div>}
      {changes.map((change) => (
        <div key={change.path}>
          <div className="mb-1 flex min-w-0 flex-wrap gap-x-2 text-xs text-zinc-500">
            <span className="uppercase tracking-wide">{change.kind}</span>
            <span className="min-w-0 break-all">{change.path}</span>
            {change.movePath && <span className="min-w-0 break-all">→ {change.movePath}</span>}
          </div>
          <div className="font-mono text-xs whitespace-pre overflow-x-auto">
            {patchLines(change).map((line, i) => (
              <div key={i} className={`px-2 ${patchLineClass(line)}`}>{line}</div>
            ))}
          </div>
        </div>
      ))}
    </ToolCardFrame>
  )
}

// A file written whole: its path, and what went into it.
function WriteToolCard({ tool, grouped, expanded, onToggle }: ToolCardProps) {
  const { path, filename, content, bytesWritten, created } = writeView(tool)
  return (
    <ToolCardFrame grouped={grouped} expanded={expanded} onToggle={onToggle} durationMs={tool.durationMs} header={<>
      <span className="rounded bg-[var(--theme-selection,color-mix(in_oklab,var(--color-green-700)_60%,transparent))] px-1.5 py-0.5 font-mono text-xs text-[var(--theme-text,white)]">write</span>
      <span className="text-xs text-zinc-300 font-mono truncate">{filename}</span>
      {created && <span className="text-xs text-green-400">created</span>}
      {bytesWritten !== undefined && <span className="text-xs text-zinc-500">{bytesWritten}B</span>}
    </>}>
      {path !== filename && <div className="text-xs text-zinc-500 mb-1">{path}</div>}
      <pre className="font-mono text-xs text-zinc-300 whitespace-pre-wrap max-h-60 overflow-y-auto">{content}</pre>
    </ToolCardFrame>
  )
}

// A shell command, and what it printed.
function BashToolCard({ tool, grouped, expanded, onToggle }: ToolCardProps) {
  const { command, stdout, stderr, exitCode } = bashView(tool)
  return (
    <ToolCardFrame grouped={grouped} expanded={expanded} onToggle={onToggle} durationMs={tool.durationMs} header={<>
      <span className="rounded bg-[var(--theme-selection,var(--color-zinc-600))] px-1.5 py-0.5 font-mono text-xs text-[var(--theme-text,white)]">bash</span>
      <span className="text-xs text-zinc-300 font-mono truncate">{command.slice(0, 80)}{command.length > 80 ? "…" : ""}</span>
      {exitCode !== undefined && (
        <span className={`shrink-0 text-xs ${exitCode === 0 ? "text-green-500" : "text-red-400"}`}>exit {exitCode}</span>
      )}
    </>}>
      <div className="font-mono text-xs text-zinc-500 mb-1">$ {command}</div>
      {stdout && <pre className="font-mono text-xs text-zinc-300 whitespace-pre-wrap max-h-60 overflow-y-auto">{stdout}</pre>}
      {stderr && <pre className="font-mono text-xs text-red-400 whitespace-pre-wrap max-h-40 overflow-y-auto">{stderr}</pre>}
    </ToolCardFrame>
  )
}

// A file read, as one line with its name.
function ReadToolCard({ agentId, tool, grouped }: ToolCardProps) {
  const { path, filename } = readView(tool)
  // The agent saw an image; the person can open it from the row.
  if (isImagePath(path)) return <ReadImageRow agentId={agentId} path={path} grouped={grouped} />
  return (
    <div className={`text-xs text-zinc-400 ${grouped ? "px-3 py-1" : "px-1 py-0.5"}`}>
      <div className="flex items-center gap-2">
        <span className="rounded bg-[var(--theme-selection,color-mix(in_oklab,var(--color-blue-700)_40%,transparent))] px-1.5 py-0.5 font-mono text-[var(--theme-text,white)]">read</span>
        <span className="font-mono truncate">{filename}</span>
      </div>
      {tool.skillName && (
        <div className="ml-12 mt-0.5 text-indigo-300">📚 skill: {tool.skillName}</div>
      )}
    </div>
  )
}

// Any other tool: its name and summary, opening to its arguments and result.
function OtherToolCard({ tool, grouped, expanded, onToggle }: ToolCardProps) {
  const name = tool.toolName
  const { args, result, summary } = toolCallParts(tool)
  // An MCP server's tool: the server as the badge, with a plug to mark it
  // as MCP, then the tool in words. The raw name is in the tooltip and the
  // expanded view.
  const mcp = mcpToolName(name)
  return (
    <ToolCardFrame grouped={grouped} expanded={expanded} onToggle={onToggle} durationMs={tool.durationMs} title={mcp ? name : undefined} header={<>
      {mcp ? (
        <>
          <span data-mcp="" className="inline-flex shrink-0 items-center gap-1 rounded bg-[var(--theme-selection,color-mix(in_oklab,var(--color-cyan-800)_55%,transparent))] px-1.5 py-0.5 text-xs text-[var(--theme-text,white)]">
            <Plug size={11} aria-label="MCP" className="shrink-0" />
            {mcp.serverLabel}
          </span>
          <span className="shrink-0 text-xs font-medium text-zinc-200">{mcp.toolLabel}</span>
        </>
      ) : (
        <span className="rounded bg-[var(--theme-selection,var(--color-zinc-600))] px-1.5 py-0.5 font-mono text-xs text-[var(--theme-text,white)]">{name}</span>
      )}
      <span className={`text-xs truncate ${mcp ? "text-zinc-400" : "text-zinc-300"}`}>{summary}</span>
    </>}>
      {mcp && (
        <div className="mb-1 text-xs text-zinc-500">
          MCP tool <span className="font-mono text-zinc-400 break-all">{name}</span>
        </div>
      )}
      {Object.keys(args).length > 0 && (
        <>
          <div className="text-xs text-zinc-500 mb-1">Args</div>
          <pre className="font-mono text-xs text-zinc-300 whitespace-pre-wrap max-h-40 overflow-y-auto">{JSON.stringify(args, null, 2)}</pre>
        </>
      )}
      {Object.keys(result).length > 0 && (
        <>
          <div className="text-xs text-zinc-500 mt-2 mb-1">Result</div>
          <pre className="font-mono text-xs text-zinc-300 whitespace-pre-wrap max-h-40 overflow-y-auto">{JSON.stringify(result, null, 2)}</pre>
        </>
      )}
    </ToolCardFrame>
  )
}
