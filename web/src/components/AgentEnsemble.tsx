import { useLayoutEffect, useRef, useState, type ReactNode } from "react"
import { Loader2 } from "lucide-react"
import type { AgentInfo } from "../hooks/useWebSocket"
import type { AgentContext } from "../hooks/useAgentContexts"
import { useAgentGlance } from "../hooks/useAgentGlance"
import {
  backgroundSummary,
  glanceItems,
  orderForEnsemble,
  plainText,
  type AgentConversation,
  type GlanceItem,
  type ToolState,
} from "../lib/agentGlance"
import { agentToolLabel } from "../lib/agentKind"
import { formatAgentTime, stateSeconds } from "../lib/agentList"
import { glanceSection, stageLabel, workStages, type GlanceSection, type WorkStage } from "../lib/agentStage"
import { cn } from "../lib/utils"
import { ActivityBar, ContextMeter } from "./AgentMeters"
import { StagePie } from "./StagePie"

interface AgentEnsembleProps {
  // The running agents, in any order.
  agents: AgentInfo[]
  // How full each native agent's context window is, by task ID.
  contexts?: Record<string, AgentContext>
  onOpen: (taskId: string) => void
  onSpawn: () => void
  onShowList: () => void
  // Sends a message to an agent, as its own message box would.
  onSend?: (taskId: string, content: string) => void
}

// The header's counts, by glance group. The Reeve is left out of every
// count.
const counts: Array<{ section: GlanceSection; count: (n: number) => string; glyph: string; tone: string }> = [
  { section: "needs-you", count: (n) => `${n} ${n === 1 ? "needs" : "need"} you`, glyph: "◆", tone: "text-lw-needs" },
  { section: "your-turn", count: (n) => `${n} your turn`, glyph: "◇", tone: "text-lw-needs opacity-85" },
  { section: "working", count: (n) => `${n} working`, glyph: "●", tone: "text-lw-live" },
  { section: "finished", count: (n) => `${n} finished`, glyph: "✓", tone: "text-lw-done" },
]

// Where the viewer's choice of layout is kept on this device.
export const ENSEMBLE_LAYOUT_KEY = "legato.ensemble.layout"

type EnsembleLayout = "tiles" | "stage"

function rememberedLayout(): EnsembleLayout {
  try {
    return localStorage.getItem(ENSEMBLE_LAYOUT_KEY) === "stage" ? "stage" : "tiles"
  } catch {
    return "tiles"
  }
}

// How an agent's state is written on its tile, and the colors that go with it.
function stateOf(agent: AgentInfo): { label: string; glyph: string; chip: string; frame: string; ground: string } {
  switch (agent.activity) {
    case "waiting":
      return { label: "needs you", glyph: "◆", chip: "bg-lw-needs-soft text-lw-needs", frame: "border-[var(--theme-border,#4a3a1a)]", ground: "bg-lw-surface" }
    case "error":
      return { label: "error", glyph: "✕", chip: "bg-lw-error-soft text-lw-error", frame: "border-[var(--theme-border,#4a2a2e)]", ground: "bg-lw-surface" }
    case "working":
      return { label: "running", glyph: "●", chip: "bg-lw-live-soft text-lw-live", frame: "border-[var(--theme-border,#2d3a33)]", ground: "bg-lw-surface" }
    case "background": {
      const waitsOn = backgroundSummary(agent.background_jobs)
      return { label: waitsOn ? `background · ${waitsOn}` : "background", glyph: "◌", chip: "bg-lw-done-soft text-lw-done", frame: "border-lw-line", ground: "bg-lw-surface" }
    }
    default:
      return { label: "idle", glyph: "○", chip: "bg-lw-idle-soft text-lw-text-2", frame: "border-lw-line", ground: "bg-[var(--theme-surface,#141417)]" }
  }
}

