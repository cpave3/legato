import { lazy, Suspense } from "react"
import type { AgentGraphCanvasProps } from "./AgentGraphCanvas"
import { LoadingState } from "../ui/LoadingState"

// The graph's own chunk, with React Flow in it: the agents page loads it
// only when the graph is shown.
const AgentGraphCanvas = lazy(() => import("./AgentGraphCanvas"))

export function LazyAgentGraphCanvas(props: AgentGraphCanvasProps) {
  return <Suspense fallback={<LoadingState label="Drawing the graph…" size="inline" />}><AgentGraphCanvas {...props} /></Suspense>
}
