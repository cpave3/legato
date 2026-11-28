// The workflow graph, drawn with React Flow and laid out by dagre. Loaded
// only through LazyWorkflowGraph, so neither library is in the bundle of any
// other page.
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react"
import { Background, BaseEdge, MarkerType, Controls, EdgeLabelRenderer, getBezierPath, Handle, MiniMap, Panel, Position, ReactFlow, ReactFlowProvider, type Edge, type EdgeProps, type EdgeTypes, type NodeProps, type NodeTypes, useReactFlow, useViewportStore } from "@xyflow/react"
import "@xyflow/react/dist/base.css"
import { Link } from "react-router-dom"
import { CircleAlert, CircleCheck, CircleDashed, CircleDot, CirclePause, CircleX, CornerDownRight, SkipForward, Hand, Hourglass, Repeat, Workflow, type LucideIcon } from "lucide-react"
import { stepIcons, triggerIcons } from "./graphIcons"
import { layout, sizes, type FlowNode } from "./graphLayout"
import { chosenOf, isCompact, keyedNode, lookOf, miniColor, withState } from "./graphView"
import { useGraphViewport } from "./useGraphViewport"
import { dataEdgeName, gateLabel, keyValueText, runStateWords, stepTypeLabel, triggerName, triggerOn, type GraphNodeExtra, type NodeRunState } from "../../lib/workflowGraph"
import { type WorkflowGraphModel } from "../../lib/workflow"
import { toneGlyph, toneText } from "../../lib/workflowTones"
import { centerOn as centerView, fitView as fitAll, focusZoom, type Insets } from "../../lib/graphViewport"
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
  // canvas makes the graph a page's background (LEG-651): every fit and
  // centring keeps to the free area the page's panels leave (insets); it
  // opens on focusId (where a run is) when the whole graph would be too
  // small to read; its toolbar has zoom, Fit, Where it is and the page's
  // tools; the minimap colours nodes by state; zoomed out, nodes turn
  // compact.
  canvas?: { insets: Insets; focusId?: string | null; tools?: ReactNode }
  className?: string
}

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
  return <div style={state ? sizes.runStep : sizes.step} className={cn("relative flex flex-col justify-center gap-0.5 rounded-lg border bg-lw-surface px-3 py-2 text-left font-lw text-lw-text shadow-sm group-data-[compact]/flow:justify-start", selected ? "border-lw-accent ring-2 ring-lw-accent-ring" : state ? stateBorders[state] : "border-lw-line-strong", state === "skipped" && "opacity-60")}>
    <Handles direction={data.direction} />
    <CompactTitle title={step.title} state={state} />
    <span className="flex items-center gap-1.5 text-[11px] font-medium uppercase tracking-wide text-lw-text-2 group-data-[compact]/flow:hidden">
      <Icon size={13} aria-hidden="true" />{stepTypeLabel(step.type)}
      {step.gate && <span role="img" aria-label={gateLabel(step.gate)} title={gateLabel(step.gate)} className="ml-auto inline-flex items-center gap-1 rounded-full bg-lw-needs-soft px-1.5 py-px normal-case tracking-normal text-lw-needs">
        {step.gate === "manual" ? <Hand size={11} /> : <Hourglass size={11} />}{step.gate === "manual" ? "you" : "agent ends"}
      </span>}
    </span>
    <span className="truncate text-[13px] font-semibold group-data-[compact]/flow:hidden" title={step.title}>{step.title}</span>
    {step.key.value && <span className="truncate font-lw-mono text-[11px] text-lw-text-2 group-data-[compact]/flow:hidden" title={step.key.value}>{keyValueText(step.key.value)}</span>}
    <span className="contents group-data-[compact]/flow:hidden"><RunLine extra={data.extra} /></span>
    {data.extra?.childRun && <ChildRunLink run={data.extra.childRun} />}
  </div>
}

// ChildRunLink hangs under a workflow.run step that waits on its child run,
// and opens that run; it never selects the node.
function ChildRunLink({ run }: { run: { id: string; label: string } }) {
  return <Link to={`/workflows/runs/${encodeURIComponent(run.id)}`} aria-label={`Child run: ${run.label}`} onClick={event => event.stopPropagation()}
    className="nodrag nopan absolute inset-x-0 top-full mt-2 flex items-center gap-1.5 rounded-md border border-lw-live/35 bg-lw-live-soft px-2.5 py-1.5 text-xs text-lw-text hover:border-lw-live group-data-[compact]/flow:hidden">
    <CornerDownRight size={13} aria-hidden="true" className="shrink-0 text-lw-live" />
    <span className="min-w-0 flex-1 truncate"><span className="font-semibold">Child run</span> · {run.label}</span>
    <span className="text-lw-link">Open</span>
  </Link>
}

// The size of a compact node's text on screen, whatever the zoom, up to
// what a node holds.
const compactText = { fontSize: "min(calc(12px / var(--lw-zoom, 1)), 30px)" }