// AgentEnsemble shows every running agent at once: a tile for each with what
// it is at, the agents that need the person first.
export function AgentEnsemble({ agents, contexts = {}, onOpen, onSpawn, onShowList, onSend }: AgentEnsembleProps) {
  const ordered = orderForEnsemble(agents)
  const counted = agents.filter((agent) => !agent.reeve)
  const [layout, setLayoutState] = useState<EnsembleLayout>(rememberedLayout)
  const setLayout = (next: EnsembleLayout) => {
    setLayoutState(next)
    try { localStorage.setItem(ENSEMBLE_LAYOUT_KEY, next) } catch { /* not remembered */ }
  }
  const segment = (on: boolean) => cn("h-[26px] rounded px-2.5 text-xs", on ? "bg-lw-selected text-lw-text" : "text-lw-text-2 hover:text-lw-text")

  return (
    <section aria-label="Ensemble" className="flex h-full min-h-0 min-w-0 flex-1 flex-col bg-lw-ground font-lw text-[13px] leading-5 text-lw-text">
      <header className="flex shrink-0 flex-wrap items-center justify-between gap-x-4 gap-y-2 border-b border-[var(--theme-border,#222228)] px-5 py-3.5">
        <div className="flex flex-wrap items-center gap-x-3.5 gap-y-1">
          <h1 className="m-0 text-lg font-semibold leading-6">Ensemble</h1>
          <ul aria-label="Agents by state" className="m-0 flex list-none flex-wrap items-center gap-x-3 gap-y-1 p-0">
            {counts.map(({ section, count, glyph, tone }) => {
              const total = counted.filter((agent) => glanceSection(agent) === section).length
              return total > 0 && <li key={section} className={tone}><span aria-hidden="true">{glyph}</span> {count(total)}</li>
            })}
          </ul>
        </div>
        <div className="flex items-center gap-2">
          <div role="group" aria-label="Layout" className="flex rounded-md border border-lw-line bg-lw-surface p-0.5">
            <button type="button" aria-pressed={layout === "tiles"} onClick={() => setLayout("tiles")} className={segment(layout === "tiles")}>Tiles</button>
            <button type="button" aria-pressed={layout === "stage"} onClick={() => setLayout("stage")} className={segment(layout === "stage")}>By stage</button>
            <button type="button" aria-pressed="false" onClick={onShowList} className={segment(false)}>List</button>
          </div>
          <button
            type="button"
            aria-label="Spawn agent"
            onClick={onSpawn}
            className="h-8 rounded-md bg-lw-accent px-3.5 text-[13px] font-medium text-[var(--theme-canvas,#fff)] hover:brightness-110"
          >
            + Spawn agent
          </button>
        </div>
      </header>

      <div className="min-h-0 flex-1 overflow-y-auto px-5 pb-5 pt-4">
        {ordered.length === 0
          ? <p className="py-12 text-center text-lw-faint">No agents are running.</p>
          : layout === "stage"
          ? (
            <div data-ensemble-lanes className="flex flex-col gap-3 lg:grid lg:min-h-full lg:grid-cols-5">
              {workStages.map((stage) => (
                <StageLane key={stage} stage={stage} agents={ordered.filter((agent) => agent.stage === stage)} contexts={contexts} onOpen={onOpen} onSend={onSend} />
              ))}
            </div>
          )
          : (
            // The rows share the height of the window, down to the least a
            // tile needs; only then does the ensemble scroll.
            <div data-ensemble-grid className="grid min-h-full auto-rows-[minmax(15rem,1fr)] gap-3 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4">
              {ordered.map((agent) => (
                <AgentTile key={agent.task_id} agent={agent} context={contexts[agent.task_id]} onOpen={onOpen} onSend={onSend} />
              ))}
            </div>
          )}
      </div>
    </section>
  )
}

