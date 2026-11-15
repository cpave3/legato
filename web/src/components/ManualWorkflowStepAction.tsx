import { Dialog, DialogPanel, DialogHeader, DialogTitle, DialogBody, DialogFooter } from "./ui/Dialog"
import { useState } from "react"
import type { WorkflowStepRun } from "../lib/workflow"
import { Button } from "./ui/Button"

function parseInputs(raw:string):Record<string,unknown>{try{return JSON.parse(raw)}catch{return {}}}

// The button that runs a step waiting for the operator (label names it).
// Removing a worktree asks first, and can force the removal.
export function ManualWorkflowStepAction({step,onTrigger,label="Run step"}:{step:WorkflowStepRun;onTrigger:(force:boolean)=>Promise<void>;label?:string}){
  const [confirming,setConfirming]=useState(false);const [force,setForce]=useState(false);const [busy,setBusy]=useState(false);const [error,setError]=useState("")
  const cleanup=step.executor_type==="worktree.remove";const expected=String(parseInputs(step.resolved_inputs_json).expected_path??"")
  async function trigger(){setBusy(true);setError("");try{await onTrigger(cleanup&&force);setConfirming(false)}catch(c){setError(c instanceof Error?c.message:String(c))}finally{setBusy(false)}}
  return <>
    <Button variant="warning" onClick={()=>cleanup?setConfirming(true):void trigger()} disabled={busy}>{busy?"Running…":cleanup&&label==="Run step"?"Remove worktree":label}</Button>
    {error&&!confirming&&<p role="alert" className="basis-full text-sm text-lw-error">{error}</p>}
    {confirming&&<Dialog open={confirming} onClose={()=>setConfirming(false)}><DialogPanel className="max-w-lg font-lw text-lw-text"><DialogHeader><DialogTitle className="text-lg font-semibold">Remove worktree?</DialogTitle></DialogHeader><DialogBody><p className="mt-3 text-sm text-lw-text-2">Yggdrasil will remove this worktree and clear its task metadata:</p><p className="mt-2 break-all rounded-lg bg-lw-ground p-3 font-lw-mono text-xs text-lw-text-2">{expected}</p><p className="mt-3 text-sm text-lw-text-2">Managed workflow artifacts, including the review report, will remain available.</p><label className="mt-4 flex min-h-11 items-center gap-3 text-sm"><input type="checkbox" checked={force} onChange={e=>setForce(e.target.checked)} className="size-4"/>Force removal (discard uncommitted changes)</label>{error&&<p role="alert" className="text-sm text-lw-error">{error}</p>}</DialogBody><DialogFooter className="flex gap-2"><Button variant="outline" onClick={()=>setConfirming(false)} className="flex-1">Keep worktree</Button><Button variant="destructive" onClick={()=>void trigger()} disabled={busy} className="flex-1">{busy?"Removing…":"Remove worktree"}</Button></DialogFooter></DialogPanel></Dialog>}
  </>
}
