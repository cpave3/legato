import { lazy, Suspense } from "react"
import type { WorkflowOutlineProps } from "./WorkflowOutline"
import { LoadingState } from "../ui/LoadingState"

// The outline editor's own chunk, with the yaml package and the document
// model.
const WorkflowOutline = lazy(() => import("./WorkflowOutline"))

export function LazyWorkflowOutline(props: WorkflowOutlineProps) {
  return <Suspense fallback={<LoadingState label="Opening the editor…" size="inline" />}><WorkflowOutline {...props} /></Suspense>
}