// StageLane is the agents at one stage, those that need the operator first.
// Side by side on a wide screen; on a phone the lanes stack, and a lane
// opens by itself when it holds an agent that needs the operator.
function StageLane({ stage, agents, contexts, onOpen, onSend }: {
  stage: WorkStage
  agents: AgentInfo[]
  contexts: Record<string, AgentContext>
  onOpen: (taskId: string) => void
  onSend?: (taskId: string, content: string) => void
}) {
  const needs = agents.filter((agent) => glanceSection(agent) === "needs-you").length
  const turn = agents.filter((agent) => glanceSection(agent) === "your-turn").length
  const [open, setOpen] = useState(needs > 0)
  const label = stageLabel(stage)
  const marks = [needs > 0 && `◆ ${needs}`, turn > 0 && `◇ ${turn}`].filter(Boolean).join(" · ")
  return (
    <section aria-label={label} className="flex min-w-0 flex-col gap-2.5 rounded-xl border border-lw-line bg-[var(--theme-surface,#141417)] p-2.5">
      <button
        type="button"
        aria-expanded={open}
        onClick={() => setOpen((value) => !value)}
        className="flex min-h-11 items-center gap-2 px-1 text-left text-[13px] font-semibold lg:pointer-events-none lg:min-h-0"
      >
        <StagePie stage={stage} label="" />
        {label}
        <span className="font-lw-mono text-xs font-normal text-lw-text-2">{agents.length}</span>
        {marks && <span className="ml-auto text-xs font-medium text-lw-needs">{marks}</span>}
      </button>
      <div className={cn("flex-col gap-2.5", open ? "flex" : "hidden lg:flex")}>
        {agents.length === 0
          ? <p className="m-0 px-1 py-2 text-xs text-lw-text-2">Nothing here.</p>
          : agents.map((agent) => (
            <div key={agent.task_id} className="h-60"><AgentTile agent={agent} context={contexts[agent.task_id]} onOpen={onOpen} onSend={onSend} /></div>
          ))}
      </div>
    </section>
  )
}

function AgentTile({ agent, context, onOpen, onSend }: {
  agent: AgentInfo
  context?: AgentContext
  onOpen: (taskId: string) => void
  onSend?: (taskId: string, content: string) => void
}) {
  const { conversation, pane, failed, observe } = useAgentGlance(agent)
  const state = stateOf(agent)
  const native = Boolean(agent.has_chimera_endpoint)
  const idle = state.label === "idle"
  const time = formatAgentTime(stateSeconds(agent))
  const tool = native ? agentToolLabel(agent.agent_kind, agent.engram) : `${agentToolLabel(agent.agent_kind, agent.engram)} · terminal`
  const meta = [tool, agent.parent_task_id ? agent.role : ""].filter(Boolean).join(" · ")
  const lastReply = idle && conversation ? newestReply(conversation) : null

  let shown: ReactNode
  if (lastReply) {
    shown = <LastReply text={lastReply} onRead={() => onOpen(agent.task_id)} />
  } else if (conversation) {
    shown = <Newest><ConversationGlance conversation={conversation} activity={agent.activity} /></Newest>
  } else if (pane !== null) {
    shown = <Newest>{pane.trim()
      ? <pre className="m-0 whitespace-pre font-lw-mono text-[11px] leading-4 text-lw-text-2">{pane}</pre>
      : <Quiet>Nothing on its terminal yet.</Quiet>}</Newest>
  } else if (failed) {
    shown = <Quiet>{native ? "Could not read this agent's conversation." : "Could not read this agent's terminal."}</Quiet>
  } else {
    shown = <Quiet>Reading…</Quiet>
  }

  return (
    <article
      ref={observe}
      aria-label={`${agent.task_id}, ${state.label}`}
      className={cn("flex min-h-0 min-w-0 flex-col overflow-hidden rounded-[10px] border", state.frame, state.ground)}
    >
      <div className="flex flex-col gap-1 border-b border-[var(--theme-border,#222228)] px-3.5 pb-2.5 pt-3">
        <div className="flex items-center justify-between gap-2">
          <span className={cn("rounded px-2 font-lw-mono text-[11px] uppercase", state.chip)}>
            <span aria-hidden="true">{state.glyph}</span> {state.label}{time && ` ${time}`}
          </span>
          {stageLabel(agent.stage) && (
            <span className={cn("flex shrink-0 items-center gap-1 text-[11px]", agent.stage === "finished" ? "text-lw-done" : "text-lw-text-2")}>
              <StagePie stage={agent.stage} size={11} />{stageLabel(agent.stage)}
            </span>
          )}
          <span className="ml-auto truncate font-lw-mono text-xs text-lw-faint">{agent.task_id}</span>
        </div>
        <span className="truncate text-[15px] font-semibold">{agent.task_title || agent.task_id}</span>
        <span className="truncate text-xs text-lw-text-2">{meta}</span>
      </div>

      {/* Contained in size: what the agent says never makes the tile, or its
          row, taller; the newest is kept in view and the rest cut. */}
      <div data-tile-body className="flex min-h-0 flex-1 flex-col overflow-hidden px-3.5 py-2.5 [contain:size]">{shown}</div>

      <div className="flex flex-col gap-2.5 border-t border-[var(--theme-border,#222228)] px-3.5 py-2.5">
        <div className="flex min-h-3 items-center justify-between gap-2">
          <ActivityBar states={agent.state_timeline} />
          {context && <ContextMeter share={context.share} width="w-[60px]" />}
        </div>
        <TileComposer agent={agent} idle={idle} onOpen={onOpen} onSend={onSend} />
      </div>
    </article>
  )
}

