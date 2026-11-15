import { useCallback, useLayoutEffect, useRef, useState } from "react"
import type { FormEvent } from "react"
import { Link } from "react-router-dom"
import { BookOpen, Search } from "lucide-react"
import { useServer } from "../hooks/useServer"
import { fetchLearnings, fetchLearningPromptPreview, type Learning, type LearningPromptPreview } from "../lib/learning"
import { InlineNotice } from "../components/ui/InlineNotice"
import { LoadingState } from "../components/ui/LoadingState"

const inputClass = "min-h-10 rounded-lg border border-zinc-700 bg-zinc-900 px-3 text-sm text-zinc-200 outline-none focus:border-indigo-500"

export function LearningsPage() {
  const { baseUrl } = useServer()
  return <Learnings key={baseUrl} baseUrl={baseUrl} />
}

function Learnings({ baseUrl }: { baseUrl: string }) {
  const request = useRef<object | null>(null)
  const previewRequest = useRef(0)
  const [previewError, setPreviewError] = useState<string | null>(null)
  const [previewLoading, setPreviewLoading] = useState(false)
  const [items, setItems] = useState<Learning[]>([])
  const [query, setQuery] = useState("")
  const [submittedQuery, setSubmittedQuery] = useState("")
  const [status, setStatus] = useState("")
  const [scope, setScope] = useState("")
  const [projectKey, setProjectKey] = useState("")
  const [category, setCategory] = useState("")
  const [tags, setTags] = useState("")
  const [searchMode, setSearchMode] = useState("")
  const [previewTask, setPreviewTask] = useState("")
  const [preview, setPreview] = useState<LearningPromptPreview | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    const version = request.current = {}
    setLoading(true); setError(null)
    try {
      const result = await fetchLearnings(baseUrl, { query: submittedQuery, status, scope, projectKey, category, tags: tags.split(",").map(value => value.trim()).filter(Boolean) })
      if (version !== request.current) return
      setItems(result.items); setSearchMode(submittedQuery ? result.mode : "")
    }
    catch (cause) { if (version === request.current) setError(cause instanceof Error ? cause.message : String(cause)) }
    finally { if (version === request.current) setLoading(false) }
  }, [baseUrl, submittedQuery, status, scope, projectKey, category, tags])
  useLayoutEffect(() => {
    void load()
    return () => { request.current = null }
  }, [load])

  function search(event: FormEvent) { event.preventDefault(); setSubmittedQuery(query.trim()) }
  useLayoutEffect(() => () => { previewRequest.current++ }, [])
  async function loadPreview(event: FormEvent) {
    event.preventDefault()
    const version = ++previewRequest.current
    setPreviewError(null); setPreview(null); setPreviewLoading(true)
    try {
      const next = await fetchLearningPromptPreview(baseUrl, previewTask.trim())
      if (version === previewRequest.current) setPreview(next)
    } catch (cause) {
      if (version === previewRequest.current) setPreviewError(cause instanceof Error ? cause.message : String(cause))
    } finally {
      if (version === previewRequest.current) setPreviewLoading(false)
    }
  }

  return <div className="flex flex-1 flex-col overflow-hidden">
    <header className="border-b border-zinc-800 px-4 py-4 sm:px-6">
      <h1 className="flex items-center gap-2 text-lg font-semibold"><BookOpen size={20} className="text-indigo-400" />Learning</h1>
      <form onSubmit={search} className="mt-4 flex flex-wrap gap-2">
        <label className="relative min-w-52 flex-1"><span className="sr-only">Search learnings</span><Search size={16} className="absolute left-3 top-3 text-zinc-500" /><input className={`${inputClass} w-full pl-9`} value={query} onChange={e => setQuery(e.target.value)} placeholder="Search claims and rationale" /></label>
        <select aria-label="Status" className={inputClass} value={status} onChange={e => setStatus(e.target.value)}><option value="">All statuses</option>{["candidate","validated","rejected","action_proposed","incorporated","stale"].map(v => <option key={v}>{v}</option>)}</select>
        <select aria-label="Scope" className={inputClass} value={scope} onChange={e => setScope(e.target.value)}><option value="">All scopes</option><option value="user">user</option><option value="project">project</option></select>
        <input aria-label="Project key filter" className={inputClass} value={projectKey} onChange={e => setProjectKey(e.target.value)} placeholder="Project key" />
        <input aria-label="Category filter" className={inputClass} value={category} onChange={e => setCategory(e.target.value)} placeholder="Category" />
        <input aria-label="Tag filters" className={inputClass} value={tags} onChange={e => setTags(e.target.value)} placeholder="Tags (comma-separated)" />
        <button className="min-h-10 rounded-lg bg-indigo-500 px-4 text-sm font-medium">Search</button>
        {searchMode && <span className="self-center text-xs text-zinc-500">{searchMode} search</span>}
      </form>
      <form onSubmit={loadPreview} className="mt-3 flex flex-wrap gap-2 rounded-lg border border-zinc-800 p-3"><input aria-label="Prompt preview task" className={`${inputClass} min-w-56 flex-1`} value={previewTask} onChange={e => { previewRequest.current++; setPreviewTask(e.target.value); setPreview(null); setPreviewError(null); setPreviewLoading(false) }} placeholder="Task ID for effective prompt preview" required /><button disabled={previewLoading} className="rounded-lg border border-indigo-700 px-4 text-sm text-indigo-200 disabled:opacity-50">{previewLoading ? "Loading preview…" : "Preview prompt"}</button>{previewError && <InlineNotice variant="error" className="w-full">{previewError} Try Preview prompt again.</InlineNotice>}{preview && <div className="w-full"><p className="text-xs text-zinc-500">Repository: {preview.project_key || "user scope only"} · {preview.included.length} enabled learning(s)</p><pre className="mt-2 whitespace-pre-wrap rounded bg-zinc-950 p-3 text-xs text-zinc-300">{preview.rendered || "No enabled learnings would be injected."}</pre></div>}</form>
      {error && <InlineNotice variant="error" className="mt-2">{error} <button type="button" onClick={() => void load()} className="underline">Retry learnings</button></InlineNotice>}
    </header>
    <div className="flex-1 overflow-y-auto">
      {loading ? <LoadingState label="Loading learnings…" /> : error ? null : items.length === 0 ? <p className="p-6 text-zinc-500">No learnings found.</p> :
        <ul className="divide-y divide-zinc-800">{items.map(item => <li key={item.id}>
          <Link to={`/learning/${encodeURIComponent(item.id)}`} className="block px-4 py-4 hover:bg-zinc-800/40 sm:px-6">
            <div className="flex flex-wrap gap-2 text-xs"><span className="rounded bg-indigo-500/15 px-2 py-0.5 text-indigo-300">{item.status}</span><span className="rounded bg-zinc-800 px-2 py-0.5 text-zinc-400">{item.scope}{item.project_key ? ` · ${item.project_key}` : ""}</span><span className="rounded bg-zinc-800 px-2 py-0.5 text-zinc-400">{item.kind}</span>{item.category && <span className="rounded bg-sky-500/15 px-2 py-0.5 text-sky-300">{item.category}</span>}{item.tags?.map(tag => <span key={tag} className="rounded bg-zinc-800 px-2 py-0.5 text-zinc-500">#{tag}</span>)}{(item.status === "validated" || item.status === "incorporated") && <span className={`rounded px-2 py-0.5 ${item.prompt_enabled ? "bg-emerald-500/15 text-emerald-300" : "bg-zinc-800 text-zinc-500"}`}>{item.prompt_enabled ? "prompt enabled" : "prompt disabled"}</span>}<span className="ml-auto text-zinc-600">rev {item.revision}</span></div>
            <p className="mt-2 text-sm text-zinc-200">{item.claim}</p>
          </Link>
        </li>)}</ul>}
    </div>
  </div>
}
