import { lazy, Suspense } from "react"
import type { WorkflowMapCanvasProps } from "./WorkflowMapCanvas"
import { LoadingState } from "../ui/LoadingState"

// The map's own chunk, with React Flow in it: the Workflows page loads it
// only when the map is shown.
const WorkflowMapCanvas = lazy(() => import("./WorkflowMapCanvas"))

export function LazyWorkflowMapCanvas(props: WorkflowMapCanvasProps) {
  return <Suspense fallback={<LoadingState label="Drawing the map…" size="inline" />}><WorkflowMapCanvas {...props} /></Suspense>
}