// The tile's own message box: Enter sends what was typed to the agent, which
// reads it at its next step when it is busy.
function TileComposer({ agent, idle, onOpen, onSend }: {
  agent: AgentInfo
  idle: boolean
  onOpen: (taskId: string) => void
  onSend?: (taskId: string, content: string) => void
}) {
  const [draft, setDraft] = useState("")
  const label = idle ? `Reply to ${agent.task_id}` : `Message ${agent.task_id}`
  return (
    <div className="flex gap-2">
      {onSend && (
        <input
          aria-label={label}
          placeholder={idle ? "Reply to continue…" : "Message…"}
          value={draft}
          onChange={(event) => setDraft(event.target.value)}
          onKeyDown={(event) => {
            if (event.key !== "Enter" || event.shiftKey || event.nativeEvent.isComposing) return
            event.preventDefault()
            const content = draft.trim()
            if (!content) return
            onSend(agent.task_id, content)
            setDraft("")
          }}
          className={cn(
            "h-[30px] min-w-0 flex-1 rounded-md border bg-lw-ground px-2.5 text-xs text-lw-text outline-none placeholder:text-lw-faint focus:border-lw-accent",
            idle ? "border-lw-accent" : "border-lw-line-strong",
          )}
        />
      )}
      <button
        type="button"
        aria-label={`Open ${agent.task_id}`}
        onClick={() => onOpen(agent.task_id)}
        className={cn("h-[30px] shrink-0 rounded-md border border-lw-line-strong bg-lw-raised px-3 text-xs text-lw-text hover:border-[var(--theme-border,#4a4955)]", !onSend && "flex-1")}
      >
        Open
      </button>
    </div>
  )
}

// The newest thing an agent said, when there is one: what an idle tile shows.
function newestReply(conversation: AgentConversation): string | null {
  const said = conversation.entries.findLast((entry) => entry.kind === "assistant" && entry.text.trim())
  return said ? plainText(said.text) : null
}

function LastReply({ text, onRead }: { text: string; onRead: () => void }) {
  return (
    <div className="flex min-h-0 flex-1 flex-col gap-2">
      <span className="text-[11px] font-semibold uppercase tracking-[1px] text-lw-faint">Last reply</span>
      <p className="m-0 min-h-0 overflow-hidden whitespace-pre-line text-[13px] leading-5 text-lw-text [display:-webkit-box] [-webkit-box-orient:vertical] [-webkit-line-clamp:9]">{text}</p>
      <button type="button" onClick={onRead} className="self-start text-xs text-lw-link hover:text-[var(--theme-link,#c8c0ff)]">Read full reply</button>
    </div>
  )
}

// Newest keeps the end of its content in view: what does not fit is cut at
// the top, where the oldest is.
function Newest({ children }: { children: ReactNode }) {
  const box = useRef<HTMLDivElement>(null)
  useLayoutEffect(() => {
    if (box.current) box.current.scrollTop = box.current.scrollHeight
  })
  return <div ref={box} className="min-h-0 flex-1 overflow-hidden">{children}</div>
}

