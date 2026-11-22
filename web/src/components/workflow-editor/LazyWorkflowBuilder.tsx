import { lazy, Suspense } from "react"
import type { WorkflowBuilderProps } from "./WorkflowBuilder"
import { LoadingState } from "../ui/LoadingState"

// The builder's own chunk, with the yaml package and the document model.
const WorkflowBuilder = lazy(() => import("./WorkflowBuilder"))

export function LazyWorkflowBuilder(props: WorkflowBuilderProps) {
  return <Suspense fallback={<LoadingState label="Opening the builder…" size="inline" />}><WorkflowBuilder {...props} /></Suspense>
}
