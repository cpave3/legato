import type { WorkflowGraphModel } from "../../lib/workflow"
import { LazyWorkflowGraph } from "../workflow-graph/LazyWorkflowGraph"
import { WorkflowCanvas } from "../workflow-canvas/WorkflowCanvas"

export type OutlineView = "list" | "map"

export const STALE_WORDS = "The graph is of the last version Legato could read."

/** ViewSwitch chooses the outline as a list or as the graph (Graph only
 * once there is one). */
export function ViewSwitch({ view, onView, hasGraph }: { view: OutlineView; onView: (view: OutlineView) => void; hasGraph: boolean }) {
  return <div role="radiogroup" aria-label="Outline view" className="inline-flex self-start rounded-md border border-lw-line p-0.5">
    {(["list", "map"] as const).map(option => {
      const off = option === "map" && !hasGraph
      return <button key={option} type="button" role="radio" aria-checked={view === option} disabled={off}
        title={off ? "The graph shows once Legato has read the file" : undefined}
        className={`min-h-7 rounded px-2.5 text-xs disabled:opacity-40 ${view === option ? "bg-lw-raised text-lw-text" : "text-lw-text-2"}`} onClick={() => onView(option)}>{option === "list" ? "List" : "Graph"}</button>
    })}
  </div>
}

type MapProps = { graph: { model: WorkflowGraphModel; stale: boolean }; selectedId: string | null; onSelect: (id: string | null) => void }

/** PhoneMap is Graph on a phone: the canvas in the outline's place, top to
 * bottom. */
export function PhoneMap({ graph, selectedId, onSelect }: MapProps) {
  return <div className="relative flex h-[70vh] flex-col overflow-hidden rounded-lg border border-lw-line">
    <WorkflowCanvas label="Graph" narrow graph={{ graph: graph.model, selectedId, onSelect, direction: "TB" }}
      notices={graph.stale && <p role="status" className="text-xs text-lw-needs">{STALE_WORDS}</p>} selected={false} onClosePanel={() => {}} hiddenPill="Graph" />
  </div>
}

/** OutlineMap is Graph beside the inspector: the graph in the outline's
 * place, saying when it is of an older text. */
export function OutlineMap({ graph, selectedId, onSelect, narrow }: MapProps & { narrow: boolean }) {
  return <section aria-label="Graph" className="flex flex-col gap-1">
    {graph.stale && <p role="status" className="text-xs text-lw-needs">{STALE_WORDS}</p>}
    <div className={narrow ? "h-[60vh] overflow-hidden rounded-lg border border-lw-line bg-lw-ground" : "h-[32rem] overflow-hidden rounded-lg border border-lw-line bg-lw-ground"}>
      <LazyWorkflowGraph graph={graph.model} selectedId={selectedId} direction="TB" onSelect={onSelect} />
    </div>
  </section>
}
