import { useState } from "react"
import { RotateCcw } from "lucide-react"
import type { WorkflowStepRun } from "../lib/workflow"
import { Button } from "./ui/Button"

function expectedPath(step:WorkflowStepRun){try{return String(JSON.parse(step.resolved_inputs_json).expected_path??"")}catch{return ""}}

// Runs a failed step again. Retrying a worktree removal asks first, and can
// force it.
export function WorkflowStepRetryAction({step,onRetry}:{step:WorkflowStepRun;onRetry:(force:boolean)=>Promise<void>}){
  const cleanup=step.executor_type==="worktree.remove";const [confirming,setConfirming]=useState(false);const [force,setForce]=useState(false);const [busy,setBusy]=useState(false);const [error,setError]=useState("")
  async function retry(){setBusy(true);setError("");try{await onRetry(cleanup&&force);setConfirming(false)}catch(c){setError(c instanceof Error?c.message:String(c))}finally{setBusy(false)}}
  return <>
    <Button variant="outline" onClick={()=>cleanup?setConfirming(true):void retry()} disabled={busy}><RotateCcw size={15}/>{busy?"Retrying…":"Retry step"}</Button>
    {error&&!confirming&&<p role="alert" className="basis-full text-sm text-lw-error">{error}</p>}
    {confirming&&<div role="dialog" aria-modal="true" className="fixed inset-0 z-50 flex items-end bg-black/70 p-2 sm:items-center sm:justify-center"><div className="w-full rounded-xl border border-lw-line-strong bg-lw-surface p-4 font-lw text-lw-text sm:max-w-lg"><h3 className="text-lg font-semibold">Retry worktree removal?</h3><p className="mt-3 break-all rounded-lg bg-lw-ground p-3 font-lw-mono text-xs text-lw-text-2">{expectedPath(step)}</p><label className="mt-4 flex min-h-11 items-center gap-3 text-sm"><input type="checkbox" checked={force} onChange={e=>setForce(e.target.checked)} className="size-4"/>Force removal (discard uncommitted changes)</label>{error&&<p role="alert" className="mt-3 text-sm text-lw-error">{error}</p>}<div className="mt-5 flex gap-2"><Button variant="outline" onClick={()=>setConfirming(false)} className="flex-1">Cancel</Button><Button variant="destructive" onClick={()=>void retry()} disabled={busy} className="flex-1">{busy?"Removing…":"Retry removal"}</Button></div></div></div>}
  </>
}
