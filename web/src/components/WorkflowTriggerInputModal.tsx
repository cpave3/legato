import { useEffect, useState } from "react"
import { apiFetch } from "../lib/api"
import type { WorkflowTriggerRequest } from "../lib/board"
import { RepositoryPresetSelect, type RepositoryPreset } from "./RepositoryPresetSelect"

type DefinitionSummary = { title?: string; description?: string }

export function WorkflowTriggerInputModal({baseUrl,request,repositories,onClose,onStarted}:{baseUrl:string;request:WorkflowTriggerRequest;repositories:RepositoryPreset[];onClose:()=>void;onStarted:(request:WorkflowTriggerRequest)=>void}) {
  const [preset,setPreset]=useState("")
  const [repo,setRepo]=useState("")
  const [primaryDir,setPrimaryDir]=useState("")
  const [baseBranch,setBaseBranch]=useState("main")
  const [remember,setRemember]=useState(true)
  const [error,setError]=useState("")
  const [busy,setBusy]=useState(false)
  const confirming=request.status==="waiting_confirmation"
  let definition:DefinitionSummary={}
  try{definition=JSON.parse(request.definition_json??"{}") as DefinitionSummary}catch{/* stored definitions are validated by the backend */}
  useEffect(()=>{if(repositories.length===1){const item=repositories[0];setPreset(item.name);setRepo(item.repo);setPrimaryDir(item.primary_dir);setBaseBranch(item.base_branch??"main")}},[repositories])
  async function submit(){setBusy(true);setError("");const repository=preset||JSON.stringify({repo,primary_dir:primaryDir,base_branch:baseBranch});const body=confirming?{confirm:true}:{inputs:{repository},remember};try{const response=await apiFetch(baseUrl,`/api/workflow-triggers/${encodeURIComponent(request.id)}`,{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify(body)});if(!response.ok)throw new Error(await response.text());onStarted(await response.json() as WorkflowTriggerRequest)}catch(e){setError(e instanceof Error?e.message:String(e));setBusy(false)}}
  async function skip(){if(!confirm("Skip this automatic workflow trigger? You can still run the workflow manually."))return;const response=await apiFetch(baseUrl,`/api/workflow-triggers/${encodeURIComponent(request.id)}`,{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({skip:true})});if(response.ok)onClose();else setError(await response.text())}
  return <div role="dialog" aria-modal="true" className="fixed inset-0 z-50 flex items-end bg-black/70 p-2 sm:items-center sm:justify-center"><div className="w-full rounded-xl border border-zinc-700 bg-zinc-900 p-4 sm:max-w-lg"><h2 className="text-lg font-semibold">{confirming?(definition.title??request.workflow_name):"Repository required"}</h2><p className="mt-1 text-sm text-zinc-400">{confirming?(definition.description??`Start ${request.workflow_name}?`):`Choose a repository to start ${request.workflow_name}.`} Closing keeps this request in Attention.</p>{!confirming&&<div className="mt-4 space-y-3"><RepositoryPresetSelect repositories={repositories} value={preset} onChange={(name,item)=>{setPreset(name);if(item){setRepo(item.repo);setPrimaryDir(item.primary_dir);setBaseBranch(item.base_branch??"main")}}}/>{!preset&&<><label className="block text-sm">Repository<input aria-label="Repository" value={repo} onChange={e=>setRepo(e.target.value)} placeholder="owner/repo" className="mt-1 min-h-11 w-full rounded border border-zinc-700 bg-zinc-950 px-3"/></label><label className="block text-sm">Primary directory<input aria-label="Primary directory" value={primaryDir} onChange={e=>setPrimaryDir(e.target.value)} className="mt-1 min-h-11 w-full rounded border border-zinc-700 bg-zinc-950 px-3"/></label><label className="block text-sm">Base branch<input aria-label="Base branch" value={baseBranch} onChange={e=>setBaseBranch(e.target.value)} className="mt-1 min-h-11 w-full rounded border border-zinc-700 bg-zinc-950 px-3"/></label></>}<label className="flex gap-2 text-sm"><input type="checkbox" checked={remember} onChange={e=>setRemember(e.target.checked)}/> Remember on this task</label></div>}{error&&<p className="mt-4 text-sm text-red-400">{error}</p>}<div className="mt-5 flex flex-wrap gap-2"><button onClick={onClose} className="min-h-11 rounded border border-zinc-700 px-3">Not now</button><button onClick={skip} className="min-h-11 rounded border border-red-800 px-3 text-red-300">Skip trigger</button><button disabled={busy||(!confirming&&!preset&&(!repo||!primaryDir))} onClick={submit} className="min-h-11 flex-1 rounded bg-indigo-500 px-3 font-medium disabled:opacity-50">{busy?"Starting…":"Start workflow"}</button></div></div></div>
}
