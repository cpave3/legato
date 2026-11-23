// AgentGraphView is the ensemble's Graph layout (LEG-537): every running
// agent on one graph, boxed by repository, its state as its border, its
// sub-agents branching off it, and the links between agents that
// coordinate. A click selects an agent, lights what it is linked to and
// opens a card to open or message it; a double-click opens it.
import { useCallback, useEffect, useMemo, useState, type KeyboardEvent } from "react"
import { Maximize2, X } from "lucide-react"
import type { AgentInfo } from "../hooks/useWebSocket"
import { useServer } from "../hooks/useServer"
import { useMediaQuery } from "../hooks/useMediaQuery"
import { apiFetch } from "../lib/api"
import { layoutAgentGraph, linkKinds, linkLabels, linkLines, type AgentGraphData, type AgentGraphLayout, type GraphNode } from "../lib/agentGraph"
import { glanceSection, needsYouOutside, stageLabel, type WorkStage } from "../lib/agentStage"
import { formatAgentTime, stateSeconds } from "../lib/agentList"
import { agentToolLabel } from "../lib/agentKind"
import { cn } from "../lib/utils"
import { LazyAgentGraphCanvas } from "./agent-graph/LazyAgentGraphCanvas"
import { linkStyles } from "./agent-graph/linkStyles"
import { agentLook, subagentLook } from "./agent-graph/look"
import { StageMenu } from "./StageMenu"
import { LoadingState } from "./ui/LoadingState"

export interface AgentGraphViewProps {
  agents: AgentInfo[]
  onOpen: (taskId: string) => void
  onSend?: (taskId: string, content: string) => void
  // The agents list's stage filter, which the graph follows.
  stageFilter?: WorkStage | null
  onStageFilter?: (stage: WorkStage | null) => void
}

// How often the graph's clock moves, so a peer message's link fades.
const TICK_MS = 30_000