function Quiet({ children }: { children: ReactNode }) {
  return <p className="m-0 text-xs text-lw-faint">{children}</p>
}

// The end of a native agent's conversation: what was asked, said and done,
// and after it what the agent waits on the person for.
function ConversationGlance({ conversation, activity }: { conversation: AgentConversation; activity: string }) {
  const items = glanceItems(conversation.entries, activity)
  const { permission, question } = conversation
  if (items.length === 0 && !permission && !question) return <Quiet>No messages yet.</Quiet>

  return (
    <div className="flex flex-col gap-1.5 text-xs leading-[18px]">
      {items.map((item, index) => <GlanceRow key={index} item={item} />)}
      {permission && (
        <div className="flex flex-col gap-1">
          <p className="m-0 text-lw-text">
            {permission.plan ? "Asks you to approve a plan:" : !permission.tool || permission.tool === "bash" ? "Wants to run:" : `Wants to use ${permission.tool}:`}
          </p>
          {permission.plan
            ? <p className="m-0 line-clamp-3 whitespace-pre-line pl-4 text-lw-text-2">{plainText(permission.plan)}</p>
            : <code className="line-clamp-3 rounded bg-lw-ground px-2 py-1 font-lw-mono text-lw-needs">{permission.command || permission.reason}</code>}
        </div>
      )}
      {question && (
        <div className="flex flex-col">
          <p className="m-0 text-lw-text">Asks:</p>
          <p className="m-0 line-clamp-3 pl-4 text-lw-needs">{question.text}</p>
          {question.options.length > 0 && <p className="m-0 truncate pl-4 text-lw-text-2">{question.options.join(" · ")}</p>}
        </div>
      )}
    </div>
  )
}

const toolMarks: Record<ToolState, { mark: ReactNode; text: string }> = {
  done: { mark: <span className="text-lw-live">●</span>, text: "text-lw-text-2" },
  failed: { mark: <span className="text-lw-error">✕</span>, text: "text-lw-text-2" },
  running: { mark: <Loader2 size={10} className="animate-spin text-lw-needs motion-reduce:animate-none" />, text: "text-lw-text" },
  waiting: { mark: <span className="text-lw-needs">◆</span>, text: "text-lw-text" },
  stopped: { mark: <span className="text-lw-faint">○</span>, text: "text-lw-text-2" },
}

function GlanceRow({ item }: { item: GlanceItem }) {
  switch (item.kind) {
    case "person":
      return <p className="m-0 line-clamp-2 self-end whitespace-pre-line rounded-lg bg-lw-selected px-2.5 py-1 text-lw-text">{item.text}</p>
    case "relayed":
      return (
        <p className="m-0 line-clamp-2 self-end rounded-lg bg-lw-raised px-2.5 py-1 text-lw-text-2">
          <span className="mr-1.5 text-[10px] font-medium uppercase tracking-wide text-teal-400">{item.from}</span>
          {item.text}
        </p>
      )
    case "said":
      return (
        <p className={cn("m-0 whitespace-pre-line pl-4", item.newest ? "line-clamp-6 text-lw-text" : "line-clamp-2 text-lw-text-2")}>
          {plainText(item.text)}
        </p>
      )
    case "tool": {
      const { mark, text } = toolMarks[item.state]
      return (
        <p data-state={item.state} className={cn("m-0 flex min-w-0 items-start gap-2", text)}>
          <span aria-hidden="true" className="flex w-2.5 shrink-0 justify-center pt-[3px]">{mark}</span>
          <span className="w-[84px] shrink-0 truncate font-lw-mono text-lw-link">{item.tool}</span>
          <span className="min-w-0 line-clamp-2">{item.text}</span>
        </p>
      )
    }
    case "earlier":
      return (
        <p className="m-0 text-lw-faint">
          {item.count === null ? "earlier calls" : item.count === 1 ? "1 earlier call" : `${item.count} earlier calls`}
        </p>
      )
  }
}
