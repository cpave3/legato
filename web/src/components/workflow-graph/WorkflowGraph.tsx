// The workflow graph, drawn with React Flow and laid out by dagre. Loaded
// only through LazyWorkflowGraph, so neither library is in the bundle of any
// other page.
import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import dagre from "@dagrejs/dagre"
import { Background, BaseEdge, MarkerType, Controls, EdgeLabelRenderer, getBezierPath, Handle, MiniMap, Position, ReactFlow, ReactFlowProvider, type Edge, type Node, type EdgeProps, type EdgeTypes, type NodeProps, type NodeTypes, type ReactFlowInstance, useReactFlow } from "@xyflow/react"
import "@xyflow/react/dist/base.css"
import { CircleAlert, CircleCheck, CircleDashed, CircleDot, CirclePause, CircleX, SkipForward, Hand, Hourglass, Repeat, Workflow, type LucideIcon } from "lucide-react"
import { stepIcons, triggerIcons } from "./graphIcons"
import { dataEdgeName, gateLabel, keyValueText, nodeName, runStateWords, stepTypeLabel, triggerName, triggerOn, type GraphNode, type GraphNodeExtra, type NodeRunState, type WorkflowGraphModel } from "../../lib/workflowGraph"
import { toneText } from "../../lib/workflowTones"
import type { Tone } from "../../lib/workflowView"
import { cn } from "../../lib/utils"

export type WorkflowGraphProps = {
  graph: WorkflowGraphModel
  selectedId?: string | null
  onSelect?: (id: string | null) => void
  // direction is LR on wide screens, TB at phone width.
  direction?: "LR" | "TB"
  // extras is what the page knows of each node, by node ID.
  extras?: Record<string, GraphNodeExtra>
  // run makes step nodes tall enough for a run's state line.
  run?: boolean
  // centerOn moves the view to a node, each time n changes.
  centerOn?: { id: string; n: number } | null
  className?: string
}

// Node sizes come from fixed classes, so the layout never waits for the
// nodes to be measured.
const sizes = { step: { width: 240, height: 76 }, runStep: { width: 240, height: 98 }, trigger: { width: 220, height: 60 }, loop: { width: 340, height: 168 }, runLoop: { width: 340, height: 188 }, inner: { width: 132, height: 44 } } as const
// Where a group's inner nodes sit inside it, by role.
const innerPlaces: Record<string, { x: number; y: number }> = { pass: { x: 20, y: 72 }, fixes: { x: 340 - 20 - 132, y: 72 } }

type Size = { width: number; height: number }

// A loop (graph version 2) is a box around its body's steps: its header
// above them, a margin round them, at least a step's width.
const repeatBox = { header: 92, runHeader: 112, pad: 16, minWidth: 280 }

function sizeOf(node: GraphNode, run = false): Size {
  if (node.kind === "inner") return sizes.inner
  if (node.kind === "step" && node.step?.group === "review_loop") return run ? sizes.runLoop : sizes.loop
  if (node.kind === "step" && node.step?.group === "loop") return { width: repeatBox.minWidth, height: run ? repeatBox.runHeader : repeatBox.header }
  return node.kind === "trigger" ? sizes.trigger : run ? sizes.runStep : sizes.step
}

// isRepeat is a loop of graph version 2, drawn as a box holding its body.
const isRepeat = (node: GraphNode | undefined) => node?.kind === "step" && node.step?.group === "loop"

const stateTones: Record<NodeRunState, Tone> = { waiting: "idle", running: "live", needs: "needs", failed: "error", done: "done", skipped: "idle", suspended: "idle" }
const stateIcons: Record<NodeRunState, LucideIcon> = { waiting: CircleDashed, running: CircleDot, needs: CircleAlert, failed: CircleX, done: CircleCheck, skipped: SkipForward, suspended: CirclePause }
const stateBorders: Record<NodeRunState, string> = { waiting: "border-lw-line-strong", running: "border-lw-live", needs: "border-lw-needs", failed: "border-lw-error", done: "border-lw-done/60", skipped: "border-lw-line", suspended: "border-lw-line-strong" }