export function AgentGraphView({ agents, onOpen, onSend, stageFilter = null, onStageFilter }: AgentGraphViewProps) {
  const { baseUrl } = useServer()
  const phone = useMediaQuery("(max-width: 767px)")
  const direction = phone ? "TB" : "LR"
  const [data, setData] = useState<AgentGraphData | null>(null)
  // The graph is drawn once the first read has answered (or failed), so it
  // is first fitted with its agents in their boxes.
  const [read, setRead] = useState(false)
  const [now, setNow] = useState(() => Date.now())
  const [unfolded, setUnfolded] = useState<ReadonlySet<string>>(new Set())
  const [collapsed, setCollapsed] = useState<ReadonlySet<string>>(new Set())
  const [selected, setSelected] = useState<string | null>(null)
  const [focusOn, setFocusOn] = useState<{ id: string; n: number } | null>(null)
  const [fit, setFit] = useState(0)

  // The graph is read again whenever the agent list is: a new list comes
  // with every agents_changed, which a peer message sends too.
  useEffect(() => {
    let live = true
    apiFetch(baseUrl, "/api/agents/graph")
      .then((res) => (res.ok ? res.json() : null))
      .then((body: AgentGraphData | null) => { if (live && body) setData(body) })
      .catch(() => { /* the graph draws from the agent list alone */ })
      .finally(() => { if (live) setRead(true) })
    setNow(Date.now())
    return () => { live = false }
  }, [baseUrl, agents])
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), TICK_MS)
    return () => clearInterval(timer)
  }, [])

  const counted = useMemo(() => agents.filter((agent) => !agent.reeve), [agents])
  const shown = useMemo(() => stageFilter ? agents.filter((agent) => agent.reeve || agent.stage === stageFilter) : agents, [agents, stageFilter])
  const layout = useMemo(() => layoutAgentGraph(shown, data, { now, direction, unfolded, collapsed }), [shown, data, now, direction, unfolded, collapsed])

  // A selection that left the graph is let go.
  const selectedNode = layout.nodes.find((node) => node.id === selected) ?? null
  useEffect(() => {
    if (selected && !selectedNode) setSelected(null)
  }, [selected, selectedNode])

  const toggle = (set: ReadonlySet<string>, id: string) => {
    const next = new Set(set)
    if (!next.delete(id)) next.add(id)
    return next
  }
  const onToggleFold = useCallback((id: string) => setUnfolded((current) => toggle(current, id)), [])
  const onToggleBox = useCallback((id: string) => setCollapsed((current) => toggle(current, id)), [])
  const focus = (id: string) => {
    setSelected(id)
    setFocusOn((current) => ({ id, n: (current?.n ?? 0) + 1 }))
  }

  const needsYou = shown.filter((agent) => !agent.reeve && glanceSection(agent) === "needs-you")
  const hiddenNeeds = needsYouOutside(counted, stageFilter)
  const kinds = linkKinds.filter((kind) => layout.edges.some((edge) => edge.kind === kind))

  return (
    <div data-agent-graph className="relative flex h-full min-h-0 flex-col">
      <div className="flex shrink-0 flex-wrap items-center gap-x-3 gap-y-2 border-b border-[var(--theme-border,#222228)] px-5 py-2">
        {onStageFilter && <StageMenu agents={counted} filter={stageFilter} onFilter={onStageFilter} touch={phone} />}
        {needsYou.length > 0 && (
          <ul aria-label="Needs you" className="m-0 flex list-none flex-wrap items-center gap-1.5 p-0">
            {needsYou.map((agent) => (
              <li key={agent.task_id}>
                <button type="button" onClick={() => focus(agent.task_id)} className="flex h-7 items-center gap-1 rounded-md border border-lw-needs/50 bg-lw-needs-soft px-2 text-xs text-lw-needs hover:border-lw-needs">
                  <span aria-hidden="true">◆</span><span className="font-lw-mono">{agent.task_id}</span>
                </button>
              </li>
            ))}
          </ul>
        )}
        {hiddenNeeds > 0 && onStageFilter && (
          <span className="text-xs text-lw-needs">
            {hiddenNeeds} more {hiddenNeeds === 1 ? "needs" : "need"} you · <button type="button" onClick={() => onStageFilter(null)} className="text-lw-link hover:underline">Show all</button>
          </span>
        )}
        <div className="ml-auto flex items-center gap-3">
          {kinds.length > 0 && (
            <ul aria-label="Links" className="m-0 hidden list-none flex-wrap items-center gap-x-3 gap-y-1 p-0 text-[11px] text-lw-text-2 sm:flex">
              {kinds.map((kind) => <li key={kind} className="flex items-center gap-1"><LinkSwatch kind={kind} />{linkLabels[kind]}</li>)}
            </ul>
          )}
          <button type="button" aria-label="Fit the graph" title="Fit the graph" onClick={() => setFit((n) => n + 1)} className="flex h-7 w-7 items-center justify-center rounded-md border border-lw-line text-lw-text-2 hover:text-lw-text">
            <Maximize2 size={14} aria-hidden="true" />
          </button>
        </div>
      </div>
      <div className="relative min-h-0 flex-1">
        {!read
          ? <LoadingState label="Reading the graph…" size="inline" />
          : shown.length === 0
          ? <p className="py-12 text-center text-lw-faint">No agents at this stage.</p>
          : <LazyAgentGraphCanvas layout={layout} direction={direction} selectedId={selected} onSelect={setSelected} onOpen={onOpen}
              onToggleFold={onToggleFold} onToggleBox={onToggleBox} focusOn={focusOn} fit={fit} />}
        {selectedNode && (
          <SelectionCard node={selectedNode} layout={layout} agents={agents} phone={phone} onOpen={onOpen} onSend={onSend} onClose={() => setSelected(null)} />
        )}
      </div>
    </div>
  )
}

function LinkSwatch({ kind }: { kind: keyof typeof linkStyles }) {
  const style = linkStyles[kind]
  return <svg aria-hidden="true" width="22" height="6" className="shrink-0">
    <line x1="1" y1="3" x2="21" y2="3" stroke={style.stroke} strokeWidth={style.width} strokeDasharray={style.dash} strokeLinecap="round" />
  </svg>
}

