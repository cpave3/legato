import { useCallback, useEffect, useState } from "react"
import type { FormEvent } from "react"
import { Link, useParams } from "react-router-dom"
import { ArrowLeft, BookOpen } from "lucide-react"
import { useServer } from "../hooks/useServer"
import { decideLearning, fetchLearning, setLearningPromptEnabled, type LearningView } from "../lib/learning"
import { LoadingState } from "../components/ui/LoadingState"

const inputClass = "w-full rounded-lg border border-zinc-700 bg-zinc-950 px-3 py-2 text-sm outline-none focus:border-indigo-500"

export function LearningDetailPage() {
  const { learningId = "" } = useParams()
  const { baseUrl } = useServer()
  const [view, setView] = useState<LearningView | null>(null)
  const [claim, setClaim] = useState("")
  const [rationale, setRationale] = useState("")
  const [scope, setScope] = useState("")
  const [category, setCategory] = useState("")
  const [tags, setTags] = useState("")
  const [projectKey, setProjectKey] = useState("")
  const [loading, setLoading] = useState(true)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    setLoading(true); setError(null)
    try {
      const next = await fetchLearning(baseUrl, learningId)
      setView(next); setClaim(next.learning.claim); setRationale(next.learning.rationale ?? ""); setScope(next.learning.scope); setProjectKey(next.learning.project_key ?? ""); setCategory(next.learning.category ?? ""); setTags(next.learning.tags?.join(", ") ?? "")
    } catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)) }
    finally { setLoading(false) }
  }, [baseUrl, learningId])
  useEffect(() => { void load() }, [load])

  async function decide(action: "accept" | "edit" | "reject", event?: FormEvent) {
    event?.preventDefault()
    if (!view) return
    setPending(true); setError(null)
    try {
      const body = action === "edit" ? { revision: view.learning.revision, claim, rationale, scope, project_key: scope === "project" ? projectKey : "", category, tags: tags.split(",").map(value => value.trim()).filter(Boolean) } : { revision: view.learning.revision }
      await decideLearning(baseUrl, learningId, action, body)
      const next = await fetchLearning(baseUrl, learningId)
      setView(next); setClaim(next.learning.claim); setRationale(next.learning.rationale ?? ""); setScope(next.learning.scope); setProjectKey(next.learning.project_key ?? ""); setCategory(next.learning.category ?? ""); setTags(next.learning.tags?.join(", ") ?? "")
    } catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)) }
    finally { setPending(false) }
  }

  async function togglePrompt(enabled: boolean) {
    if (!view) return
    setPending(true); setError(null)
    try {
      const next = await setLearningPromptEnabled(baseUrl, learningId, view.learning.revision, enabled)
      setView(next)
    } catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)) }
    finally { setPending(false) }
  }

  if (loading) return <LoadingState label="Loading the learning…" />
  if (!view) return <div className="p-8"><Link to="/learning" className="text-indigo-300">Back to Learning</Link>{error && <p role="alert" className="mt-3 text-red-400">{error}</p>}</div>
  const { learning, evidence, incorporations = [] } = view

  return <div className="flex-1 overflow-y-auto">
    <header className="border-b border-zinc-800 px-4 py-4 sm:px-6"><Link to="/learning" className="flex items-center gap-1 text-sm text-zinc-400 hover:text-zinc-200"><ArrowLeft size={16} />Learning</Link><h1 className="mt-3 flex items-center gap-2 text-lg font-semibold"><BookOpen size={20} className="text-indigo-400" />Learning detail</h1></header>
    <div className="mx-auto max-w-4xl space-y-5 p-4 sm:p-6">
      {error && <p role="alert" className="rounded border border-red-900 bg-red-950/40 p-3 text-sm text-red-400">{error}</p>}
      <section className="rounded-xl border border-zinc-800 bg-zinc-900/40 p-4">
        <div className="flex flex-wrap gap-2 text-xs"><span className="rounded bg-indigo-500/15 px-2 py-1 text-indigo-300">{learning.status}</span><span className="rounded bg-zinc-800 px-2 py-1">{learning.scope}</span><span className="rounded bg-zinc-800 px-2 py-1">{learning.kind}</span><span className="ml-auto text-zinc-500">Revision {learning.revision}</span></div>
        <p className="mt-4 text-lg text-zinc-100">{learning.claim}</p>{learning.rationale && <p className="mt-2 text-sm text-zinc-400">{learning.rationale}</p>}
        {learning.project_key && <p className="mt-3 text-xs text-zinc-500">Project: {learning.project_key}</p>}
      </section>

      <form onSubmit={e => decide("edit", e)} className="space-y-3 rounded-xl border border-zinc-800 p-4">
        <h2 className="font-medium">Curate</h2><label className="block text-xs text-zinc-400">Claim<textarea aria-label="Claim" className={`${inputClass} mt-1 min-h-24`} value={claim} onChange={e => setClaim(e.target.value)} required /></label>
        <label className="block text-xs text-zinc-400">Rationale<textarea aria-label="Rationale" className={`${inputClass} mt-1 min-h-16`} value={rationale} onChange={e => setRationale(e.target.value)} /></label>
        <div className="grid gap-3 sm:grid-cols-2"><label className="text-xs text-zinc-400">Scope<select aria-label="Edit scope" className={`${inputClass} mt-1`} value={scope} onChange={e => setScope(e.target.value)}><option value="user">user</option><option value="project">project</option></select></label>{scope === "project" && <label className="text-xs text-zinc-400">Project key<input aria-label="Project key" className={`${inputClass} mt-1`} value={projectKey} onChange={e => setProjectKey(e.target.value)} required /></label>}<label className="text-xs text-zinc-400">Category<input aria-label="Category" className={`${inputClass} mt-1`} value={category} onChange={e => setCategory(e.target.value)} /></label><label className="text-xs text-zinc-400">Tags<input aria-label="Tags" className={`${inputClass} mt-1`} value={tags} onChange={e => setTags(e.target.value)} placeholder="agents, callbacks" /></label></div>
        <p className="text-xs text-zinc-500">Actions will submit revision {learning.revision}.</p><div className="flex flex-wrap gap-2"><button disabled={pending || learning.status === "rejected"} className="rounded-lg border border-zinc-700 px-4 py-2 text-sm disabled:opacity-50">Save edit</button>{learning.status === "candidate" && <><button type="button" disabled={pending} onClick={() => decide("accept")} className="rounded-lg bg-emerald-600 px-4 py-2 text-sm font-medium disabled:opacity-50">Accept</button><button type="button" disabled={pending} onClick={() => decide("reject")} className="rounded-lg bg-red-600/80 px-4 py-2 text-sm font-medium disabled:opacity-50">Reject</button></>}</div>
      </form>

      {(learning.status === "validated" || learning.status === "incorporated") && <section className="space-y-3 rounded-xl border border-indigo-900/60 bg-indigo-950/20 p-4"><div className="flex items-center justify-between gap-4"><div><h2 className="font-medium">Agent prompt activation</h2><p className="mt-1 text-sm text-zinc-400">{learning.scope === "user" ? "Applies to new agents in every repository." : `Applies only when the task resolves to ${learning.project_key}.`} Existing sessions are unchanged.</p></div><button type="button" role="switch" aria-checked={learning.prompt_enabled} disabled={pending} onClick={() => togglePrompt(!learning.prompt_enabled)} className={`rounded-full px-4 py-2 text-sm font-medium ${learning.prompt_enabled ? "bg-emerald-600" : "bg-zinc-700"}`}>{learning.prompt_enabled ? "Enabled" : "Disabled"}</button></div>{!learning.prompt_decided && <p className="text-xs text-amber-300">Newly accepted learnings start disabled. Choose whether to enable this one; you can change it later.</p>}</section>}

      <section className="rounded-xl border border-zinc-800 p-4"><h2 className="font-medium">Evidence provenance</h2>{evidence.length === 0 ? <p className="mt-3 text-sm text-zinc-500">No evidence linked.</p> : <ul className="mt-3 space-y-3">{evidence.map(item => <li key={item.id} className="rounded-lg bg-zinc-900 p-3"><p className="text-sm">{item.content}</p><dl className="mt-2 grid gap-1 text-xs text-zinc-500 sm:grid-cols-2"><div><dt className="inline text-zinc-400">Source: </dt><dd className="inline">{item.source_kind} · {item.source_ref}</dd></div><div><dt className="inline text-zinc-400">Author: </dt><dd className="inline">{item.author}</dd></div>{item.task_id && <div><dt className="inline text-zinc-400">Task: </dt><dd className="inline">{item.task_id}</dd></div>}<div className="break-all"><dt className="inline text-zinc-400">Digest: </dt><dd className="inline">{item.content_digest}</dd></div></dl></li>)}</ul>}</section>

      <section className="rounded-xl border border-zinc-800 p-4"><h2 className="font-medium">Incorporation history</h2>{incorporations.length === 0 ? <p className="mt-3 text-sm text-zinc-500">No incorporations yet.</p> : <ul className="mt-3 space-y-3">{incorporations.map(item => <li key={item.id} className="rounded-lg bg-zinc-900 p-3"><div className="flex gap-2 text-sm"><strong>{item.destination}</strong><span className="rounded bg-zinc-800 px-2 text-xs leading-5 text-zinc-400">{item.status}</span></div>{item.target && <p className="mt-1 text-sm text-zinc-400">{item.action ? `${item.action} · ` : ""}{item.target}</p>}{item.verification_result && <p className="mt-2 text-xs text-zinc-500">Verification: {item.verification_result}</p>}</li>)}</ul>}</section>
    </div>
  </div>
}