// RunLine is a step's state in a run: the word with its icon and colour,
// how long it has run, its attempts folded into a count, its agent tool.
function RunLine({ extra }: { extra?: GraphNodeExtra }) {
  if (!extra?.state) return null
  const Icon = stateIcons[extra.state]
  return <span className="mt-0.5 flex items-center gap-2 text-[11px] text-lw-text-2">
    <span className={cn("inline-flex items-center gap-1 font-medium", toneText[stateTones[extra.state]])}><Icon size={12} aria-hidden="true" />{runStateWords[extra.state]}</span>
    {extra.elapsed && <span className="font-lw-mono">{extra.elapsed}</span>}
    {(extra.attempts ?? 0) > 1 && <span title={`${extra.attempts} attempts`} className="font-lw-mono">×{extra.attempts}</span>}
    {extra.agent && <span className="ml-auto truncate font-lw-mono">{extra.agent}</span>}
  </span>
}

type FlowData = { node: GraphNode; extra?: GraphNodeExtra; direction: "LR" | "TB" }
type FlowNode = Node<FlowData>

function Handles({ direction }: { direction: "LR" | "TB" }) {
  const [target, source] = direction === "LR" ? [Position.Left, Position.Right] : [Position.Top, Position.Bottom]
  return <>
    <Handle type="target" position={target} isConnectable={false} className="!h-1.5 !w-1.5 !border-0 !bg-lw-line-strong" />
    <Handle type="source" position={source} isConnectable={false} className="!h-1.5 !w-1.5 !border-0 !bg-lw-line-strong" />
  </>
}

function StepNode({ data, selected }: NodeProps<FlowNode>) {
  const step = data.node.step!
  const Icon = stepIcons[step.type] ?? Workflow
  const state = data.extra?.state
  return <div style={state ? sizes.runStep : sizes.step} className={cn("flex flex-col justify-center gap-0.5 rounded-lg border bg-lw-surface px-3 py-2 text-left font-lw text-lw-text shadow-sm", selected ? "border-lw-accent ring-2 ring-lw-accent-ring" : state ? stateBorders[state] : "border-lw-line-strong", state === "skipped" && "opacity-60")}>
    <Handles direction={data.direction} />
    <span className="flex items-center gap-1.5 text-[11px] font-medium uppercase tracking-wide text-lw-text-2">
      <Icon size={13} aria-hidden="true" />{stepTypeLabel(step.type)}
      {step.gate && <span role="img" aria-label={gateLabel(step.gate)} title={gateLabel(step.gate)} className="ml-auto inline-flex items-center gap-1 rounded-full bg-lw-needs-soft px-1.5 py-px normal-case tracking-normal text-lw-needs">
        {step.gate === "manual" ? <Hand size={11} /> : <Hourglass size={11} />}{step.gate === "manual" ? "you" : "agent ends"}
      </span>}
    </span>
    <span className="truncate text-[13px] font-semibold" title={step.title}>{step.title}</span>
    {step.key.value && <span className="truncate font-lw-mono text-[11px] text-lw-text-2" title={step.key.value}>{keyValueText(step.key.value)}</span>}
    <RunLine extra={data.extra} />
  </div>
}

function TriggerNode({ data, selected }: NodeProps<FlowNode>) {
  const trigger = data.node.trigger!
  const Icon = triggerIcons[trigger.kind] ?? Workflow
  const manual = trigger.kind === "manual"
  const on = triggerOn(trigger, data.extra)
  const inRun = Boolean(data.extra?.trigger)
  return <div style={sizes.trigger} className={cn("flex items-center gap-2.5 rounded-2xl border border-dashed bg-lw-raised px-3.5 font-lw text-[13px] text-lw-text", selected ? "border-lw-accent ring-2 ring-lw-accent-ring" : data.extra?.trigger === "started" ? "border-lw-accent" : "border-lw-line-strong", !manual && !on && !inRun && "opacity-70")}>
    <Handles direction={data.direction} />
    <Icon size={15} aria-hidden="true" className="shrink-0 text-lw-text-2" />
    <span className="flex min-w-0 flex-col">
      <span className="truncate font-medium" title={trigger.summary}>{trigger.summary || triggerName(trigger.kind)}</span>
      {data.extra?.trigger === "started" && <span className="text-[11px] font-medium text-lw-accent-text">Started this run</span>}
      {!manual && !inRun && <span className="flex items-center gap-1.5 text-[11px] text-lw-text-2">
        <span aria-hidden="true" className={cn("h-1.5 w-1.5 rounded-full", on ? "bg-lw-live" : "bg-lw-faint")} />
        <span className={on ? "text-lw-live" : undefined}>{on ? "on here" : "paused here"}</span>
        {trigger.max_runs_per_day ? <span title={`Loop stop: ${trigger.max_runs_per_day} runs per item per day`} className="rounded border border-lw-line px-1 font-lw-mono text-[10px]">{trigger.max_runs_per_day}/day</span> : null}
      </span>}
    </span>
  </div>
}

