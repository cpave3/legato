// AgentGraphView is the ensemble's Graph layout (LEG-537): every running
// agent on one graph, boxed by repository, its state as its border, its
// sub-agents branching off it, and the links between agents that
// coordinate. A click (or Enter) selects an agent, lights what it is linked
// to and opens a card to open or message it; a double-click opens it. An
// agent that works for a workflow run shows the run's step on its node, the
// run in its card, and, on asking, the run's own graph (LEG-612).
import { useMemo, type ComponentProps } from "react"
import { type AgentInfo } from "../lib/agentWire"
import { useServer } from "../hooks/useServer"
import { useMediaQuery } from "../hooks/useMediaQuery"
import { linkKinds, type AgentGraphLayout } from "../lib/agentGraph"
import { graphNeedsYou, graphStageView, pendingAgents } from "../lib/agentGraphFilters"
import { needsYouOutside, type WorkStage } from "../lib/agentStage"
import { LazyAgentGraphCanvas } from "./agent-graph/LazyAgentGraphCanvas"
import { RunGraphPanel } from "./agent-graph/RunGraphPanel"
import { LoadingState } from "./ui/LoadingState"
import { GraphToolbar, SelectionCard } from "./AgentGraphParts"
import { useEscapeCloses, useGraphLayout, useGraphRead, useGraphSelection, useLinkedAgent, useRunChanges, useRunPanel, type GraphFocus, type RunPanel } from "./useAgentGraph"

export type { GraphFocus } from "./useAgentGraph"

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

// The graph itself, once the first read has settled, or why there is none.
function GraphCanvas({ read, agents, ...canvas }: { read: boolean; agents: AgentInfo[] } & ComponentProps<typeof LazyAgentGraphCanvas>) {
  if (!read) return <LoadingState label="Reading the graph…" size="inline" />
  if (agents.length === 0) return <p className="py-12 text-center text-lw-faint">No agents running.</p>
  return <LazyAgentGraphCanvas {...canvas} />
}

// Over the graph, which stays drawn (dimmed) so its view is kept, when the
// filter dims every agent.
function NoneAtStage({ show }: { show: boolean }) {
  if (!show) return null
  return <p className="pointer-events-none absolute inset-x-0 top-3 text-center text-lw-faint">No agents at this stage.</p>
}

// The run's graph beside the fleet, named by the run's workflow when the
// selected agent's node knows it.
function RunPanelFor({ panel, layout, selectedId, phone, onClose }: { panel: RunPanel | null; layout: AgentGraphLayout; selectedId: string | null; phone: boolean; onClose: () => void }) {
  if (!panel) return null
  const workflow = layout.nodes.find((node) => node.id === selectedId)?.workflows?.find((candidate) => candidate.run.id === panel.runId)
  return <RunGraphPanel runId={panel.runId} title={workflow?.run.workflow_title ?? panel.runId} stepId={workflow?.place.step_id} phone={phone} onClose={onClose} />
}

export function AgentGraphView({ agents, onOpen, onSend, stageFilter = null, onStageFilter, focus: linked = null }: AgentGraphViewProps) {
  const { baseUrl } = useServer()
  const phone = useMediaQuery("(max-width: 767px)")
  const direction = phone ? "TB" : "LR"
  const moved = useRunChanges()
  const { data, readFor, read, now } = useGraphRead(baseUrl, agents, moved)
  const { counted, atStage, faded } = useMemo(() => graphStageView(agents, stageFilter), [agents, stageFilter])
  const pending = useMemo(() => pendingAgents(agents, readFor, data), [agents, readFor, data])
  const { layout, fit, fitGraph, onToggleFold, onToggleBox } = useGraphLayout(agents, data, read, now, direction, pending)
  const { selected, setSelected, selectedNode, focusOn, focus } = useGraphSelection(layout)
  const { panel, openPanel, closePanel, setRunPanel } = useRunPanel(selectedNode)
  useEscapeCloses(panel, selected, closePanel, setSelected)
  useLinkedAgent({ linked, layout, faded, agents, stageFilter, onStageFilter, focus, setRunPanel })

  return (
    <div data-agent-graph className="relative flex h-full min-h-0 flex-col">
      <GraphToolbar counted={counted} stageFilter={stageFilter} onStageFilter={onStageFilter} phone={phone} needsYou={graphNeedsYou(atStage)}
        hiddenNeeds={needsYouOutside(counted, stageFilter)} runId={linked?.runId} kinds={linkKinds.filter((kind) => layout.edges.some((edge) => edge.kind === kind))}
        onFocus={focus} onFit={fitGraph} />
      <div className="relative min-h-0 flex-1">
        <GraphCanvas read={read} agents={agents} layout={layout} direction={direction} selectedId={selected} onSelect={setSelected} onOpen={onOpen}
          onToggleFold={onToggleFold} onToggleBox={onToggleBox} focusOn={focusOn} fit={fit} faded={faded} />
        <NoneAtStage show={read && Boolean(stageFilter) && counted.length > 0 && faded.size === counted.length} />
        {selectedNode && !(phone && panel) && (
          <SelectionCard node={selectedNode} layout={layout} agents={agents} phone={phone} onOpen={onOpen} onSend={onSend} onClose={() => setSelected(null)}
            onShowRun={(runId, button) => openPanel(runId, selectedNode.agentId, button)} />
        )}
        <RunPanelFor panel={panel} layout={layout} selectedId={selected} phone={phone} onClose={closePanel} />
      </div>
    </div>
  )
}
