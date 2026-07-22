import { ArrowRight, CheckCircle2, RefreshCw } from "lucide-react"
import { Link } from "react-router-dom"
import { usePlanQueue } from "../hooks/usePlan"

export function PlanQueuePage() {
  const {data,loading,error,refresh}=usePlanQueue()
  return <div className="flex h-full flex-col overflow-hidden bg-[#0a0a0f] text-zinc-200">
    <header className="flex items-center justify-between border-b border-zinc-800 px-6 py-4"><div><h1 className="text-lg font-semibold">Plan queue</h1><p className="mt-0.5 text-xs text-zinc-500">Collaborate on implementation plans before agents begin work.</p></div><button onClick={()=>void refresh()} className="rounded p-2 text-zinc-500 hover:bg-zinc-900"><RefreshCw size={16}/></button></header>
    <div className="flex-1 overflow-y-auto p-6">
      {loading&&!data&&<p className="text-sm text-zinc-500">Loading plans…</p>}
      {error&&<div role="alert" className="rounded border border-red-900 bg-red-950/40 p-3 text-sm text-red-300">{error}</div>}
      {data?.length===0&&<div className="flex flex-col items-center gap-2 py-20 text-zinc-500"><CheckCircle2 size={28}/><p className="text-sm">No plans need a decision.</p></div>}
      <div className="mx-auto grid max-w-4xl gap-3">{(data??[]).map(item=><Link key={item.plan_id} to={`/plans/${encodeURIComponent(item.plan_id)}`} className="group rounded border border-zinc-800 bg-zinc-950 p-4 hover:border-indigo-700 hover:bg-zinc-900"><div className="flex items-start justify-between gap-4"><div><div className="flex gap-2"><span className="font-mono text-xs text-zinc-500">{item.task_id}</span><span className="rounded bg-zinc-800 px-1.5 py-0.5 text-[10px] uppercase text-zinc-400">{item.status.replace("_"," ")}</span><span className="text-xs text-zinc-600">rev {item.revision}</span></div><h2 className="mt-1 font-semibold text-zinc-100">{item.title}</h2>{item.summary&&<p className="mt-2 text-sm text-zinc-400">{item.summary}</p>}</div><div className="flex items-center gap-3"><span className="text-xs text-amber-300">{item.unanswered_required ? `${item.unanswered_required} required` : "Ready for decision"}</span><ArrowRight size={16}/></div></div></Link>)}</div>
    </div>
  </div>
}