// SelectionCard is the selected agent (or the agent of a selected
// sub-agent): what it is at, who it is linked to, a message box and Open.
function SelectionCard({ node, layout, agents, phone, onOpen, onSend, onClose }: {
  node: GraphNode
  layout: AgentGraphLayout
  agents: AgentInfo[]
  phone: boolean
  onOpen: (taskId: string) => void
  onSend?: (taskId: string, content: string) => void
  onClose: () => void
}) {
  const agent = agents.find((candidate) => candidate.task_id === node.agentId)
  const [draft, setDraft] = useState("")
  if (!agent) return null
  const look = agentLook(agent)
  const time = formatAgentTime(stateSeconds(agent))
  const sub = node.kind === "subagent" ? node : null
  const subLook = sub ? subagentLook(sub.state) : null
  const subagents = (agent.background_jobs ?? []).filter((job) => job.kind === "subagent").length
  const lines = linkLines(agent.task_id, layout.edges, agents)
  const send = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key !== "Enter" || event.shiftKey || event.nativeEvent.isComposing) return
    event.preventDefault()
    const content = draft.trim()
    if (!content || !onSend) return
    onSend(agent.task_id, content)
    setDraft("")
  }
  return (
    <section
      aria-label={agent.task_id}
      className={cn(
        "absolute z-10 flex flex-col gap-2 border border-lw-line-strong bg-lw-surface p-3.5 font-lw text-[13px] text-lw-text shadow-lg",
        phone ? "inset-x-0 bottom-0 rounded-t-xl" : "right-4 top-4 w-80 rounded-xl",
      )}
    >
      <div className="flex items-center gap-2">
        <span className={cn("font-lw-mono text-[11px] uppercase", look.text)}><span aria-hidden="true">{look.glyph}</span> {look.label}{time && ` ${time}`}</span>
        <span className="ml-auto font-lw-mono text-xs text-lw-faint">{agent.task_id}</span>
        <button type="button" aria-label="Close" onClick={onClose} className="flex h-6 w-6 items-center justify-center rounded text-lw-text-2 hover:text-lw-text"><X size={14} aria-hidden="true" /></button>
      </div>
      <span className="text-[15px] font-semibold leading-5">{agent.task_title || agent.task_id}</span>
      <span className="text-xs text-lw-text-2">{[stageLabel(agent.stage), agent.stage_reason, agentToolLabel(agent.agent_kind, agent.engram)].filter(Boolean).join(" · ")}</span>
      {sub && subLook && (
        <span className="rounded-md bg-lw-raised px-2 py-1 text-xs"><span className={subLook.text}>{subLook.glyph} {subLook.label}</span> · sub-agent {sub.job?.label}</span>
      )}
      {(node.badges?.length ?? 0) > 0 && (
        <span className="flex flex-wrap gap-1">
          {node.badges!.map((badge) => <span key={badge.kind} className="rounded bg-lw-accent-soft px-1.5 text-xs text-lw-accent-text">{badge.label}{badge.detail && ` · ${badge.detail}`}</span>)}
        </span>
      )}
      {(lines.length > 0 || subagents > 0) && (
        <ul aria-label="Links of this agent" className="m-0 flex list-none flex-col gap-0.5 p-0 text-xs text-lw-text-2">
          {subagents > 0 && <li>{subagents} {subagents === 1 ? "sub-agent" : "sub-agents"} running</li>}
          {lines.map((line) => <li key={line}>{line}</li>)}
        </ul>
      )}
      <div className="flex gap-2">
        {onSend && (
          <input
            aria-label={`Message ${agent.task_id}`}
            placeholder="Message…"
            value={draft}
            onChange={(event) => setDraft(event.target.value)}
            onKeyDown={send}
            className="h-[30px] min-w-0 flex-1 rounded-md border border-lw-line-strong bg-lw-ground px-2.5 text-xs text-lw-text outline-none placeholder:text-lw-faint focus:border-lw-accent"
          />
        )}
        <button type="button" aria-label={`Open ${agent.task_id}`} onClick={() => onOpen(agent.task_id)} className={cn("h-[30px] shrink-0 rounded-md bg-lw-accent px-3 text-xs font-medium text-[var(--theme-canvas,#fff)] hover:brightness-110", !onSend && "flex-1")}>
          Open
        </button>
      </div>
    </section>
  )
}
