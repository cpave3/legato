import { lazy, Suspense, useEffect, useMemo, useRef, useState } from "react"
import { Link, useLocation, useNavigate, useParams } from "react-router-dom"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { fetchMacroList } from "../lib/macros"
import { fetchAdapters } from "../lib/adapters"
import { fetchBoard } from "../lib/board"
import { listRepositories } from "../lib/repositories"
import { AlertTriangle, ArrowLeft, Redo2, Save, Trash2, Undo2 } from "lucide-react"
import { useServer } from "../hooks/useServer"
import { useWorkflows } from "../hooks/useWorkflow"
import { queryKeys } from "../lib/query"
import { checkWorkflowFile, createWorkflowFile, deleteWorkflowFile, fetchWorkflowFile, offsetOf, saveWorkflowFile, starterWorkflow, WorkflowFileError, type WorkflowCheck, type WorkflowDiagnostic, type WorkflowFile } from "../lib/workflowFiles"
import { LazyWorkflowYamlEditor } from "../components/workflow-editor/LazyWorkflowYamlEditor"
import { LazyWorkflowOutline } from "../components/workflow-editor/LazyWorkflowOutline"
import type { HeldYaml } from "../components/workflow-editor/WorkflowOutline"
import { KIND_TITLES } from "../components/workflow-editor/triggerTitles"
import { setWorkflowTrigger } from "../lib/workflow"
import type { WorkflowGraphModel } from "../lib/workflowGraph"

// Drops held item YAML that no longer belongs to the text, in either mode.
const HeldYamlReconciler = lazy(() => import("../components/workflow-editor/HeldYamlReconciler"))
import { addressOf, selectionOf, type Selection } from "../lib/workflowAddress"
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

// useBuilderOptions is what the builder's pickers offer: this Legato's
// macros, agent tools with their tiers, and board columns. Each is left
// empty when it can't be read; a value can still be typed.
function useBuilderOptions(baseUrl: string) {
  const quiet = async <T,>(read: () => Promise<T>, fallback: T) => { try { return await read() } catch { return fallback } }
  const query = useQuery({
    queryKey: [baseUrl, "workflow-builder-options"],
    queryFn: async () => {
      const [macros, adapters, board, repositories] = await Promise.all([
        quiet(() => fetchMacroList(baseUrl), null),
        quiet(() => fetchAdapters(baseUrl), { adapters: [], default: "" }),
        quiet(() => fetchBoard(baseUrl), null),
        quiet(() => listRepositories(baseUrl), []),
      ])
      return {
        macros: (macros?.macros ?? []).map(macro => macro.name),
        agents: adapters.adapters,
        tiers: Object.fromEntries(Object.entries(macros?.tiers ?? {}).map(([agent, tiers]) => [agent, tiers.map(tier => tier.name)])),
        columns: (board?.columns ?? []).map(column => column.name),
        repositories: [...new Set(repositories.map(repository => repository.repo).filter(Boolean))],
      }
    },
    staleTime: 60_000,
  })
  return query.data
}

type Entry = { text: string; label: string; typing: boolean; at?: number }

const TYPING_PAUSE = 1000

// useHistory is the text's undo history, across the YAML and Visual modes:
// each builder change is a step; typing in YAML is one step until the
// next change of another kind, or a pause of TYPING_PAUSE. The YAML editor
// keeps no history of its own: its Undo and Redo keys come here.
function useHistory(initial: string | null) {
  // sealed: after Undo or Redo, typing starts a new step rather than
  // changing the one returned to.
  const [state, setState] = useState<{ entries: Entry[]; index: number; sealed?: boolean }>(() => ({ entries: initial === null ? [] : [{ text: initial, label: "", typing: false }], index: 0 }))
  const current = state.entries[state.index]
  return {
    text: current ? current.text : null,
    canUndo: state.index > 0,
    canRedo: state.index < state.entries.length - 1,
    undoLabel: current?.label ?? "",
    redoLabel: state.entries[state.index + 1]?.label ?? "",
    reset: (text: string) => setState({ entries: [{ text, label: "", typing: false }], index: 0 }),
    push: (text: string, label: string, typing = false) => setState(previous => {
      const here = previous.entries[previous.index]
      if (here && here.text === text) return previous
      const now = Date.now()
      if (typing && here?.typing && !previous.sealed && now - (here.at ?? now) < TYPING_PAUSE) {
        const entries = [...previous.entries.slice(0, previous.index), { ...here, text, at: now }]
        return { entries, index: previous.index }
      }
      const entries = [...previous.entries.slice(0, previous.index + 1), { text, label, typing, at: now }]
      return { entries, index: entries.length - 1 }
    }),
    undo: () => setState(previous => ({ ...previous, index: Math.max(0, previous.index - 1), sealed: true })),
    redo: () => setState(previous => ({ ...previous, index: Math.min(previous.entries.length - 1, previous.index + 1), sealed: true })),
  }
}

