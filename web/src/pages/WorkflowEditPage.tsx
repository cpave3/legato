import { useEffect, useRef, useState } from "react"
import { Link, useNavigate, useParams } from "react-router-dom"
import { useQueryClient } from "@tanstack/react-query"
import { ArrowLeft, Save, Trash2 } from "lucide-react"
import { useServer } from "../hooks/useServer"
import { useWorkflows } from "../hooks/useWorkflow"
import { queryKeys } from "../lib/query"
import { checkWorkflowFile, createWorkflowFile, deleteWorkflowFile, fetchWorkflowFile, saveWorkflowFile, starterWorkflow, WorkflowFileError, type WorkflowCheck, type WorkflowDiagnostic, type WorkflowFile } from "../lib/workflowFiles"
import { LazyWorkflowYamlEditor } from "../components/workflow-editor/LazyWorkflowYamlEditor"
import { LazyWorkflowGraph } from "../components/workflow-graph/LazyWorkflowGraph"
import { Button } from "../components/ui/Button"
import { LoadingState } from "../components/ui/LoadingState"
import { useMediaQuery } from "../hooks/useMediaQuery"

const CHECK_DELAY = 400
const field = "min-h-9 rounded-md border border-lw-line-strong bg-lw-ground px-2 text-sm text-lw-text"

type Conflict = { yaml: string; digest: string }

// useDebouncedCheck asks the server to check the text once typing pauses:
// its diagnostics and graph, for the text they are of.
function useDebouncedCheck(text: string | null) {
  const { baseUrl } = useServer()
  const [result, setResult] = useState<{ text: string; check: WorkflowCheck } | null>(null)
  const [error, setError] = useState("")
  useEffect(() => {
    if (text === null) return
    const controller = new AbortController()
    const timer = setTimeout(() => {
      checkWorkflowFile(baseUrl, text, controller.signal).then(check => { setResult({ text, check }); setError("") }, reason => {
        if (!controller.signal.aborted) setError(reason instanceof Error ? reason.message : String(reason))
      })
    }, CHECK_DELAY)
    return () => { clearTimeout(timer); controller.abort() }
  }, [baseUrl, text])
  return { result, error }
}

type Draft = { text: string; digest: string }

// A draft is the unsaved text kept in this browser, so that leaving the
// page by any way (another section, browser Back, a reload) loses nothing.
const draftKey = (baseUrl: string, name: string) => `legato:workflow-draft:${baseUrl}:${name}`

function readDraft(key: string): Draft | null {
  try {
    const raw = localStorage.getItem(key)
    const draft = raw ? JSON.parse(raw) as Draft : null
    return draft && typeof draft.text === "string" ? draft : null
  } catch { return null }
}

function writeDraft(key: string, draft: Draft | null) {
  try {
    if (draft) localStorage.setItem(key, JSON.stringify(draft))
    else localStorage.removeItem(key)
  } catch { /* kept only where storage works */ }
}

function Problems({ diagnostics }: { diagnostics: WorkflowDiagnostic[] }) {
  if (diagnostics.length === 0) return <p className="text-sm text-lw-text-2">No problems</p>
  return <ul aria-label="Problems" className="flex flex-col gap-1 text-[13px]">
    {diagnostics.map((d, i) => <li key={i} className={d.severity === "error" ? "text-lw-danger" : "text-lw-needs"}>
      <span className="font-lw-mono text-xs text-lw-faint">{d.line}:{d.column}</span> {d.severity === "warning" ? "Warning: " : ""}{d.message}
    </li>)}
  </ul>
}

