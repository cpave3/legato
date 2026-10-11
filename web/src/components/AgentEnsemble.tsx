import { useLayoutEffect, useRef, type ReactNode } from "react"
import { LayoutList, Loader2, Plus } from "lucide-react"
import type { AgentInfo } from "../hooks/useWebSocket"
import { useAgentGlance } from "../hooks/useAgentGlance"
import {
  contextShare,
  ensembleSection,
  glanceItems,
  orderForEnsemble,
  plainText,
  type AgentConversation,
  type EnsembleSection,
  type GlanceItem,
  type ToolState,
} from "../lib/agentGlance"
import { cn } from "../lib/utils"
import { AgentKindBadge } from "./AgentKindBadge"

interface AgentEnsembleProps {
  // The running agents, in any order.
  agents: AgentInfo[]
  onOpen: (taskId: string) => void
  onSpawn: () => void
  onShowList: () => void
}

const sections: Array<{ section: EnsembleSection; count: (n: number) => string; tone: string }> = [
  { section: "needs-you", count: (n) => `${n} ${n === 1 ? "needs" : "need"} you`, tone: "text-yellow-400" },
  { section: "running", count: (n) => `${n} running`, tone: "text-emerald-400" },
  { section: "background", count: (n) => `${n} in background`, tone: "text-sky-300" },
  { section: "idle", count: (n) => `${n} idle`, tone: "text-zinc-500" },
]