type EditorMode = "visual" | "yaml"
const MODE_KEY = "legato:workflow-editor-mode"

function storedMode(): EditorMode {
  try { return localStorage.getItem(MODE_KEY) === "yaml" ? "yaml" : "visual" } catch { return "visual" }
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
  const location = useLocation()
  // The row the inspector edits is the page's address (#steps/review), so
  // a reload and Back keep it. On a phone it is the inspector's sheet: a
  // row opened is a new entry, which Back closes.
  const selection = selectionOf(location.hash)
  const sheetPushed = Boolean((location.state as { sheet?: boolean } | null)?.sheet)
  const select = (next: Selection | null, how?: { open?: boolean }) => {
    const hash = next ? `#${addressOf(next)}` : ""
    if (hash === location.hash) return
    const push = narrow && how?.open && next !== null
    navigate({ pathname: location.pathname, search: location.search, hash }, { replace: !push, state: push ? { sheet: true } : location.state })
  }
  const closeSheet = () => {
    if (sheetPushed) navigate(-1)
    else navigate({ pathname: location.pathname, search: location.search, hash: "" }, { replace: true })
  }
  const [showProblems, setShowProblems] = useState(false)
  // Item YAML the outline holds until it reads (LEG-505): kept here so it
  // outlives a switch to the whole file's YAML, and counted as unsaved.
  const [heldYaml, setHeldYaml] = useState<HeldYaml>({})
  // YAML mode's outline beside the editor (LEG-512): the cursor marks a
  // row, and a row (or Show on a broken file) moves the cursor.
  const [cursor, setCursor] = useState<number | undefined>(undefined)
  const [jump, setJump] = useState<{ from: number; to: number; seq: number } | undefined>(undefined)
  const [lastGood, setLastGood] = useState<string | null>(null)
  // The map is the last graph the check could make: stale while the text
  // now is not what it was made of.
  const [lastGraph, setLastGraph] = useState<{ text: string; model: WorkflowGraphModel } | null>(null)
  const jumpTo = (range: { from: number; to: number }) => setJump(previous => ({ ...range, seq: (previous?.seq ?? 0) + 1 }))
  // Saved · paused · Turn on (LEG-509): after a save, a trigger this
  // Legato has paused is named, with a way to switch it on.
  const [savedNotice, setSavedNotice] = useState(Boolean((location.state as { saved?: boolean } | null)?.saved))
  // Create goes on to this editor on the same page, which then only sees a
  // new location: its saved state says so.
  const savedByNavigation = Boolean((location.state as { saved?: boolean } | null)?.saved)
  useEffect(() => { if (savedByNavigation) setSavedNotice(true) }, [savedByNavigation, location.key])
  const [turningOn, setTurningOn] = useState<"ask" | "busy" | "done" | null>(null)
  const triggerStates = useMemo(() => workflows.data?.trigger_states?.[name] ?? [], [workflows.data, name])
  const pausedHere = useMemo(() => Object.fromEntries(triggerStates.map(state => [state.trigger, !state.enabled])), [triggerStates])
  const pausedTriggers = triggerStates.filter(state => !state.enabled).map(state => state.trigger)
  const turnOn = async () => {
    setTurningOn("busy")
    try {
      for (const trigger of pausedTriggers) await setWorkflowTrigger(baseUrl, name, trigger, true)
      await workflows.refresh?.()
      setTurningOn("done")
    } catch (reason) {
      setTurningOn(null)
      setError(reason instanceof Error ? reason.message : String(reason))
    }
  }
  const [file, setFile] = useState<WorkflowFile | null>(null)
  const [loadError, setLoadError] = useState("")
  const history = useHistory(mode === "new" ? starterWorkflow : null)
  const text = history.text
  const [editorMode, setEditorMode] = useState<EditorMode>(storedMode)
  const switchMode = (next: EditorMode) => {
    setEditorMode(next)
    try { localStorage.setItem(MODE_KEY, next) } catch { /* only remembered where storage works */ }
  }
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
  const options = useBuilderOptions(baseUrl)

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
      history.reset(loaded.yaml)
      setSaved(loaded.yaml)
      setDigest(loaded.digest)
      const kept = readDraft(key)
      if (kept && kept.text !== loaded.yaml) setDraft(kept)
      setDraftRead(true)
    }, reason => { if (current) setLoadError(reason instanceof Error ? reason.message : String(reason)) })
    return () => { current = false }
    // history.reset is a fresh function each render; the file loads once.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [baseUrl, mode, name, key, attempt])

  const [pendingEdits, setPendingEdits] = useState(false)
  const dirty = pendingEdits || Object.keys(heldYaml).length > 0 || (text !== null && (mode === "new" ? text !== starterWorkflow : text !== saved))
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
  useEffect(() => { if (result?.check.graph) setLastGraph({ text: result.text, model: result.check.graph }) }, [result])
  const graph = lastGraph ? { model: lastGraph.model, stale: lastGraph.text !== text } : null
  const diagnostics = checked?.diagnostics ?? []
  const errors = diagnostics.filter(d => d.severity === "error")
  const editable = mode === "new" || Boolean(file?.editable)
  const canSave = editable && !busy && checked !== null && errors.length === 0 && (mode === "new" || dirty)
  const folders = workflows.data?.folders ?? []
  // y switches between Visual and YAML, Mod-s saves, outside text fields.
  const keys = useRef({ save: () => {}, canSave: false, toggle: () => {} })
  keys.current = { save: () => void save(), canSave, toggle: () => switchMode(editorMode === "yaml" ? "visual" : "yaml") }
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && !event.altKey && event.key.toLowerCase() === "s") {
        event.preventDefault()
        if (keys.current.canSave) keys.current.save()
        return
      }
      const target = event.target as HTMLElement | null
      const typing = Boolean(target && (target.tagName === "INPUT" || target.tagName === "TEXTAREA" || target.tagName === "SELECT" || target.isContentEditable))
      if (!typing && !event.metaKey && !event.ctrlKey && !event.altKey && event.key === "y") { event.preventDefault(); keys.current.toggle() }
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [])
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
        // A workflow saved with a paused trigger stays in the editor, which
        // says so and offers to turn it on; any other opens its page.
        const paused = /^\s+enabled:\s*false\b/m.test(text)
        navigate(paused ? `/workflows/${encodeURIComponent(created.name)}/edit` : `/workflows/${encodeURIComponent(created.name)}`, { state: paused ? { saved: true } : undefined })
        return
      }
      const result = await saveWorkflowFile(baseUrl, name, text, digest, confirmTriggers)
      setDigest(result.digest)
      setSaved(text)
      setTriggers(null)
      setSavedNotice(true)
      setTurningOn(null)
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

  const parsedTitle = text !== null ? /^title:[ \t]*(.+?)[ \t]*(#.*)?$/m.exec(text)?.[1]?.replace(/^["']|["']$/g, "") : undefined
  const headerTitle = parsedTitle || (mode === "new" ? "New workflow" : name)
  const fileName = mode === "new" ? (text !== null ? /^name:[ \t]*(\S+)/m.exec(text)?.[1] ?? "" : "") : name
  const folderLabel = folders.find(f => f.id === (mode === "new" ? folder : file?.folder))?.label ?? (mode === "new" ? "Your workflows" : "")
  const problemCount = diagnostics.filter(d => d.severity === "error").length
  const problemsButton = <Button size="sm" variant="ghost" aria-expanded={editorMode === "visual" ? showProblems : undefined} onClick={() => setShowProblems(!showProblems)} className={problemCount > 0 ? "text-lw-danger" : "text-lw-text-2"}>
    <AlertTriangle size={13} />{checked ? (problemCount === 0 ? "No problems" : `${problemCount} ${problemCount === 1 ? "problem" : "problems"}`) : "Checking…"}
  </Button>
  const saveButton = <Button size="sm" disabled={!canSave} onClick={() => void save()}><Save size={13} />{mode === "new" ? "Create" : "Save"}</Button>

  if (loadError) return frame(<div role="alert" className="flex flex-col items-start gap-2 text-sm text-lw-error">
    <p>Could not open the workflow: {loadError}</p>
    <Button size="sm" variant="outline" onClick={() => setAttempt(n => n + 1)}>Retry</Button>
  </div>)
  if (text === null) return frame(<LoadingState label="Opening the file…" size="inline" />)

  return frame(<>
    <header className="flex flex-wrap items-start justify-between gap-3">
      <div className="min-w-0">
        <h1 className="text-xl font-semibold">{headerTitle}</h1>
        <p className="mt-0.5 break-all text-xs text-lw-faint"><span className="font-lw-mono">{fileName}</span>{folderLabel ? ` · ${folderLabel}` : ""}</p>
        {mode === "edit" && file && !file.editable && <p className="mt-1 text-sm text-lw-text-2">This workflow can't be edited here: {file.reason}</p>}
      </div>
      <div className="flex flex-wrap items-center gap-2">
        {!narrow && problemsButton}
        <div role="radiogroup" aria-label="Editor" className="inline-flex rounded-md border border-lw-line p-0.5">
          {(["visual", "yaml"] as const).map(option => <button key={option} type="button" role="radio" aria-checked={editorMode === option}
            className={`min-h-8 rounded px-3 text-xs ${editorMode === option ? "bg-lw-raised text-lw-text" : "text-lw-text-2 hover:text-lw-text"}`}
            onClick={() => switchMode(option)}>{option === "visual" ? "Visual" : "YAML"}</button>)}
        </div>
        {editable && <div className="flex gap-1">
          <Button size="sm" variant="ghost" disabled={!history.canUndo} title={history.undoLabel ? `Undo ${history.undoLabel}` : "Undo"} onClick={history.undo}><Undo2 size={13} />Undo</Button>
          <Button size="sm" variant="ghost" disabled={!history.canRedo} title={history.redoLabel ? `Redo ${history.redoLabel}` : "Redo"} onClick={history.redo}><Redo2 size={13} />Redo</Button>
        </div>}
        {mode === "new" && <label className="flex items-center gap-2 text-xs text-lw-text-2">Folder
          <select aria-label="Folder" className={field} value={folder} onChange={event => setFolder(event.target.value)}>
            {(folders.length > 0 ? folders : [{ id: "global", label: "Your workflows", path: "" }]).map(f => <option key={f.id} value={f.id}>{f.label}</option>)}
          </select>
        </label>}
        {mode === "edit" && file?.editable && <Button variant="outline" size="sm" disabled={busy} onClick={() => setDeleting(true)}><Trash2 size={13} />Delete</Button>}
        {editable && !narrow && saveButton}
      </div>
    </header>
    {showProblems && editorMode === "visual" && <section aria-label="Check" className="rounded-xl border border-lw-line bg-lw-surface p-3">
      <Problems diagnostics={diagnostics} />
    </section>}

    {savedNotice && mode === "edit" && (pausedTriggers.length > 0 || turningOn === "done") && <section role="status" aria-label="Saved" className="flex flex-wrap items-center gap-2 rounded-lg border border-lw-line bg-lw-surface p-3 text-sm">
      {turningOn === "done"
        ? <p className="flex-1">Saved · on: it starts runs by itself.</p>
        : <>
          <p className="min-w-0 flex-1"><span className="font-medium">Saved · paused</span> <span className="text-lw-text-2">· {pausedTriggers.map(t => KIND_TITLES[t] ?? t).join(", ")} won't start runs by itself until you turn it on.</span></p>
          {turningOn === "ask" || turningOn === "busy"
            ? <span role="alertdialog" aria-label="Turn on" className="flex flex-wrap items-center gap-2 text-xs">
              <span>It will start runs by itself.</span>
              <Button size="sm" disabled={turningOn === "busy"} onClick={() => void turnOn()}>Turn on</Button>
              <Button size="sm" variant="ghost" onClick={() => setTurningOn(null)}>Not now</Button>
            </span>
            : <Button size="sm" variant="outline" onClick={() => setTurningOn("ask")}>Turn on</Button>}
        </>}
      <Button size="sm" variant="ghost" aria-label="Dismiss" onClick={() => { setSavedNotice(false); setTurningOn(null) }}>×</Button>
    </section>}
    {draft && <section role="alertdialog" aria-label="Unsaved changes" className="flex flex-wrap items-center gap-2 rounded-lg border border-lw-needs bg-lw-surface p-3 text-sm">
      <p className="flex-1">You left this workflow with unsaved changes. They were kept in this browser.</p>
      <Button size="sm" variant="outline" onClick={() => { history.push(draft.text, "Restore unsaved changes"); if (draft.digest) setDigest(draft.digest); setDraft(null) }}>Restore them</Button>
      <Button size="sm" variant="ghost" onClick={() => { writeDraft(key, null); setDraft(null) }}>Throw them away</Button>
    </section>}
        {error && <p role="alert" className="break-words rounded-lg border border-lw-danger-line bg-lw-error-soft p-3 text-sm text-lw-danger">{error}</p>}
    {checkError && <p role="alert" className="text-sm text-lw-error">Could not check the file: {checkError}</p>}

    {conflict && <section role="alertdialog" aria-label="The file changed" className="flex flex-col gap-2 rounded-lg border border-lw-needs bg-lw-surface p-3 text-sm">
      <p>The file changed on disk since you opened it. Reload it to see the change, or keep your edits and save them over it.</p>
      <pre className="max-h-48 overflow-auto rounded border border-lw-line bg-lw-ground p-2 font-lw-mono text-xs">{conflict.yaml}</pre>
      <div className="flex gap-2">
        <Button size="sm" variant="outline" onClick={() => { history.reset(conflict.yaml); setSaved(conflict.yaml); setDigest(conflict.digest); setConflict(null) }}>Reload</Button>
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

    {editorMode === "yaml" || !editable
      ? <div className="flex flex-col gap-4">
        <div className={editable && !narrow ? "grid gap-4 md:grid-cols-[minmax(0,18rem)_minmax(0,1fr)] lg:grid-cols-[minmax(0,22rem)_minmax(0,1fr)]" : "flex flex-col gap-2"}>
          {editable && <section aria-label="Navigator" className={narrow ? "" : "max-h-[36rem] overflow-y-auto rounded-xl border border-lw-line bg-lw-surface p-2"}>
            <LazyWorkflowOutline mode="navigate" text={text} cursor={cursor} onJump={jumpTo} onEdit={() => {}} diagnostics={diagnostics} onEditYaml={line => { if (line) { const at = offsetOf(text, line, 1); jumpTo({ from: at, to: at }) } }}
              options={options} selection={null} onSelect={() => {}} narrow={narrow} pausedHere={pausedHere} lastGood={lastGood} onGood={setLastGood} graph={graph} />
          </section>}
          <section aria-label="YAML" className={narrow ? "h-[70vh] overflow-hidden rounded-xl border border-lw-line" : "h-[36rem] overflow-hidden rounded-xl border border-lw-line"}>
            <LazyWorkflowYamlEditor value={text} onChange={next => history.push(next, "Edit YAML", true)} serverDiagnostics={diagnostics} readOnly={!editable} onUndo={editable ? history.undo : undefined} onRedo={editable ? history.redo : undefined} jumpTo={jump} onCursor={setCursor} />
          </section>
        </div>
        <section aria-label="Check" className="rounded-xl border border-lw-line bg-lw-surface p-3">
          <h2 className="mb-2 text-xs font-semibold uppercase tracking-wide text-lw-text-2">{checked ? "Problems" : "Checking…"}</h2>
          {checked && <Problems diagnostics={diagnostics} />}
        </section>
      </div>
      : <section aria-label="Visual" className={narrow ? "pb-16" : undefined}>
        <LazyWorkflowOutline text={text} onEdit={(change, how) => history.push(change.text, change.undo.label, how?.typing)} diagnostics={diagnostics} nameFixed={mode === "edit"} onEditYaml={line => { switchMode("yaml"); if (line) { const at = offsetOf(text, line, 1); jumpTo({ from: at, to: at }) } }} options={options} schedule={checked?.schedule} onPending={setPendingEdits}
          savedText={mode === "edit" ? saved : null} pausedHere={pausedHere} lastGood={lastGood} onGood={setLastGood} graph={graph} selection={selection} onSelect={select} narrow={narrow} sheet={narrow && selection !== null} onCloseSheet={closeSheet} onUndo={history.undo} onRedo={history.redo} held={heldYaml} onHeld={setHeldYaml} />
      </section>}
    {text !== null && Object.keys(heldYaml).length > 0 && <Suspense fallback={null}><HeldYamlReconciler text={text} held={heldYaml} onHeld={setHeldYaml} /></Suspense>}
    {narrow && editable && <div role="toolbar" aria-label="Save" className="fixed inset-x-0 bottom-0 z-50 flex items-center justify-between gap-2 border-t border-lw-line bg-lw-surface px-4 py-2">
      {problemsButton}
      {saveButton}
    </div>}
  </>)
}