// LoopNode is a step drawn as a group: the review loop, a review pass and
// the author's fixes inside it, round and round until a pass approves.
function LoopNode({ data, selected }: NodeProps<FlowNode>) {
  const step = data.node.step!
  const state = data.extra?.state
  return <div style={state ? sizes.runLoop : sizes.loop} className={cn("relative flex flex-col gap-0.5 rounded-xl border border-dashed bg-lw-surface/60 px-3 py-2 text-left font-lw text-lw-text", selected ? "border-lw-accent ring-2 ring-lw-accent-ring" : state ? stateBorders[state] : "border-lw-line-strong")}>
    <Handles direction={data.direction} />
    <span className="flex items-center gap-1.5 text-[11px] font-medium uppercase tracking-wide text-lw-text-2">
      <Repeat size={13} aria-hidden="true" />{stepTypeLabel(step.type)}
    </span>
    <span className="truncate text-[13px] font-semibold" title={step.title}>{step.title}</span>
    {step.key.value && <span className="truncate font-lw-mono text-[11px] text-lw-text-2" title={step.key.value}>{keyValueText(step.key.value)}</span>}
    <RunLine extra={data.extra} />
    {data.extra?.pass && <span className="absolute bottom-1.5 left-1/2 -translate-x-1/2 rounded border border-lw-line bg-lw-surface px-1.5 py-px font-lw-mono text-[10px] text-lw-text">{data.extra.pass}</span>}
  </div>
}

// RepeatNode is a loop (graph version 2): a box holding its pass's steps,
// with what it goes over, its state, and a mark for each pass so far.
function RepeatNode({ data, selected, width, height }: NodeProps<FlowNode>) {
  const step = data.node.step!
  const state = data.extra?.state
  return <div style={{ width, height }} className={cn("flex flex-col gap-0.5 rounded-xl border border-dashed bg-lw-surface/40 px-3 py-2 text-left font-lw text-lw-text", selected ? "border-lw-accent ring-2 ring-lw-accent-ring" : state ? stateBorders[state] : "border-lw-line-strong", state === "skipped" && "opacity-60")}>
    <Handles direction={data.direction} />
    <span className="flex items-center gap-1.5 text-[11px] font-medium uppercase tracking-wide text-lw-text-2">
      <Repeat size={13} aria-hidden="true" />{stepTypeLabel(step.type)}
      {step.loop && <span className="normal-case tracking-normal">· at most {step.loop.max}</span>}
      {(data.extra?.passes?.length ?? 0) > 0 && <span className="ml-auto flex items-center gap-1">
        {data.extra!.passes!.map(pass => <span key={pass.pass} role="img" aria-label={`Pass ${pass.pass + 1}: ${runStateWords[pass.state]}`} title={`Pass ${pass.pass + 1}: ${runStateWords[pass.state]}`} className={cn("h-2 w-2 rounded-full border bg-current", toneText[stateTones[pass.state]])} />)}
      </span>}
    </span>
    <span className="truncate text-[13px] font-semibold" title={step.title}>{step.title}</span>
    {(step.loop?.summary || step.loop?.until) && <span className="truncate font-lw-mono text-[11px] text-lw-text-2" title={[step.loop?.summary, step.loop?.until && `until ${step.loop.until}`].filter(Boolean).join(", ")}>
      {keyValueText([step.loop?.summary, step.loop?.until && `until ${step.loop.until}`].filter(Boolean).join(", "))}
    </span>}
    <RunLine extra={data.extra} />
    {data.extra?.pass && <span className="text-[11px] text-lw-text-2">{data.extra.pass}</span>}
  </div>
}