function formatDuration(seconds: number): string {
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m`
  return `${Math.floor(minutes / 60)}h ${minutes % 60}m`
}

// How an agent's state is written on its tile, and the colors that go with it.
function stateOf(agent: AgentInfo): { label: string; tone: string; frame: string; head: string } {
  switch (agent.activity) {
    case "waiting":
      return { label: "needs you", tone: "text-yellow-400", frame: "border-yellow-600/60", head: "bg-yellow-950/40" }
    case "error":
      return { label: "error", tone: "text-red-400", frame: "border-red-700/60", head: "bg-red-950/40" }
    case "working":
      return { label: "running", tone: "text-emerald-400", frame: "border-zinc-800", head: "bg-zinc-900" }
    case "background":
      return { label: "background", tone: "text-sky-300", frame: "border-zinc-800", head: "bg-zinc-900" }
    default:
      return { label: "idle", tone: "text-zinc-500", frame: "border-zinc-800", head: "bg-zinc-900" }
  }
}

// AgentEnsemble shows every running agent at once: a tile for each with what
// it is at, the agents that need the person first.
export function AgentEnsemble({ agents, onOpen, onSpawn, onShowList }: AgentEnsembleProps) {
  const ordered = orderForEnsemble(agents)

  return (
    <section aria-label="Ensemble" className="flex h-full min-h-0 min-w-0 flex-1 flex-col bg-zinc-950">
      <header className="flex shrink-0 flex-wrap items-center gap-x-4 gap-y-2 border-b border-zinc-800 px-3 py-2.5">
        <h1 className="text-xs font-medium uppercase tracking-wider text-zinc-400">Ensemble</h1>
        <ul aria-label="Agents by state" className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs">
          {sections.map(({ section, count, tone }) => {
            const total = agents.filter((agent) => ensembleSection(agent) === section).length
            return total > 0 && <li key={section} className={tone}>{count(total)}</li>
          })}
        </ul>
        <button
          type="button"
          onClick={onShowList}
          className="ml-auto inline-flex min-h-9 items-center gap-1.5 rounded border border-zinc-700 px-3 text-xs font-medium text-zinc-300 transition-colors hover:bg-zinc-800 hover:text-zinc-100"
        >
          <LayoutList size={14} aria-hidden="true" />
          List
        </button>
      </header>

      <div className="min-h-0 flex-1 overflow-y-auto p-3">
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4">
          {ordered.map((agent) => <AgentTile key={agent.task_id} agent={agent} onOpen={onOpen} />)}
          <button
            type="button"
            onClick={onSpawn}
            className="flex h-24 flex-col items-center justify-center gap-1 rounded-lg border border-dashed border-zinc-800 text-xs text-zinc-500 transition-colors hover:border-zinc-600 hover:text-zinc-300 sm:h-72"
          >
            <Plus size={18} aria-hidden="true" />
            Spawn agent
          </button>
        </div>
      </div>
    </section>
  )
}

function AgentTile({ agent, onOpen }: { agent: AgentInfo; onOpen: (taskId: string) => void }) {
  const { conversation, pane, failed, observe } = useAgentGlance(agent)
  const state = stateOf(agent)
  const native = Boolean(agent.has_chimera_endpoint)
  const title = [agent.parent_task_id ? agent.role : "", agent.task_title].filter(Boolean).join(" · ")

  let shown: ReactNode
  if (conversation) {
    shown = <ConversationGlance conversation={conversation} activity={agent.activity} />
  } else if (pane !== null) {
    shown = pane.trim()
      ? <pre className="whitespace-pre font-mono text-[11px] leading-4 text-zinc-400">{pane}</pre>
      : <Quiet>Nothing on its terminal yet.</Quiet>
  } else if (failed) {
    shown = <Quiet>{native ? "Could not read this agent's conversation." : "Could not read this agent's terminal."}</Quiet>
  } else {
    shown = <Quiet>Reading…</Quiet>
  }
  const context = conversation ? contextShare(conversation) : null

  return (
    <article
      ref={observe}
      aria-label={`${agent.task_id}, ${state.label}`}
      className={cn(
        "relative flex h-72 min-w-0 flex-col overflow-hidden rounded-lg border bg-zinc-900/30 transition-colors hover:border-zinc-600 focus-within:border-indigo-500",
        state.frame,
      )}
    >
      <header className={cn("flex shrink-0 items-center gap-2 px-3 py-2", state.head)}>
        <StateMark activity={agent.activity} />
        <h2 className="min-w-0 truncate font-mono text-sm font-semibold text-zinc-100">
          {/* The button covers the tile, so all of it opens the agent. */}
          <button
            type="button"
            aria-label={`Open ${agent.task_id}`}
            onClick={() => onOpen(agent.task_id)}
            className="outline-none after:absolute after:inset-0"
          >
            {agent.task_id}
          </button>
        </h2>
        <AgentKindBadge kind={agent.agent_kind} isNative={native} />
        <span className={cn("ml-auto shrink-0 text-[10px] font-medium uppercase tracking-wide", state.tone)}>
          {state.label}
          {agent.activity === "working" && agent.working_seconds >= 60 && <span className="normal-case"> · {formatDuration(agent.working_seconds)}</span>}
        </span>
      </header>
      {title && <p className="shrink-0 truncate px-3 pt-2 text-xs text-zinc-400">{title}</p>}

      <Newest className="mx-3 my-2">{shown}</Newest>

      <footer className="flex h-7 shrink-0 items-center gap-3 border-t border-zinc-800 px-3">
        <Timeline states={agent.state_timeline} />
        {context !== null && <ContextMeter share={context} />}
      </footer>
    </article>
  )
}

// Newest keeps the end of its content in view: what does not fit is cut at
// the top, where the oldest is.
function Newest({ className, children }: { className?: string; children: ReactNode }) {
  const box = useRef<HTMLDivElement>(null)
  useLayoutEffect(() => {
    if (box.current) box.current.scrollTop = box.current.scrollHeight
  })
  return <div ref={box} className={cn("min-h-0 flex-1 overflow-hidden", className)}>{children}</div>
}

function Quiet({ children }: { children: ReactNode }) {
  return <p className="text-xs text-zinc-600">{children}</p>
}

function StateMark({ activity }: { activity: string }) {
  if (activity === "working") {
    return <Loader2 size={12} aria-hidden="true" className="shrink-0 animate-spin text-emerald-400 motion-reduce:animate-none" />
  }
  const tone = activity === "waiting" ? "bg-yellow-500"
    : activity === "error" ? "bg-red-500"
    : activity === "background" ? "bg-sky-400"
    : "bg-zinc-600"
  return <span aria-hidden="true" className={cn("inline-flex h-2 w-2 shrink-0 rounded-full", tone)} />
}

// The agent's recent activity, one cell for each stretch of time.
function Timeline({ states }: { states?: string[] }) {
  if (!states?.length) return null
  return (
    <div aria-hidden="true" className="flex w-24 items-center gap-0.5">
      {states.map((state, index) => (
        <span
          key={index}
          className={cn(
            "h-1.5 flex-1 rounded-sm",
            state === "working" ? "bg-emerald-500" : state === "waiting" ? "bg-yellow-500" : state === "error" ? "bg-red-500" : "bg-zinc-700",
          )}
        />
      ))}
    </div>
  )
}

// How full the agent's context is. It turns yellow, then red, as it fills.
function ContextMeter({ share }: { share: number }) {
  const tone = share >= 85 ? "text-red-400" : share >= 70 ? "text-yellow-400" : "text-zinc-400"
  const fill = share >= 85 ? "bg-red-400" : share >= 70 ? "bg-yellow-400" : "bg-zinc-400"
  return (
    <span role="img" aria-label={`Context ${share}% full`} className="ml-auto flex items-center gap-1.5 text-[10px]">
      <span className="text-zinc-600">ctx</span>
      <span className="h-1.5 w-12 overflow-hidden rounded-sm bg-zinc-800">
        <span className={cn("block h-full", fill)} style={{ width: `${share}%` }} />
      </span>
      <span className={cn("tabular-nums", tone)}>{share}%</span>
    </span>
  )
}

// The end of a native agent's conversation: what was asked, said and done,
// and after it what the agent waits on the person for.
function ConversationGlance({ conversation, activity }: { conversation: AgentConversation; activity: string }) {
  const items = glanceItems(conversation.entries, activity)
  const { permission, question } = conversation
  if (items.length === 0 && !permission && !question) return <Quiet>No messages yet.</Quiet>

  return (
    <div className="flex flex-col gap-1 text-xs leading-5">
      {items.map((item, index) => <GlanceRow key={index} item={item} />)}
      {permission && (
        <div className="flex flex-col gap-1">
          <p className="text-zinc-200">
            {permission.plan ? "Asks you to approve a plan:" : !permission.tool || permission.tool === "bash" ? "Wants to run:" : `Wants to use ${permission.tool}:`}
          </p>
          {permission.plan
            ? <p className="line-clamp-3 whitespace-pre-line pl-4 text-zinc-400">{plainText(permission.plan)}</p>
            : <code className="line-clamp-3 rounded bg-zinc-950 px-2 py-1 font-mono text-yellow-300">{permission.command || permission.reason}</code>}
        </div>
      )}
      {question && (
        <div className="flex flex-col">
          <p className="text-zinc-200">Asks:</p>
          <p className="line-clamp-3 pl-4 text-yellow-200">{question.text}</p>
          {question.options.length > 0 && <p className="truncate pl-4 text-zinc-500">{question.options.join(" · ")}</p>}
        </div>
      )}
    </div>
  )
}

const toolMarks: Record<ToolState, { mark: ReactNode; text: string }> = {
  done: { mark: <span className="text-emerald-500">●</span>, text: "text-zinc-500" },
  failed: { mark: <span className="text-red-400">✗</span>, text: "text-zinc-500" },
  running: { mark: <Loader2 size={10} className="animate-spin text-yellow-300 motion-reduce:animate-none" />, text: "text-zinc-200" },
  waiting: { mark: <span className="text-yellow-400">◆</span>, text: "text-zinc-200" },
  stopped: { mark: <span className="text-zinc-600">○</span>, text: "text-zinc-500" },
}

function GlanceRow({ item }: { item: GlanceItem }) {
  switch (item.kind) {
    case "person":
      return (
        <p className="flex gap-1.5 text-zinc-400">
          <span aria-hidden="true" className="w-2.5 shrink-0 text-indigo-400">›</span>
          <span className="line-clamp-2 whitespace-pre-line">{item.text}</span>
        </p>
      )
    case "relayed":
      return (
        <p className="flex gap-1.5 text-zinc-500">
          <span aria-hidden="true" className="w-2.5 shrink-0 text-teal-400">›</span>
          <span className="line-clamp-2">
            <span className="mr-1.5 text-[10px] font-medium uppercase tracking-wide text-teal-400">{item.from}</span>
            {item.text}
          </span>
        </p>
      )
    case "said":
      return (
        <p className={cn("whitespace-pre-line pl-4", item.newest ? "line-clamp-6 text-zinc-100" : "line-clamp-2 text-zinc-500")}>
          {plainText(item.text)}
        </p>
      )
    case "tool": {
      const { mark, text } = toolMarks[item.state]
      return (
        <p data-state={item.state} className={cn("flex min-w-0 items-center gap-1.5", text)}>
          <span aria-hidden="true" className="flex w-2.5 shrink-0 justify-center">{mark}</span>
          <span className="shrink-0 font-mono font-medium">{item.tool}</span>
          <span className="min-w-0 truncate">{item.text}</span>
        </p>
      )
    }
    case "earlier":
      return (
        <p className="pl-4 text-zinc-600">
          ⋮ {item.count === null ? "earlier calls" : item.count === 1 ? "1 earlier call" : `${item.count} earlier calls`}
        </p>
      )
  }
}
