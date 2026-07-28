import { useState } from "react"
import { RotateCcw } from "lucide-react"
import type { WorkflowStepRun } from "../lib/workflow"

function expectedPath(step:WorkflowStepRun){try{return String(JSON.parse(step.resolved_inputs_json).expected_path??"")}catch{return ""}}

export function WorkflowStepRetryAction({step,onRetry}:{step:WorkflowStepRun;onRetry:(force:boolean)=>Promise<void>}){
  const cleanup=step.executor_type==="worktree.remove";const [confirming,setConfirming]=useState(false);const [force,setForce]=useState(false);const [busy,setBusy]=useState(false);const [error,setError]=useState("")
  async function retry(){setBusy(true);setError("");try{await onRetry(cleanup&&force);setConfirming(false)}catch(c){setError(c instanceof Error?c.message:String(c));setBusy(false)}}
  if(!cleanup)return <button onClick={()=>void retry()} disabled={busy} className="flex min-h-11 items-center gap-2 rounded-lg border border-zinc-700 px-3 text-sm"><RotateCcw size={15}/>{busy?"Retrying…":"Retry"}</button>
  return <><button onClick={()=>setConfirming(true)} className="flex min-h-11 items-center gap-2 rounded-lg border border-zinc-700 px-3 text-sm"><RotateCcw size={15}/>Retry</button>{confirming&&<div role="dialog" aria-modal="true" className="fixed inset-0 z-50 flex items-end bg-black/70 p-2 sm:items-center sm:justify-center"><div className="w-full rounded-xl border border-zinc-700 bg-zinc-900 p-4 sm:max-w-lg"><h3 className="text-lg font-semibold">Retry worktree removal?</h3><p className="mt-3 break-all rounded bg-zinc-950 p-3 text-xs text-zinc-400">{expectedPath(step)}</p><label className="mt-4 flex min-h-11 items-center gap-3 text-sm text-zinc-300"><input type="checkbox" checked={force} onChange={e=>setForce(e.target.checked)} className="size-4"/>Force removal (discard uncommitted changes)</label>{error&&<p className="mt-3 text-sm text-red-400">{error}</p>}<div className="mt-5 flex gap-2"><button onClick={()=>setConfirming(false)} className="min-h-11 flex-1 rounded-lg border border-zinc-700">Cancel</button><button onClick={()=>void retry()} disabled={busy} className="min-h-11 flex-1 rounded-lg bg-red-500 font-medium text-white">{busy?"Removing…":"Retry removal"}</button></div></div></div>}</>
}