// CompactTitle is a node's state and title as a zoomed-out canvas draws
// them, at one size on screen; hidden otherwise.
function CompactTitle({ title, state, pass }: { title: string; state?: NodeRunState; pass?: string }) {
  return <span aria-hidden="true" style={compactText} className="hidden items-start gap-[0.4em] leading-tight group-data-[compact]/flow:flex">
    {state && <span className={toneText[stateTones[state]]}>{toneGlyph[stateTones[state]]}</span>}
    <span className="line-clamp-2 font-semibold">{title}{pass && <span className="font-normal text-lw-text-2"> · {pass}</span>}</span>
  </span>
}

function TriggerNode({ data, selected }: NodeProps<FlowNode>) {
  const trigger = data.node.trigger!
  const Icon = triggerIcons[trigger.kind] ?? Workflow
  const manual = trigger.kind === "manual"
  const on = triggerOn(trigger, data.extra)
  const inRun = Boolean(data.extra?.trigger)
  return <div style={sizes.trigger} className={cn("flex items-center gap-2.5 rounded-2xl border border-dashed bg-lw-raised px-3.5 font-lw text-[13px] text-lw-text", selected ? "border-lw-accent ring-2 ring-lw-accent-ring" : data.extra?.trigger === "started" ? "border-lw-accent" : "border-lw-line-strong", !manual && !on && !inRun && "opacity-70")}>
    <Handles direction={data.direction} />
    <Icon size={15} aria-hidden="true" className="shrink-0 text-lw-text-2 group-data-[compact]/flow:hidden" />
    <span aria-hidden="true" style={compactText} className="hidden truncate font-medium group-data-[compact]/flow:block">{trigger.summary || triggerName(trigger.kind)}</span>
    <span className="flex min-w-0 flex-col group-data-[compact]/flow:hidden">
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
    <CompactTitle title={step.title} state={state} />
    <span className="flex items-center gap-1.5 text-[11px] font-medium uppercase tracking-wide text-lw-text-2 group-data-[compact]/flow:hidden">
      <Repeat size={13} aria-hidden="true" />{stepTypeLabel(step.type)}
    </span>
    <span className="truncate text-[13px] font-semibold group-data-[compact]/flow:hidden" title={step.title}>{step.title}</span>
    {step.key.value && <span className="truncate font-lw-mono text-[11px] text-lw-text-2 group-data-[compact]/flow:hidden" title={step.key.value}>{keyValueText(step.key.value)}</span>}
    <span className="contents group-data-[compact]/flow:hidden"><RunLine extra={data.extra} /></span>
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
    <CompactTitle title={step.title} state={state} pass={data.extra?.pass} />
    <span className="flex items-center gap-1.5 text-[11px] font-medium uppercase tracking-wide text-lw-text-2 group-data-[compact]/flow:hidden">
      <Repeat size={13} aria-hidden="true" />{stepTypeLabel(step.type)}
      {step.loop && <span className="normal-case tracking-normal">· at most {step.loop.max}</span>}
      {(data.extra?.passes?.length ?? 0) > 0 && <span className="ml-auto flex items-center gap-1">
        {data.extra!.passes!.map(pass => <span key={pass.pass} role="img" aria-label={`Pass ${pass.pass + 1}: ${runStateWords[pass.state]}`} title={`Pass ${pass.pass + 1}: ${runStateWords[pass.state]}`} className={cn("h-2 w-2 rounded-full border bg-current", toneText[stateTones[pass.state]])} />)}
      </span>}
    </span>
    <span className="truncate text-[13px] font-semibold group-data-[compact]/flow:hidden" title={step.title}>{step.title}</span>
    {(step.loop?.summary || step.loop?.until) && <span className="truncate font-lw-mono text-[11px] text-lw-text-2 group-data-[compact]/flow:hidden" title={[step.loop?.summary, step.loop?.until && `until ${step.loop.until}`].filter(Boolean).join(", ")}>
      {keyValueText([step.loop?.summary, step.loop?.until && `until ${step.loop.until}`].filter(Boolean).join(", "))}
    </span>}
    <span className="contents group-data-[compact]/flow:hidden"><RunLine extra={data.extra} /></span>
    {data.extra?.pass && <span className="text-[11px] text-lw-text-2 group-data-[compact]/flow:hidden">{data.extra.pass}</span>}
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

const toolButton = "inline-flex h-8 min-w-8 items-center justify-center rounded-md px-2 text-xs text-lw-text hover:bg-lw-selected aria-pressed:bg-lw-selected"

type View = ReturnType<typeof useGraphViewport>

// CanvasToolbar is a canvas's view toolbar: zoom, Fit, Where it is (with
// a step to centre on) and the page's tools. On a phone the run's cards
// are a sheet along the bottom, so the toolbar goes to the top.
function CanvasToolbar({ canvas, direction, zoom, view }: { canvas: NonNullable<WorkflowGraphProps["canvas"]>; direction: "LR" | "TB"; zoom: number; view: View }) {
  const flow = useReactFlow()
  const { focusId } = canvas
  const whereItIs = () => { const rect = view.rect(focusId!); if (rect) void flow.setViewport(centerView(rect, view.frameSize(), view.insets, focusZoom)) }
  return <Panel position={direction === "TB" ? "top-right" : "bottom-left"} className="!m-4">
    <div role="toolbar" aria-label="View" className="flex items-center gap-0.5 rounded-lg border border-lw-line bg-lw-surface p-0.5 font-lw text-lw-text shadow-[var(--shadow-lw-panel)]">
      <button type="button" aria-label="Zoom out" className={toolButton} onClick={() => { void flow.zoomOut() }}>−</button>
      <span className="w-11 text-center font-lw-mono text-xs text-lw-text-2">{Math.round(zoom * 100)}%</span>
      <button type="button" aria-label="Zoom in" className={toolButton} onClick={() => { void flow.zoomIn() }}>+</button>
      <span aria-hidden="true" className="mx-1 h-5 w-px bg-lw-line" />
      <button type="button" aria-label="Fit the whole flow" title="Fit the whole flow" className={toolButton} onClick={() => { void flow.setViewport(fitAll(view.tops, view.frameSize(), view.insets)) }}>Fit</button>
      {focusId && <button type="button" title="Centre on the step the run is at" className={toolButton} onClick={whereItIs}>Where it is</button>}
      {canvas.tools && <><span aria-hidden="true" className="mx-1 h-5 w-px bg-lw-line" />{canvas.tools}</>}
    </div>
  </Panel>
}

// GraphMiniMap is the overview of a graph drawn left to right: on a canvas
// coloured by state, clear of the page's right panel.
function GraphMiniMap({ canvas, insets }: { canvas: boolean; insets: { right: number } }) {
  return <MiniMap pannable zoomable ariaLabel="Workflow graph overview"
    nodeColor={canvas ? miniColor : undefined}
    style={canvas ? { width: 200, height: 72, marginRight: 16 + insets.right, marginBottom: 16 } : { width: 160, height: 90 }} className="!rounded-md !border !border-lw-line" />
}

function Graph({ graph, selectedId = null, onSelect, direction = "LR", extras, run = false, centerOn, canvas, className }: WorkflowGraphProps) {
  const flow = useReactFlow()
  // The layout is computed once per graph and direction; a new graph with
  // the same shape keeps its places.
  const shape = useMemo(() => JSON.stringify([direction, run, graph.nodes.map(node => [node.id, node.kind]), graph.edges.map(edge => edge.id)]), [graph, direction, run])
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const placed = useMemo(() => layout(graph, direction, run), [shape])
  const [selected, setSelected] = useState<string | null>(selectedId)
  useEffect(() => { setSelected(selectedId) }, [selectedId])
  const nodes = useMemo(() => withState(placed, graph, selected, extras), [placed, graph, selected, extras])
  const edges = useMemo(() => edgesOf(graph, selected), [graph, selected])
  const frame = useRef<HTMLDivElement>(null)
  const view = useGraphViewport({ flow, placed, frame, canvas, centerOn, selected, direction })
  const choose = useCallback((id: string | null) => {
    setSelected(id)
    onSelect?.(id)
  }, [onSelect])
  // Enter or Space on a focused node selects it, as a click does. The
  // selection is this component's (or its page's) alone: React Flow's own
  // never feeds back into it, so the two cannot chase each other.
  const onKeyDown = useCallback((event: React.KeyboardEvent) => {
    const id = keyedNode(event.key, event.target as HTMLElement)
    if (!id) return
    event.preventDefault()
    choose(chosenOf(graph, id))
  }, [choose, graph])
  const zoom = useViewportStore(state => state.transform[2])
  const compact = isCompact(Boolean(canvas), zoom)
  return <div ref={frame} onKeyDown={onKeyDown} data-compact={compact || undefined} className={cn("group/flow h-full min-h-[18rem] w-full", className)} style={lookOf(Boolean(canvas), zoom) as React.CSSProperties}>
    <ReactFlow
      nodes={nodes}
      edges={edges}
      nodeTypes={nodeTypes}
      edgeTypes={edgeTypes}
      onInit={view.startView}
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
      onNodeClick={(_, node) => choose(chosenOf(graph, node.id))}
      onPaneClick={() => choose(null)}
    >
      <Background gap={20} size={1} />
      {canvas ? <CanvasToolbar canvas={canvas} direction={direction} zoom={zoom} view={view} /> : <Controls showInteractive={false} />}
      {direction === "LR" && <GraphMiniMap canvas={Boolean(canvas)} insets={view.insets} />}
    </ReactFlow>
  </div>
}

export default function WorkflowGraph(props: WorkflowGraphProps) {
  return <ReactFlowProvider><Graph {...props} /></ReactFlowProvider>
}