function InnerNode({ data }: NodeProps<FlowNode>) {
  return <div style={sizes.inner} className="flex items-center justify-center rounded-md border border-lw-line-strong bg-lw-raised px-2 text-center font-lw text-xs text-lw-text">
    <Handle id="r" type="source" position={Position.Right} isConnectable={false} className="!h-1 !w-1 !border-0 !bg-transparent" />
    <Handle id="l" type="target" position={Position.Left} isConnectable={false} className="!h-1 !w-1 !border-0 !bg-transparent" />
    <Handle id="bs" type="source" position={Position.Bottom} isConnectable={false} className="!h-1 !w-1 !border-0 !bg-transparent" />
    <Handle id="bt" type="target" position={Position.Bottom} isConnectable={false} className="!h-1 !w-1 !border-0 !bg-transparent" />
    {data.node.inner?.title}
  </div>
}

const nodeTypes: NodeTypes = { step: StepNode, trigger: TriggerNode, loop: LoopNode, repeat: RepeatNode, inner: InnerNode }

type LoopEdgeData = { back: boolean }

// LoopEdge joins the inner nodes of a loop: straight on to the author's
// fixes, and curved back under them to the next pass. In a run the loop
// node labels the curve with the pass, since a group draws over edge labels.
function LoopEdge({ id, sourceX, sourceY, targetX, targetY, data, markerEnd }: EdgeProps & { data?: LoopEdgeData }) {
  const back = Boolean(data?.back)
  const path = back ? `M ${sourceX} ${sourceY} C ${sourceX} ${sourceY + 38}, ${targetX} ${targetY + 38}, ${targetX} ${targetY}` : `M ${sourceX} ${sourceY} L ${targetX} ${targetY}`
  return <>
    <BaseEdge id={id} path={path} markerEnd={markerEnd} style={{ stroke: "var(--color-lw-text-2)", strokeWidth: 1.5 }} />
  </>
}

type DataEdgeData = { outputs: string[] }

// DataEdge joins a step to a later one that uses its outputs, dashed and
// labelled with the output keys. It is drawn only while one of its ends is
// selected, and never shapes the layout.
function DataEdge({ id, sourceX, sourceY, targetX, targetY, sourcePosition, targetPosition, data }: EdgeProps & { data?: DataEdgeData }) {
  const [path, labelX, labelY] = getBezierPath({ sourceX, sourceY, targetX, targetY, sourcePosition, targetPosition, curvature: 0.5 })
  return <>
    <BaseEdge id={id} path={path} style={{ stroke: "var(--color-lw-link)", strokeDasharray: "5 4", strokeWidth: 1.5 }} />
    <EdgeLabelRenderer>
      <span style={{ transform: `translate(-50%, -50%) translate(${labelX}px, ${labelY - 14}px)` }} className="nodrag nopan pointer-events-none absolute rounded bg-lw-ground px-1 font-lw-mono text-[10px] text-lw-link">{(data?.outputs ?? []).join(", ")}</span>
    </EdgeLabelRenderer>
  </>
}

type BranchEdgeData = { label: string; skip: boolean }

// BranchEdge leads into a step with if:, labelled with its condition, or
// (dashed) past it to where the run goes when it is skipped.
function BranchEdge({ id, sourceX, sourceY, targetX, targetY, sourcePosition, targetPosition, data, markerEnd }: EdgeProps & { data?: BranchEdgeData }) {
  const [path, labelX, labelY] = getBezierPath({ sourceX, sourceY, targetX, targetY, sourcePosition, targetPosition, curvature: data?.skip ? 0.6 : 0.25 })
  return <>
    <BaseEdge id={id} path={path} markerEnd={markerEnd} style={{ stroke: "var(--color-lw-line-strong)", strokeWidth: 1.5, strokeDasharray: data?.skip ? "4 4" : undefined }} />
    <EdgeLabelRenderer>
      <span style={{ transform: `translate(-50%, -50%) translate(${labelX}px, ${labelY}px)` }} title={data?.label} className="nodrag nopan pointer-events-none absolute max-w-48 truncate rounded border border-lw-line bg-lw-ground px-1 font-lw-mono text-[10px] text-lw-text-2">{data?.label}</span>
    </EdgeLabelRenderer>
  </>
}

