// AgentGraphView is the ensemble's Graph layout (LEG-537): every running
// agent on one graph, boxed by repository, its state as its border, its
// sub-agents branching off it, and the links between agents that
// coordinate. A click (or Enter) selects an agent, lights what it is linked
// to and opens a card to open or message it; a double-click opens it. An
// agent that works for a workflow run shows the run's step on its node, the
// run in its card, and, on asking, the run's own graph (LEG-612).
import { useCallback, useEffect, useMemo, useRef, useState, type KeyboardEvent } from "react"
import { Link } from "react-router-dom"
import { Maximize2, Workflow, X } from "lucide-react"
import { useWebSocket, type AgentInfo, type WSMessage } from "../hooks/useWebSocket"
import { useServer } from "../hooks/useServer"
import { useMediaQuery } from "../hooks/useMediaQuery"
import { apiFetch } from "../lib/api"
import { compactGraphMemory, layoutAgentGraph, linkKinds, linkLabels, linkLines, workflowWords, type AgentGraphData, type AgentGraphLayout, type GraphMemory, type GraphNode, type NodeWorkflow } from "../lib/agentGraph"
import { runState, stepSegments } from "../lib/workflowView"
import { glanceSection, needsYouOutside, stageLabel, type WorkStage } from "../lib/agentStage"
import { formatAgentTime, stateSeconds } from "../lib/agentList"
import { agentToolLabel } from "../lib/agentKind"
import { cn } from "../lib/utils"
import { LazyAgentGraphCanvas } from "./agent-graph/LazyAgentGraphCanvas"
import { RunGraphPanel } from "./agent-graph/RunGraphPanel"
import { StatePill, StepBar } from "./WorkflowRunStatus"
import { linkStyles } from "./agent-graph/linkStyles"
import { agentLook, subagentLook } from "./agent-graph/look"
import { StageMenu } from "./StageMenu"
import { LoadingState } from "./ui/LoadingState"
import { coalesce, isRunChange } from "../lib/querySync"

export interface AgentGraphViewProps {
  agents: AgentInfo[]
  onOpen: (taskId: string) => void
  onSend?: (taskId: string, content: string) => void
  // The agents list's stage filter, which the graph follows.
  stageFilter?: WorkStage | null
  onStageFilter?: (stage: WorkStage | null) => void
  // What the address names (from a run page): an agent to select, and the
  // run whose graph to open and to link back to.
  focus?: GraphFocus | null
}

export interface GraphFocus {
  agentId?: string
  runId?: string
}

// RunPanel is the run whose graph is open, and the agent it was opened for.
interface RunPanel {
  runId: string
  agentId: string
}

// How often the graph's clock moves, so a peer message's link fades.
const TICK_MS = 30_000

