import { lazy, Suspense } from "react"
import type { WorkflowYamlEditorProps } from "./WorkflowYamlEditor"
import { LoadingState } from "../ui/LoadingState"

// The editor's own chunk, with CodeMirror and the workflow schema in it:
// only the pages that edit a workflow load it.
const WorkflowYamlEditor = lazy(() => import("./WorkflowYamlEditor"))

export function LazyWorkflowYamlEditor(props: WorkflowYamlEditorProps) {
  return <Suspense fallback={<LoadingState label="Opening the editor…" size="inline" />}><WorkflowYamlEditor {...props} /></Suspense>
}