const edgeTypes: EdgeTypes = { data: DataEdge, loop: LoopEdge, branch: BranchEdge }

// shapes is whether an edge shapes the layout: start, order and a branch
// into a step, between nodes of one scope.
const shapes = (edge: GraphWorkflowEdge) => edge.kind === "start" || edge.kind === "order" || (edge.kind === "branch" && !edge.skip)
type GraphWorkflowEdge = WorkflowGraphModel["edges"][number]

// layout places the nodes once per graph. Only the edges that shape it
// (shapes) count; the other kinds are drawn over the result. A loop of
// graph version 2 is laid out from the inside out: its body first, as a
// graph of its own, whose size makes the loop's box.
function layout(graph: WorkflowGraphModel, direction: "LR" | "TB", run: boolean): Omit<FlowNode, "ariaLabel">[] {
  const byId = new Map(graph.nodes.map(node => [node.id, node]))
  const sized = new Map<string, Size>()
  const relative = new Map<string, { x: number; y: number }>()
  // A scope's nodes: the run's own (no parent), or a loop's body.
  const scopeOf = (node: GraphNode) => node.kind === "inner" ? null : node.parent && isRepeat(byId.get(node.parent)) ? node.parent : ""
  const place = (scope: string): Size => {
    const members = graph.nodes.filter(node => scopeOf(node) === scope)
    for (const member of members) if (isRepeat(member)) {
      const body = place(member.id)
      const header = run ? repeatBox.runHeader : repeatBox.header
      sized.set(member.id, body.width === 0 ? sizeOf(member, run) : { width: Math.max(repeatBox.minWidth, body.width + 2 * repeatBox.pad), height: header + body.height + repeatBox.pad })
    }
    if (members.length === 0) return { width: 0, height: 0 }
    const g = new dagre.graphlib.Graph()
    g.setGraph({ rankdir: direction, nodesep: 28, ranksep: 56, marginx: 0, marginy: 0 })
    g.setDefaultEdgeLabel(() => ({}))
    for (const member of members) g.setNode(member.id, { ...(sized.get(member.id) ?? sizeOf(member, run)) })
    const inScope = new Set(members.map(member => member.id))
    for (const edge of graph.edges) if (shapes(edge) && inScope.has(edge.source) && inScope.has(edge.target)) g.setEdge(edge.source, edge.target)
    dagre.layout(g)
    let maxX = 0, maxY = 0
    for (const member of members) {
      const at = g.node(member.id)
      const size = sized.get(member.id) ?? sizeOf(member, run)
      const x = at.x - size.width / 2, y = at.y - size.height / 2
      relative.set(member.id, scope === "" ? { x: x + 8, y: y + 8 } : { x: x + repeatBox.pad, y: y + (run ? repeatBox.runHeader : repeatBox.header) })
      maxX = Math.max(maxX, x + size.width)
      maxY = Math.max(maxY, y + size.height)
    }
    return { width: maxX, height: maxY }
  }
  place("")
  return graph.nodes.map(node => {
    const size = sized.get(node.id) ?? sizeOf(node, run)
    if (node.kind === "inner") return {
      id: node.id,
      type: "inner",
      parentId: node.parent,
      extent: "parent" as const,
      position: { ...(innerPlaces[node.inner?.role ?? ""] ?? { x: 0, y: 0 }), ...(run ? { y: innerPlaces.pass.y + 20 } : {}) },
      width: size.width,
      height: size.height,
      data: { node, direction },
      draggable: false,
      connectable: false,
      selectable: false,
      focusable: false,
      ariaRole: "img",
    }
    const inLoop = node.parent && isRepeat(byId.get(node.parent))
    return {
      id: node.id,
      type: isRepeat(node) ? "repeat" : node.kind === "step" && node.step?.group ? "loop" : node.kind,
      ...(inLoop ? { parentId: node.parent, extent: "parent" as const } : {}),
      position: relative.get(node.id) ?? { x: 0, y: 0 },
      width: size.width,
      height: size.height,
      data: { node, direction },
      draggable: false,
      connectable: false,
      ariaRole: "button",
    }
  })
}