export function AgentGraphView({ agents, onOpen, onSend, stageFilter = null, onStageFilter, focus: linked = null }: AgentGraphViewProps) {
  const { baseUrl } = useServer()
  const { subscribe } = useWebSocket()
  const phone = useMediaQuery("(max-width: 767px)")
  const direction = phone ? "TB" : "LR"
  const [data, setData] = useState<AgentGraphData | null>(null)
  // The agents listed when the graph was last read: one listed since then
  // waits for the next read to say where it works.
  const [readFor, setReadFor] = useState<ReadonlySet<string> | null>(null)
  // What the last layout left, so the next keeps its order (LEG-615).
  const memory = useRef<GraphMemory | undefined>(undefined)
  const [compacted, setCompacted] = useState(0)
  // The graph is drawn once the first read has answered (or failed), so it
  // is first fitted with its agents in their boxes.
  const [read, setRead] = useState(false)
  const [now, setNow] = useState(() => Date.now())
  const [unfolded, setUnfolded] = useState<ReadonlySet<string>>(new Set())
  const [collapsed, setCollapsed] = useState<ReadonlySet<string>>(new Set())
  const [selected, setSelected] = useState<string | null>(null)
  const [focusOn, setFocusOn] = useState<{ id: string; n: number } | null>(null)
  const [fit, setFit] = useState(0)
  const [runPanel, setRunPanel] = useState<RunPanel | null>(null)
  // The button that opened the run's graph, which has focus again when it
  // closes.
  const opener = useRef<HTMLElement | null>(null)
  // A run's change reads the graph again: a run can move on to its next
  // step without any agent changing. A step completing and the next
  // starting is one read.
  const [moved, setMoved] = useState(0)
  useEffect(() => {
    const read = coalesce(() => setMoved((n) => n + 1))
    const unsubscribe = subscribe?.((message: WSMessage) => {
      if (isRunChange(message)) read()
    })
    return () => {
      read.cancel()
      unsubscribe?.()
    }
  }, [subscribe])

  // The graph is read again whenever the agent list is: a new list comes
  // with every agents_changed, which a peer message sends too.
  useEffect(() => {
    let live = true
    const listed = new Set(agents.map((agent) => agent.task_id))
    apiFetch(baseUrl, "/api/agents/graph")
      .then((res) => (res.ok ? res.json() : null))
      .then((body: AgentGraphData | null) => {
        if (!live || !body) return
        setData(body)
        setReadFor(listed)
      })
      .catch(() => { /* the graph draws from the agent list alone */ })
      .finally(() => { if (live) setRead(true) })
    setNow(Date.now())
    return () => { live = false }
  }, [baseUrl, agents, moved])
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), TICK_MS)
    return () => clearInterval(timer)
  }, [])

  const counted = useMemo(() => agents.filter((agent) => !agent.reeve), [agents])
  // The stage filter dims the agents it leaves out rather than taking them
  // off the graph, so a change of stage never moves a node.
  const atStage = useMemo(() => stageFilter ? agents.filter((agent) => agent.reeve || agent.stage === stageFilter) : agents, [agents, stageFilter])
  const faded = useMemo(() => new Set(stageFilter ? agents.filter((agent) => !agent.reeve && agent.stage !== stageFilter).map((agent) => agent.task_id) : []), [agents, stageFilter])
  const pending = useMemo(() => new Set(readFor && data
    ? agents.filter((agent) => !readFor.has(agent.task_id) && !data.agents[agent.task_id] && !agent.clone?.repo).map((agent) => agent.task_id)
    : []), [agents, readFor, data])
  const layout = useMemo(() => {
    // Nothing is kept from before the first read has settled: until then the
    // links that order a box's agents are unknown.
    if (!read) return layoutAgentGraph(agents, data, { now, direction, unfolded, collapsed, pending })
    const next = layoutAgentGraph(agents, data, { now, direction, unfolded, collapsed, pending, memory: memory.current })
    memory.current = next.memory
    return next
    // compacted lays the graph out again after Fit let go of held places.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [agents, data, read, now, direction, unfolded, collapsed, pending, compacted])
  // Fit the graph also closes up the places of agents that left.
  const fitGraph = useCallback(() => {
    if (memory.current) memory.current = compactGraphMemory(memory.current)
    setCompacted((n) => n + 1)
    setFit((n) => n + 1)
  }, [])

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
  const focus = useCallback((id: string) => {
    setSelected(id)
    setFocusOn((current) => ({ id, n: (current?.n ?? 0) + 1 }))
  }, [])

  // The run's graph belongs to the selected agent: it goes with the card.
  const panel = runPanel && selectedNode && selectedNode.agentId === runPanel.agentId ? runPanel : null
  const closePanel = useCallback(() => {
    setRunPanel(null)
    const back = opener.current
    opener.current = null
    if (back?.isConnected) requestAnimationFrame(() => back.focus())
  }, [])
  const openPanel = (runId: string, agentId: string, button: HTMLElement) => {
    opener.current = button
    setRunPanel({ runId, agentId })
  }
  // Escape closes the run's graph, then the card.
  useEffect(() => {
    const onKey = (event: globalThis.KeyboardEvent) => {
      if (event.key !== "Escape" || event.defaultPrevented) return
      if (panel) closePanel()
      else if (selected) setSelected(null)
      else return
      event.preventDefault()
    }
    document.addEventListener("keydown", onKey)
    return () => document.removeEventListener("keydown", onKey)
  }, [panel, selected, closePanel])
  // An agent the address names is kept until it is on the graph (the page
  // may drop it from the address first), then selected, with its run's
  // graph open when the address names the run. A stage filter that dims it
  // is cleared.
  const [target, setTarget] = useState<GraphFocus | null>(null)
  useEffect(() => {
    if (linked?.agentId) setTarget({ agentId: linked.agentId, runId: linked.runId })
  }, [linked?.agentId, linked?.runId])
  useEffect(() => {
    const agentId = target?.agentId
    if (!agentId) return
    if (!layout.nodes.some((node) => node.id === agentId) || faded.has(agentId)) {
      // Once the page has taken the agent out of the address: clearing the
      // filter writes the address too, and would put it back.
      if (stageFilter && onStageFilter && linked?.agentId !== agentId && agents.some((agent) => agent.task_id === agentId)) onStageFilter(null)
      return
    }
    setTarget(null)
    focus(agentId)
    if (target.runId) setRunPanel({ runId: target.runId, agentId })
  }, [target, layout, faded, focus, agents, stageFilter, onStageFilter, linked?.agentId])
  const panelWorkflow = panel ? selectedNode?.workflows?.find((workflow) => workflow.run.id === panel.runId) : undefined

  const needsYou = atStage.filter((agent) => !agent.reeve && glanceSection(agent) === "needs-you")
  const hiddenNeeds = needsYouOutside(counted, stageFilter)
  const kinds = linkKinds.filter((kind) => layout.edges.some((edge) => edge.kind === kind))

  return (
    <div data-agent-graph className="relative flex h-full min-h-0 flex-col">
      {/* One line, whatever needs you: a toolbar that wrapped would move the
          whole graph down on a change of state. */}
      <div className="flex h-11 shrink-0 flex-nowrap items-center gap-x-3 border-b border-[var(--theme-border,#222228)] px-5">
        {onStageFilter && <StageMenu agents={counted} filter={stageFilter} onFilter={onStageFilter} touch={phone} />}
        {needsYou.length > 0 && (
          <ul aria-label="Needs you" className="m-0 flex min-w-0 list-none flex-nowrap items-center gap-1.5 overflow-x-auto p-0">
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
          <span className="shrink-0 whitespace-nowrap text-xs text-lw-needs">
            {hiddenNeeds} more {hiddenNeeds === 1 ? "needs" : "need"} you · <button type="button" onClick={() => onStageFilter(null)} className="text-lw-link hover:underline">Show all</button>
          </span>
        )}
        <div className="ml-auto flex shrink-0 items-center gap-3">
          {linked?.runId && <Link to={`/workflows/runs/${encodeURIComponent(linked.runId)}`} className="text-xs text-lw-link hover:underline">Back to workflow</Link>}
          {kinds.length > 0 && (
            <ul aria-label="Links" className="m-0 hidden list-none flex-nowrap items-center gap-x-3 p-0 text-[11px] text-lw-text-2 xl:flex">
              {kinds.map((kind) => <li key={kind} className="flex items-center gap-1"><LinkSwatch kind={kind} />{linkLabels[kind]}</li>)}
            </ul>
          )}
          <button type="button" aria-label="Fit the graph" title="Fit the graph" onClick={fitGraph} className="flex h-7 w-7 items-center justify-center rounded-md border border-lw-line text-lw-text-2 hover:text-lw-text">
            <Maximize2 size={14} aria-hidden="true" />
          </button>
        </div>
      </div>
      <div className="relative min-h-0 flex-1">
        {!read
          ? <LoadingState label="Reading the graph…" size="inline" />
          : agents.length === 0
          ? <p className="py-12 text-center text-lw-faint">No agents running.</p>
          : <LazyAgentGraphCanvas layout={layout} direction={direction} selectedId={selected} onSelect={setSelected} onOpen={onOpen}
              onToggleFold={onToggleFold} onToggleBox={onToggleBox} focusOn={focusOn} fit={fit} faded={faded} />}
        {/* Over the graph, which stays drawn (dimmed) so its view is kept. */}
        {read && stageFilter && counted.length > 0 && faded.size === counted.length && (
          <p className="pointer-events-none absolute inset-x-0 top-3 text-center text-lw-faint">No agents at this stage.</p>
        )}
        {selectedNode && !(phone && panel) && (
          <SelectionCard node={selectedNode} layout={layout} agents={agents} phone={phone} onOpen={onOpen} onSend={onSend} onClose={() => setSelected(null)}
            onShowRun={(runId, button) => openPanel(runId, selectedNode.agentId, button)} />
        )}
        {panel && (
          <RunGraphPanel runId={panel.runId} title={panelWorkflow?.run.workflow_title ?? panel.runId} stepId={panelWorkflow?.place.step_id} phone={phone} onClose={closePanel} />
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
function SelectionCard({ node, layout, agents, phone, onOpen, onSend, onClose, onShowRun }: {
  node: GraphNode
  layout: AgentGraphLayout
  agents: AgentInfo[]
  phone: boolean
  onOpen: (taskId: string) => void
  onSend?: (taskId: string, content: string) => void
  onClose: () => void
  onShowRun: (runId: string, button: HTMLElement) => void
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
      {node.workflows?.map((workflow) => <WorkflowSection key={workflow.run.id} workflow={workflow} onShowRun={onShowRun} />)}
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

// WorkflowSection is a run the selected agent works for: the run, how far
// it is, the agent's step and role, and its graph on asking.
function WorkflowSection({ workflow, onShowRun }: { workflow: NodeWorkflow; onShowRun: (runId: string, button: HTMLElement) => void }) {
  const { run } = workflow
  const words = workflowWords(workflow)
  const state = runState(run)
  return (
    <div role="group" aria-label="Workflow run" className="flex flex-col gap-1.5 rounded-lg border border-lw-line bg-lw-raised p-2.5 text-xs">
      <span className="flex min-w-0 items-center gap-1.5">
        <Workflow size={12} aria-hidden="true" className="shrink-0 text-lw-text-2" />
        <Link to={`/workflows/runs/${encodeURIComponent(run.id)}`} className="min-w-0 truncate font-medium text-lw-link hover:underline">{run.workflow_title || run.workflow_name}</Link>
        <StatePill tone={state.tone} label={state.label} className="ml-auto" />
      </span>
      <StepBar segments={stepSegments(run, run.steps)} label={`${state.label}, step ${words.number} of ${words.total}`} />
      <span className="text-lw-text">{words.role}</span>
      {words.now && <span className="text-lw-text-2">{words.now}</span>}
      <button type="button" onClick={(event) => onShowRun(run.id, event.currentTarget)} className="h-7 self-start rounded-md border border-lw-line-strong px-2.5 text-xs text-lw-text hover:border-lw-accent">
        Show run graph
      </button>
    </div>
  )
}
