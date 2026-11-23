import { lazy, Suspense } from "react"
import type { WorkflowStartProps } from "./WorkflowStart"
import { LoadingState } from "../ui/LoadingState"

// The start page's own chunk, with the yaml package and the document model.
const WorkflowStart = lazy(() => import("./WorkflowStart"))

export function LazyWorkflowStart(props: WorkflowStartProps) {
  return <Suspense fallback={<LoadingState label="Opening…" size="inline" />}><WorkflowStart {...props} /></Suspense>
}