function edgesOf(graph: WorkflowGraphModel, selected: string | null): Edge[] {
  const title = (id: string) => graph.nodes.find(node => node.id === id)?.step?.title ?? id
  const data: Edge[] = selected === null ? [] : graph.edges.filter(edge => edge.kind === "data" && (edge.source === selected || edge.target === selected)).map(edge => ({
    id: edge.id,
    source: edge.source,
    target: edge.target,
    type: "data",
    data: { outputs: edge.outputs ?? [] },
    focusable: false,
    selectable: false,
    ariaLabel: dataEdgeName(edge, title),
  }))
  // A loop's own edges (into its pass, back from it) are drawn by its box;
  // the review loop's join its inner nodes.
  const inner = (id: string) => graph.nodes.find(node => node.id === id)?.kind === "inner"
  const loops: Edge[] = graph.edges.filter(edge => edge.kind === "loop" && inner(edge.source) && inner(edge.target)).map(edge => {
    const back = graph.nodes.find(node => node.id === edge.source)?.inner?.role === "fixes"
    return {
      id: edge.id,
      source: edge.source,
      target: edge.target,
      sourceHandle: back ? "bs" : "r",
      targetHandle: back ? "bt" : "l",
      type: "loop",
      data: { back },
      markerEnd: { type: MarkerType.ArrowClosed, width: 14, height: 14, color: "var(--color-lw-text-2)" },
      focusable: false,
      selectable: false,
      zIndex: 1,
      ariaLabel: back ? "Author fixes, then another review pass" : "Review pass, then author fixes",
    }
  })
  // A branch past a body's last step leads to its own loop, which its box
  // already says.
  const parentOf = (id: string) => graph.nodes.find(node => node.id === id)?.parent
  const branches: Edge[] = graph.edges.filter(edge => edge.kind === "branch" && parentOf(edge.source) !== edge.target).map(edge => ({
    id: edge.id,
    source: edge.source,
    target: edge.target,
    type: "branch",
    data: { label: edge.skip ? "skipped" : `if ${edge.condition ?? ""}`, skip: Boolean(edge.skip) },
    markerEnd: { type: MarkerType.ArrowClosed, width: 14, height: 14, color: "var(--color-lw-line-strong)" },
    focusable: false,
    selectable: false,
    ariaLabel: edge.skip ? `On to ${title(edge.target)} when a step is skipped` : `If ${edge.condition}, then ${title(edge.target)}`,
  }))
  return [...loops, ...graph.edges.filter(edge => edge.kind === "start" || edge.kind === "order").map(edge => ({
    id: edge.id,
    source: edge.source,
    target: edge.target,
    focusable: false,
    selectable: false,
  })), ...branches, ...data]
}

