import { useState } from "react"
import { Link } from "react-router-dom"
import { AlertCircle, Play, RefreshCw, Workflow } from "lucide-react"
import { useWorkflows, useWorkflowRuns } from "../hooks/useWorkflow"
import type { WorkflowDefinition } from "../lib/workflow"
import { RunWorkflowDialog } from "../components/RunWorkflowDialog"

type WorkflowTab = "definitions" | "runs"

function statusClass(status: string) {
  return status === "succeeded" ? "text-emerald-400" : status === "failed" ? "text-red-400" : status === "running" ? "text-indigo-300" : "text-zinc-400"
}

function PageFrame({ tab, setTab, refresh, children }: { tab: WorkflowTab; setTab: (tab: WorkflowTab) => void; refresh: () => Promise<void>; children: React.ReactNode }) {
  return <div className="flex min-h-0 flex-1 flex-col overflow-y-auto px-4 py-4 sm:px-6">
    <header className="mx-auto flex w-full max-w-6xl items-center justify-between"><div><h1 className="flex items-center gap-2 text-xl font-semibold"><Workflow className="text-indigo-300"/>Workflows</h1><p className="text-sm text-zinc-500">Reusable deterministic and agent-backed runs</p></div><button aria-label="Refresh workflows" onClick={()=>{void refresh()}} className="min-h-11 rounded-lg border border-zinc-700 px-3 text-zinc-300"><RefreshCw size={16}/></button></header>
    <div className="mx-auto mt-4 flex w-full max-w-6xl gap-2 border-b border-zinc-800"><button onClick={()=>setTab("definitions")} className={`min-h-11 px-3 text-sm ${tab==="definitions"?"border-b-2 border-indigo-400 text-white":"text-zinc-500"}`}>Definitions</button><button onClick={()=>setTab("runs")} className={`min-h-11 px-3 text-sm ${tab==="runs"?"border-b-2 border-indigo-400 text-white":"text-zinc-500"}`}>Runs</button></div>
    {children}
  </div>
}

function DefinitionsTab({ setTab }: { setTab: (tab: WorkflowTab) => void }) {
  const definitions = useWorkflows()
  const [selected, setSelected] = useState<WorkflowDefinition | null>(null)
  return <PageFrame tab="definitions" setTab={setTab} refresh={definitions.refresh}>
    <section className="mx-auto grid w-full max-w-6xl gap-3 py-4 md:grid-cols-2">
      {(definitions.data?.definitions??[]).map(definition=><article key={definition.name} className="rounded-xl border border-zinc-800 bg-zinc-900/60 p-4"><div className="flex items-start justify-between gap-3"><div className="min-w-0"><h2 className="font-medium">{definition.title}</h2><p className="break-all text-xs text-zinc-500">{definition.source}</p></div><button onClick={()=>setSelected(definition)} className="flex min-h-11 items-center gap-2 rounded-lg bg-indigo-500 px-3 text-sm font-medium text-white"><Play size={15}/>Run</button></div><p className="mt-3 text-sm text-zinc-400">{definition.description||"No description"}</p><div className="mt-3 flex flex-wrap gap-2">{definition.steps.map(step=><span key={step.id} className="rounded bg-zinc-800 px-2 py-1 text-xs text-zinc-300">{step.type}</span>)}</div></article>)}
    </section>
    {(definitions.data?.definitions??[]).length===0&&<p className="mx-auto w-full max-w-6xl text-sm text-zinc-500">No workflow definitions found</p>}
    {(definitions.data?.errors??[]).map(error=><p key={error.path} className="mx-auto mb-2 w-full max-w-6xl rounded border border-red-900/50 bg-red-950/20 p-3 text-sm text-red-300">{error.path}: {error.error}</p>)}
    {selected&&<RunWorkflowDialog definition={selected} repositories={definitions.data?.repositories??[]} onClose={()=>setSelected(null)}/>}
  </PageFrame>
}

function RunsTab({ setTab }: { setTab: (tab: WorkflowTab) => void }) {
  const runs = useWorkflowRuns()
  return <PageFrame tab="runs" setTab={setTab} refresh={runs.refresh}>
    {(runs.data??[]).length>0&&<ul aria-label="Workflow runs" className="mx-auto my-4 w-full max-w-6xl divide-y divide-zinc-800 overflow-hidden rounded-xl border border-zinc-800 bg-zinc-900/60">
      {(runs.data??[]).map(run=><li key={run.id}><Link to={`/workflows/runs/${run.id}`} className="block p-4 hover:bg-zinc-800/50 sm:grid sm:grid-cols-[minmax(0,1fr)_minmax(10rem,0.6fr)_auto] sm:items-center sm:gap-4"><div className="min-w-0"><h2 className="truncate font-medium">{run.workflow_title}</h2><p className="mt-1 truncate text-xs text-zinc-600">{run.id}</p>{run.needs_attention&&<p className="mt-2 flex items-center gap-2 text-sm text-amber-300"><AlertCircle className="shrink-0" size={15}/><span className="min-w-0 break-words">{run.attention_reason||"Workflow needs attention"}</span></p>}{run.error&&<p className="mt-2 flex items-center gap-2 text-sm text-red-400"><AlertCircle className="shrink-0" size={15}/><span className="min-w-0 break-words">{run.error}</span></p>}</div><p className="mt-3 text-sm text-zinc-400 sm:mt-0"><span className="text-zinc-600">Current: </span>{run.current_step_id||"Complete"}</p><span className={`mt-3 text-xs font-medium sm:mt-0 sm:text-right ${statusClass(run.status)}`}>{run.status}</span></Link></li>)}
    </ul>}
    {(runs.data??[]).length===0&&<p className="mx-auto w-full max-w-6xl text-sm text-zinc-500">No workflow runs yet</p>}
  </PageFrame>
}

export function WorkflowsPage() {
  const [tab, setTab] = useState<WorkflowTab>("definitions")
  return tab === "definitions" ? <DefinitionsTab setTab={setTab}/> : <RunsTab setTab={setTab}/>
}
