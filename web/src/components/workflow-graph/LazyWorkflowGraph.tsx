import { lazy, Suspense } from "react"
import type { WorkflowGraphProps } from "./WorkflowGraph"
import { LoadingState } from "../ui/LoadingState"

// The graph's own chunk, with React Flow and dagre in it: only the pages
// that draw a workflow graph load it.
const WorkflowGraph = lazy(() => import("./WorkflowGraph"))

export function LazyWorkflowGraph(props: WorkflowGraphProps) {
  return <Suspense fallback={<LoadingState label="Drawing the graph…" size="inline" />}><WorkflowGraph {...props} /></Suspense>
}
