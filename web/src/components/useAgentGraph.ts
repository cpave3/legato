import { useCallback, useEffect, useMemo, useRef, useState } from "react"

import { useWebSocket } from "../hooks/useWebSocket"
import { compactGraphMemory, layoutAgentGraph, type AgentGraphData, type AgentGraphLayout, type GraphMemory, type GraphNode } from "../lib/agentGraph"
import { linkedAgentStep, toggled } from "../lib/agentGraphFilters"
import type { WorkStage } from "../lib/agentStage"
import type { AgentInfo, WSMessage } from "../lib/agentWire"
import { apiFetch } from "../lib/api"
import { coalesce, isRunChange } from "../lib/querySync"

// The ensemble graph's state machines: reading the graph, laying it out,
// the selection, the run's graph beside it, and the agent the address
// names. See AgentGraphView.tsx.

// How often the graph's clock moves, so a peer message's link fades.
const TICK_MS = 30_000

// RunPanel is the run whose graph is open, and the agent it was opened for.
export interface RunPanel {
  runId: string
  agentId: string
}

export interface GraphFocus {
  agentId?: string
  runId?: string
}

// A run's change reads the graph again: a run can move on to its next step
// without any agent changing. A step completing and the next starting is
// one read.
export function useRunChanges(): number {
  const { subscribe } = useWebSocket()
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
  return moved
}

// The graph is read again whenever the agent list is (a new list comes with
// every agents_changed, which a peer message sends too) and when a run
// moves. Its clock moves with each read and every TICK_MS.
export function useGraphRead(baseUrl: string, agents: AgentInfo[], moved: number) {
  const [data, setData] = useState<AgentGraphData | null>(null)
  // The agents listed when the graph was last read: one listed since then
  // waits for the next read to say where it works.
  const [readFor, setReadFor] = useState<ReadonlySet<string> | null>(null)
  // The graph is drawn once the first read has answered (or failed), so it
  // is first fitted with its agents in their boxes.
  const [read, setRead] = useState(false)
  const [now, setNow] = useState(() => Date.now())
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
  return { data, readFor, read, now }
}

// The layout, which keeps what the last one left so the next keeps its
// order (LEG-615), the boxes and sub-agent folds the operator toggled, and
// Fit, which also closes up the places of agents that left.
export function useGraphLayout(agents: AgentInfo[], data: AgentGraphData | null, read: boolean, now: number, direction: "TB" | "LR", pending: ReadonlySet<string>) {
  const memory = useRef<GraphMemory | undefined>(undefined)
  const [compacted, setCompacted] = useState(0)
  const [fit, setFit] = useState(0)
  const [unfolded, setUnfolded] = useState<ReadonlySet<string>>(new Set())
  const [collapsed, setCollapsed] = useState<ReadonlySet<string>>(new Set())
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
  const fitGraph = useCallback(() => {
    if (memory.current) memory.current = compactGraphMemory(memory.current)
    setCompacted((n) => n + 1)
    setFit((n) => n + 1)
  }, [])
  const onToggleFold = useCallback((id: string) => setUnfolded((current) => toggled(current, id)), [])
  const onToggleBox = useCallback((id: string) => setCollapsed((current) => toggled(current, id)), [])
  return { layout, fit, fitGraph, onToggleFold, onToggleBox }
}

// The selected node, let go when it leaves the graph, and focusing one,
// which selects it and brings it into view.
export function useGraphSelection(layout: AgentGraphLayout) {
  const [selected, setSelected] = useState<string | null>(null)
  const [focusOn, setFocusOn] = useState<{ id: string; n: number } | null>(null)
  const selectedNode: GraphNode | null = layout.nodes.find((node) => node.id === selected) ?? null
  useEffect(() => {
    if (selected && !selectedNode) setSelected(null)
  }, [selected, selectedNode])
  const focus = useCallback((id: string) => {
    setSelected(id)
    setFocusOn((current) => ({ id, n: (current?.n ?? 0) + 1 }))
  }, [])
  return { selected, setSelected, selectedNode, focusOn, focus }
}

// The run's graph belongs to the selected agent: it goes with the card.
// Closing it gives the focus back to the button that opened it.
export function useRunPanel(selectedNode: GraphNode | null) {
  const [runPanel, setRunPanel] = useState<RunPanel | null>(null)
  const opener = useRef<HTMLElement | null>(null)
  const panel = runPanel && selectedNode?.agentId === runPanel.agentId ? runPanel : null
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
  return { panel, openPanel, closePanel, setRunPanel }
}

// Escape closes the run's graph, then the card.
export function useEscapeCloses(panel: RunPanel | null, selected: string | null, closePanel: () => void, setSelected: (id: string | null) => void) {
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
  }, [panel, selected, closePanel, setSelected])
}

// An agent the address names is kept until it is on the graph (the page
// may drop it from the address first), then selected, with its run's graph
// open when the address names the run. A stage filter that dims it is
// cleared (see linkedAgentStep).
export function useLinkedAgent({ linked, layout, faded, agents, stageFilter, onStageFilter, focus, setRunPanel }: {
  linked: GraphFocus | null
  layout: AgentGraphLayout
  faded: ReadonlySet<string>
  agents: AgentInfo[]
  stageFilter: WorkStage | null
  onStageFilter?: (stage: WorkStage | null) => void
  focus: (id: string) => void
  setRunPanel: (panel: RunPanel) => void
}) {
  const [target, setTarget] = useState<GraphFocus | null>(null)
  useEffect(() => {
    if (linked?.agentId) setTarget({ agentId: linked.agentId, runId: linked.runId })
  }, [linked?.agentId, linked?.runId])
  useEffect(() => {
    const agentId = target?.agentId
    if (!agentId) return
    const step = linkedAgentStep({
      onGraph: layout.nodes.some((node) => node.id === agentId),
      faded: faded.has(agentId),
      filtering: Boolean(stageFilter && onStageFilter),
      stillInAddress: linked?.agentId === agentId,
      listed: agents.some((agent) => agent.task_id === agentId),
    })
    if (step === "clear-filter") onStageFilter?.(null)
    if (step !== "select") return
    setTarget(null)
    focus(agentId)
    if (target.runId) setRunPanel({ runId: target.runId, agentId })
  }, [target, layout, faded, focus, agents, stageFilter, onStageFilter, linked?.agentId, setRunPanel])
}
