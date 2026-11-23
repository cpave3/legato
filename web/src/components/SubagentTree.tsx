import { useState } from "react"
import { ChevronDown } from "lucide-react"
import type { BackgroundJob } from "../hooks/useWebSocket"
import { subagentTree, subagentsLabel, type SubagentNode } from "../lib/agentGlance"
import { cn } from "../lib/utils"
import { Dropdown } from "./ui/Dropdown"

// SubagentsLine is one line on an agent's row, tile or header: how many of
// the subagents it runs in the background (at any depth) are working. A
// click opens their tree. Nothing is drawn without subagents.
export function SubagentsLine({ jobs, className }: { jobs: BackgroundJob[] | undefined; className?: string }) {
  const [open, setOpen] = useState(false)
  const nodes = subagentTree(jobs)
  if (nodes.length === 0) return null
  const working = nodes.filter((node) => node.job.state !== "background").length
  const label = subagentsLabel(jobs)
  return (
    <Dropdown
      kind="dialog"
      align="left"
      minWidth={260}
      open={open}
      onOpenChange={setOpen}
      className={cn("min-w-0", className)}
      trigger={
        <button
          type="button"
          onClick={(event) => {
            event.stopPropagation()
            setOpen(!open)
          }}
          className="flex min-w-0 max-w-full items-center gap-1 rounded text-[11px] text-lw-live hover:text-lw-text"
        >
          <span aria-hidden className={cn("h-1.5 w-1.5 shrink-0 rounded-full", working > 0 ? "bg-lw-live" : "border border-lw-live")} />
          <span className="truncate">{label}</span>
          <ChevronDown size={11} aria-hidden className={cn("shrink-0 transition-transform", open && "rotate-180")} />
        </button>
      }
    >
      <SubagentTree nodes={nodes} />
    </Dropdown>
  )
}

// SubagentTree draws subagents indented under the one that started each,
// with what each does.
export function SubagentTree({ nodes }: { nodes: SubagentNode[] }) {
  return (
    <ul role="tree" aria-label="Sub-agents" className="max-h-72 overflow-y-auto px-2 py-1 text-xs">
      {nodes.map((node) => {
        const state = stateOf(node)
        return (
          <li
            key={node.job.id}
            role="treeitem"
            aria-level={node.depth}
            aria-selected={false}
            title={node.job.label}
            className="flex min-w-0 items-center gap-2 py-1"
            style={{ paddingLeft: `${(node.depth - 1) * 14}px` }}
          >
            <span aria-hidden className={cn("h-1.5 w-1.5 shrink-0 rounded-full", state.dot)} />
            <span className="min-w-0 flex-1 truncate text-lw-text">{description(node.job.label)}</span>
            <span className={cn("shrink-0", state.text)}>{state.label}</span>
          </li>
        )
      })}
    </ul>
  )
}

function stateOf(node: SubagentNode): { label: string; dot: string; text: string } {
  switch (node.job.state) {
    case "waiting":
      return { label: "needs you", dot: "bg-lw-needs", text: "text-lw-needs" }
    case "background": {
      const on = [node.subagents && count(node.subagents, "sub-agent"), node.jobs && count(node.jobs, "job")].filter(Boolean).join(" · ")
      return { label: on ? `waiting on ${on}` : "finishing", dot: "border border-lw-live", text: "text-lw-text-2" }
    }
    default:
      return { label: "working", dot: "bg-lw-live", text: "text-lw-live" }
  }
}

// description is a subagent's label without the agent type Legato puts
// before it ("general-purpose: Implement D1" is "Implement D1").
function description(label: string): string {
  const match = /^[\w.-]+: (.+)$/.exec(label)
  return match ? match[1] : label
}

function count(n: number, noun: string): string {
  return n === 1 ? `1 ${noun}` : `${n} ${noun}s`
}