function Graph({ graph, selectedId = null, onSelect, direction = "LR", extras, run = false, centerOn, className }: WorkflowGraphProps) {
  const flow = useReactFlow()
  // The layout is computed once per graph and direction; a new graph with
  // the same shape keeps its places.
  const shape = useMemo(() => JSON.stringify([direction, run, graph.nodes.map(node => [node.id, node.kind]), graph.edges.map(edge => edge.id)]), [graph, direction, run])
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const placed = useMemo(() => layout(graph, direction, run), [shape])
  const [selected, setSelected] = useState<string | null>(selectedId)
  useEffect(() => { setSelected(selectedId) }, [selectedId])
  const nodes = useMemo(() => {
    const byId = new Map(graph.nodes.map(node => [node.id, node]))
    return placed.map(node => {
      const current = byId.get(node.id) ?? node.data.node
      const extra = extras?.[node.id]
      return { ...node, data: { ...node.data, node: current, extra }, ariaLabel: nodeName(current, extra), selected: node.id === selected, className: extra?.trigger === "other" ? "opacity-50" : undefined }
    })
  }, [placed, graph, selected, extras])
  const edges = useMemo(() => edgesOf(graph, selected), [graph, selected])
  const frame = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!centerOn) return
    const node = placed.find(item => item.id === centerOn.id)
    if (!node) return
    // At once, not animated: no motion to sit through, and React Flow 13's
    // animated move never settled under the tests' jsdom.
    void flow.setCenter(node.position.x + (node.width ?? 0) / 2, node.position.y + (node.height ?? 0) / 2, { zoom: Math.max(flow.getZoom(), 0.9) })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [centerOn?.n])
  // startView fits the graph when it fits at a readable size, else shows it
  // from its start (the triggers) at that size, for the operator to pan.
  const startView = useCallback((flow: Pick<ReactFlowInstance, "setViewport">) => {
    const width = frame.current?.clientWidth ?? 0, height = frame.current?.clientHeight ?? 0
    const xs = placed.flatMap(node => [node.position.x, node.position.x + (node.width ?? 0)])
    const ys = placed.flatMap(node => [node.position.y, node.position.y + (node.height ?? 0)])
    const [minX, maxX, minY, maxY] = [Math.min(...xs), Math.max(...xs), Math.min(...ys), Math.max(...ys)]
    const pad = 24, boundsW = maxX - minX, boundsH = maxY - minY
    // Top to bottom (a phone) the width is what must fit, so it may shrink
    // further; a long chain still scrolls down.
    const floor = direction === "TB" ? 0.6 : 0.75
    const zoom = Math.min(1, Math.max(floor, Math.min((width - 2 * pad) / boundsW, (height - 2 * pad) / boundsH)))
    const x = boundsW * zoom <= width - 2 * pad ? (width - boundsW * zoom) / 2 - minX * zoom : pad - minX * zoom
    const y = boundsH * zoom <= height - 2 * pad ? (height - boundsH * zoom) / 2 - minY * zoom : pad - minY * zoom
    void flow.setViewport({ x, y, zoom })
  }, [placed, direction])
  const choose = useCallback((id: string | null) => {
    setSelected(id)
    onSelect?.(id)
  }, [onSelect])
  // Enter or Space on a focused node selects it, as a click does. The
  // selection is this component's (or its page's) alone: React Flow's own
  // never feeds back into it, so the two cannot chase each other.
  const onKeyDown = useCallback((event: React.KeyboardEvent) => {
    if (event.key !== "Enter" && event.key !== " ") return
    const element = (event.target as HTMLElement).closest<HTMLElement>(".react-flow__node")
    const id = element?.dataset.id
    if (!id) return
    event.preventDefault()
    const node = graph.nodes.find(item => item.id === id)
    choose(node?.kind === "inner" && node.parent ? node.parent : id)
  }, [choose, graph])
  return <div ref={frame} onKeyDown={onKeyDown} className={cn("h-full min-h-[18rem] w-full", className)} style={{ "--xy-background-color": "var(--color-lw-ground)", "--xy-edge-stroke": "var(--color-lw-line-strong)", "--xy-minimap-background-color": "var(--color-lw-surface)", "--xy-minimap-mask-background-color": "color-mix(in srgb, var(--color-lw-ground) 70%, transparent)", "--xy-minimap-mask-stroke-color": "var(--color-lw-line-strong)", "--xy-minimap-node-background-color": "var(--color-lw-line-strong)", "--xy-controls-button-background-color": "var(--color-lw-surface)", "--xy-controls-button-color": "var(--color-lw-text)", "--xy-controls-button-border-color": "var(--color-lw-line)" } as React.CSSProperties}>
    <ReactFlow
      nodes={nodes}
      edges={edges}
      nodeTypes={nodeTypes}
      edgeTypes={edgeTypes}
      onInit={startView}
      minZoom={0.2}
      maxZoom={1.75}
      nodesDraggable={false}
      nodesConnectable={false}
      elementsSelectable
      nodesFocusable
      multiSelectionKeyCode={null}
      selectionKeyCode={null}
      deleteKeyCode={null}
      zoomOnDoubleClick={false}
      onNodeClick={(_, node) => choose(node.type === "inner" && node.parentId ? node.parentId : node.id)}
      onPaneClick={() => choose(null)}
    >
      <Background gap={20} size={1} />
      <Controls showInteractive={false} />
      {direction === "LR" && <MiniMap pannable zoomable ariaLabel="Workflow graph overview" style={{ width: 160, height: 90 }} className="!rounded-md !border !border-lw-line" />}
    </ReactFlow>
  </div>
}

export default function WorkflowGraph(props: WorkflowGraphProps) {
  return <ReactFlowProvider><Graph {...props} /></ReactFlowProvider>
}
