import { useState } from "react"
import { Link, useParams } from "react-router-dom"
import { ArrowLeft, Play } from "lucide-react"
import { useWorkflows, useWorkflowRuns } from "../hooks/useWorkflow"
import { useNow } from "../hooks/useNow"
import { ago, fileWorkflowName, runState } from "../lib/workflowView"
import { RunWorkflowDialog } from "../components/RunWorkflowDialog"
import { StatePill } from "../components/WorkflowRunStatus"
import { LazyWorkflowGraph } from "../components/workflow-graph/LazyWorkflowGraph"
import { Button } from "../components/ui/Button"
import { LoadingState } from "../components/ui/LoadingState"

const RECENT_RUNS = 10

function Frame({ children }: { children: React.ReactNode }) {
  return <main className="flex min-h-0 flex-1 flex-col overflow-y-auto px-4 py-4 font-lw text-lw-text sm:px-6 md:py-6">
    <div className="mx-auto flex w-full max-w-6xl flex-col gap-4">
      <Link to="/workflows?tab=workflows" className="inline-flex items-center gap-1.5 self-start text-sm text-lw-text-2 hover:text-lw-text"><ArrowLeft size={14} />Workflows</Link>
      {children}
    </div>
  </main>
}

// WorkflowPage is one workflow by name: what it is, its workflow graph, Run,
// and its recent runs.
export function WorkflowPage() {
  const { name = "" } = useParams()
  const definitions = useWorkflows()
  const runs = useWorkflowRuns()
  const now = useNow(30_000)
  const [running, setRunning] = useState(false)
  const [selected, setSelected] = useState<string | null>(null)
  if (definitions.loading && !definitions.data) return <Frame><LoadingState label="Loading the workflow…" size="inline" /></Frame>
  if (definitions.error && !definitions.data) return <Frame><p role="alert" className="text-sm text-lw-error">Could not load workflows: {definitions.error}</p></Frame>
  const definition = (definitions.data?.definitions ?? []).find(item => item.name === name)
  if (!definition) {
    const failure = (definitions.data?.errors ?? []).find(error => fileWorkflowName(error.path) === name)
    if (failure) return <Frame>
      <h1 className="text-xl font-semibold">{name}</h1>
      <p className="font-lw-mono text-xs text-lw-faint">{failure.path}</p>
      <p role="alert" className="break-words rounded-lg border border-lw-danger-line bg-lw-error-soft p-3 text-sm text-lw-danger">This workflow's file can't be parsed, so there is no graph: {failure.error}</p>
    </Frame>
    return <Frame><p className="text-sm text-lw-text-2">No workflow of that name</p></Frame>
  }
  const recent = (runs.data ?? []).filter(run => run.workflow_name === definition.name).slice(0, RECENT_RUNS)
  return <Frame>
    <header className="flex flex-wrap items-start justify-between gap-3">
      <div className="min-w-0">
        <h1 className="text-xl font-semibold">{definition.title}</h1>
        {definition.description && <p className="mt-1 text-sm text-lw-text-2">{definition.description}</p>}
        <p className="mt-1 break-all font-lw-mono text-xs text-lw-faint">{definition.source}</p>
      </div>
      <Button variant="outline" size="sm" aria-label={`Run ${definition.title}`} onClick={() => setRunning(true)}><Play size={13} />Run</Button>
    </header>
    <section aria-label="Workflow graph" className="h-[28rem] overflow-hidden rounded-xl border border-lw-line bg-lw-ground">
      {definition.graph ? <LazyWorkflowGraph graph={definition.graph} selectedId={selected} onSelect={setSelected} /> : <p className="p-4 text-sm text-lw-text-2">This Legato sent no graph for this workflow.</p>}
    </section>
    <section>
      <h2 className="mb-2 text-xs font-semibold uppercase tracking-wide text-lw-text-2">Recent runs</h2>
      {recent.length === 0
        ? <p className="text-sm text-lw-text-2">{runs.loading ? "Loading runs…" : "No runs yet"}</p>
        : <ul aria-label="Recent runs" className="overflow-hidden rounded-xl border border-lw-line bg-lw-surface">
          {recent.map(run => {
            const state = runState(run)
            return <li key={run.id} className="border-t border-lw-line first:border-t-0">
              <Link to={`/workflows/runs/${encodeURIComponent(run.id)}`} className="flex items-center gap-3 px-4 py-2.5 hover:bg-lw-raised">
                <StatePill tone={state.tone} label={state.label} />
                <span className="min-w-0 flex-1 truncate text-sm">{run.workflow_title}</span>
                <span className="font-lw-mono text-xs text-lw-faint" title={run.created_at}>{ago(run.created_at, now)}</span>
              </Link>
            </li>
          })}
        </ul>}
    </section>
    {running && <RunWorkflowDialog definition={definition} repositories={definitions.data?.repositories ?? []} onClose={() => setRunning(false)} />}
  </Frame>
}