// WorkflowEditPage is a workflow's YAML in the editor: a new one
// (/workflows/new, saved to a folder chosen here) or a file's real text
// (/workflows/:name/edit, saved over the version it was opened at).
export function WorkflowEditPage({ mode }: { mode: "new" | "edit" }) {
  const { name = "" } = useParams()
  const { baseUrl } = useServer()
  const navigate = useNavigate()
  const client = useQueryClient()
  const workflows = useWorkflows()
  const narrow = useMediaQuery("(max-width: 767px)")
  const [file, setFile] = useState<WorkflowFile | null>(null)
  const [loadError, setLoadError] = useState("")
  const [text, setText] = useState<string | null>(mode === "new" ? starterWorkflow : null)
  const [saved, setSaved] = useState("")
  const [digest, setDigest] = useState("")
  const [folder, setFolder] = useState("global")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const [conflict, setConflict] = useState<Conflict | null>(null)
  const [triggers, setTriggers] = useState<string[] | null>(null)
  const [deleting, setDeleting] = useState(false)
  const { result, error: checkError } = useDebouncedCheck(text)
  const [attempt, setAttempt] = useState(0)
  const key = draftKey(baseUrl, mode === "new" ? "new" : name)
  const [draft, setDraft] = useState<Draft | null>(null)
  // Nothing is written to the kept draft until it has been read, so
  // opening the page never removes the draft it is about to offer.
  const [draftRead, setDraftRead] = useState(false)

  useEffect(() => {
    if (mode !== "edit") {
      const kept = readDraft(key)
      if (kept && kept.text !== starterWorkflow) setDraft(kept)
      setDraftRead(true)
      return
    }
    let current = true
    setLoadError("")
    fetchWorkflowFile(baseUrl, name).then(loaded => {
      if (!current) return
      setLoadError("")
      setFile(loaded)
      setText(loaded.yaml)
      setSaved(loaded.yaml)
      setDigest(loaded.digest)
      const kept = readDraft(key)
      if (kept && kept.text !== loaded.yaml) setDraft(kept)
      setDraftRead(true)
    }, reason => { if (current) setLoadError(reason instanceof Error ? reason.message : String(reason)) })
    return () => { current = false }
  }, [baseUrl, mode, name, key, attempt])

  const dirty = text !== null && (mode === "new" ? text !== starterWorkflow : text !== saved)
  const dirtyRef = useRef(dirty)
  dirtyRef.current = dirty
  // The unsaved text is kept as a draft, with the digest it was begun at,
  // until it is saved or thrown away. An offered draft is not overwritten.
  useEffect(() => {
    if (!draftRead || draft || text === null) return
    writeDraft(key, dirty ? { text, digest } : null)
  }, [key, text, digest, dirty, draft, draftRead])
  useEffect(() => {
    const warn = (event: BeforeUnloadEvent) => { if (dirtyRef.current) event.preventDefault() }
    // A link to elsewhere in the app asks first: the router has no blocker.
    const click = (event: MouseEvent) => {
      if (!dirtyRef.current || event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey) return
      const link = (event.target as Element | null)?.closest?.("a[href]") as HTMLAnchorElement | null
      if (!link || link.target === "_blank") return
      const to = new URL(link.href, window.location.href)
      if (to.origin !== window.location.origin || to.pathname === window.location.pathname) return
      if (!window.confirm("Leave without saving? Your changes are kept in this browser as a draft.")) { event.preventDefault(); event.stopPropagation() }
    }
    window.addEventListener("beforeunload", warn)
    document.addEventListener("click", click, true)
    return () => { window.removeEventListener("beforeunload", warn); document.removeEventListener("click", click, true) }
  }, [])
  const forget = () => { dirtyRef.current = false; writeDraft(key, null) }

  const checked = result && result.text === text ? result.check : null
  const diagnostics = checked?.diagnostics ?? []
  const errors = diagnostics.filter(d => d.severity === "error")
  const editable = mode === "new" || Boolean(file?.editable)
  const canSave = editable && !busy && checked !== null && errors.length === 0 && (mode === "new" || dirty)
  const folders = workflows.data?.folders ?? []
  const back = mode === "new" ? "/workflows?tab=workflows" : `/workflows/${encodeURIComponent(name)}`

  const refresh = () => client.invalidateQueries({ queryKey: queryKeys.workflowDefinitions(baseUrl) })

  const save = async (confirmTriggers = false) => {
    if (text === null) return
    setBusy(true)
    setError("")
    try {
      if (mode === "new") {
        const created = await createWorkflowFile(baseUrl, text, folder, confirmTriggers)
        forget()
        await refresh()
        navigate(`/workflows/${encodeURIComponent(created.name)}`)
        return
      }
      const result = await saveWorkflowFile(baseUrl, name, text, digest, confirmTriggers)
      setDigest(result.digest)
      setSaved(text)
      setTriggers(null)
      await refresh()
    } catch (reason) {
      if (reason instanceof WorkflowFileError && reason.code === "stale") {
        setConflict({ yaml: String(reason.body.yaml ?? ""), digest: String(reason.body.digest ?? "") })
      } else if (reason instanceof WorkflowFileError && reason.code === "confirm_triggers") {
        setTriggers(reason.triggers)
      } else {
        setError(reason instanceof Error ? reason.message : String(reason))
      }
    } finally {
      setBusy(false)
    }
  }

  const remove = async () => {
    setBusy(true)
    try {
      await deleteWorkflowFile(baseUrl, name, digest)
      forget()
      await refresh()
      navigate("/workflows?tab=workflows")
    } catch (reason) {
      if (reason instanceof WorkflowFileError && reason.code === "stale") setError("The file changed on disk since you opened it: reload the page before deleting it.")
      else setError(reason instanceof Error ? reason.message : String(reason))
      setDeleting(false)
    } finally {
      setBusy(false)
    }
  }

  const frame = (children: React.ReactNode) => <main className="flex min-h-0 flex-1 flex-col overflow-y-auto px-4 py-4 font-lw text-lw-text sm:px-6 md:py-6">
    <div className="mx-auto flex w-full max-w-6xl flex-col gap-4">
      <Link to={back} className="inline-flex items-center gap-1.5 self-start text-sm text-lw-text-2 hover:text-lw-text"><ArrowLeft size={14} />{mode === "new" ? "Workflows" : "Back to the workflow"}</Link>
      {children}
    </div>
  </main>

  if (loadError) return frame(<div role="alert" className="flex flex-col items-start gap-2 text-sm text-lw-error">
    <p>Could not open the workflow: {loadError}</p>
    <Button size="sm" variant="outline" onClick={() => setAttempt(n => n + 1)}>Retry</Button>
  </div>)
  if (text === null) return frame(<LoadingState label="Opening the file…" size="inline" />)

  return frame(<>
    <header className="flex flex-wrap items-start justify-between gap-3">
      <div className="min-w-0">
        <h1 className="text-xl font-semibold">{mode === "new" ? "New workflow" : `Edit ${name}`}</h1>
        {file?.path && <p className="mt-1 break-all font-lw-mono text-xs text-lw-faint">{file.path}</p>}
        {mode === "edit" && file && !file.editable && <p className="mt-1 text-sm text-lw-text-2">This workflow can't be edited here: {file.reason}</p>}
      </div>
      <div className="flex flex-wrap items-center gap-2">
        {mode === "new" && <label className="flex items-center gap-2 text-xs text-lw-text-2">Folder
          <select aria-label="Folder" className={field} value={folder} onChange={event => setFolder(event.target.value)}>
            {(folders.length > 0 ? folders : [{ id: "global", label: "Your workflows", path: "" }]).map(f => <option key={f.id} value={f.id}>{f.label}</option>)}
          </select>
        </label>}
        {mode === "edit" && file?.editable && <Button variant="outline" size="sm" disabled={busy} onClick={() => setDeleting(true)}><Trash2 size={13} />Delete</Button>}
        {editable && <Button size="sm" disabled={!canSave} onClick={() => void save()}><Save size={13} />{mode === "new" ? "Create" : "Save"}</Button>}
      </div>
    </header>

    {draft && <section role="alertdialog" aria-label="Unsaved changes" className="flex flex-wrap items-center gap-2 rounded-lg border border-lw-needs bg-lw-surface p-3 text-sm">
      <p className="flex-1">You left this workflow with unsaved changes. They were kept in this browser.</p>
      <Button size="sm" variant="outline" onClick={() => { setText(draft.text); if (draft.digest) setDigest(draft.digest); setDraft(null) }}>Restore them</Button>
      <Button size="sm" variant="ghost" onClick={() => { writeDraft(key, null); setDraft(null) }}>Throw them away</Button>
    </section>}
        {error && <p role="alert" className="break-words rounded-lg border border-lw-danger-line bg-lw-error-soft p-3 text-sm text-lw-danger">{error}</p>}
    {checkError && <p role="alert" className="text-sm text-lw-error">Could not check the file: {checkError}</p>}

    {conflict && <section role="alertdialog" aria-label="The file changed" className="flex flex-col gap-2 rounded-lg border border-lw-needs bg-lw-surface p-3 text-sm">
      <p>The file changed on disk since you opened it. Reload it to see the change, or keep your edits and save them over it.</p>
      <pre className="max-h-48 overflow-auto rounded border border-lw-line bg-lw-ground p-2 font-lw-mono text-xs">{conflict.yaml}</pre>
      <div className="flex gap-2">
        <Button size="sm" variant="outline" onClick={() => { setText(conflict.yaml); setSaved(conflict.yaml); setDigest(conflict.digest); setConflict(null) }}>Reload</Button>
        <Button size="sm" variant="ghost" onClick={() => { setDigest(conflict.digest); setConflict(null) }}>Keep my edits</Button>
      </div>
    </section>}

    {triggers && <section role="alertdialog" aria-label="Switch on triggers" className="flex flex-col gap-2 rounded-lg border border-lw-needs bg-lw-surface p-3 text-sm">
      <p>This save switches on {triggers.join(", ")}: the trigger will start runs by itself.</p>
      <div className="flex gap-2">
        <Button size="sm" onClick={() => void save(true)} disabled={busy}>Save and switch on</Button>
        <Button size="sm" variant="ghost" onClick={() => setTriggers(null)}>Cancel</Button>
      </div>
    </section>}

    {deleting && <section role="alertdialog" aria-label="Delete the workflow" className="flex flex-col gap-2 rounded-lg border border-lw-danger-line bg-lw-surface p-3 text-sm">
      <p>Delete <code className="font-lw-mono text-xs">{file?.path}</code>? Its runs are kept; this Legato forgets its trigger switches.</p>
      <div className="flex gap-2">
        <Button size="sm" variant="destructive" onClick={() => void remove()} disabled={busy}>Delete</Button>
        <Button size="sm" variant="ghost" onClick={() => setDeleting(false)}>Cancel</Button>
      </div>
    </section>}

    <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_24rem]">
      <section aria-label="YAML" className={narrow ? "h-[28rem] overflow-hidden rounded-xl border border-lw-line" : "h-[36rem] overflow-hidden rounded-xl border border-lw-line"}>
        <LazyWorkflowYamlEditor value={text} onChange={setText} serverDiagnostics={diagnostics} readOnly={!editable} />
      </section>
      <div className="flex min-w-0 flex-col gap-4">
        <section aria-label="Check" className="rounded-xl border border-lw-line bg-lw-surface p-3">
          <h2 className="mb-2 text-xs font-semibold uppercase tracking-wide text-lw-text-2">{checked ? "Problems" : "Checking…"}</h2>
          {checked && <Problems diagnostics={diagnostics} />}
        </section>
        <section aria-label="Preview" className="h-72 overflow-hidden rounded-xl border border-lw-line bg-lw-ground">
          {checked?.graph ? <LazyWorkflowGraph graph={checked.graph} selectedId={null} onSelect={() => {}} direction={narrow ? "TB" : "LR"} /> : <p className="p-3 text-sm text-lw-text-2">The graph shows once the file is valid.</p>}
        </section>
      </div>
    </div>
  </>)
}
