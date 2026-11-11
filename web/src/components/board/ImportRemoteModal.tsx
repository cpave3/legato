import { useState, useEffect, useRef, useCallback } from "react"
import { Link } from "react-router-dom"
import { useServer } from "../../hooks/useServer"
import { remoteSearch, remoteImport, fetchTicketProviders, type ImportedTask } from "../../lib/board"
import type { RemoteSearchResult } from "../../lib/board-types"
import { cn } from "../../lib/utils"
import { providerLabel } from "../../lib/providers"
import { X } from "lucide-react"

interface ImportRemoteModalProps {
  open: boolean
  workspaces?: { id: number; name: string; color: string }[]
  /** The board's workspace view: "all", "unassigned" or a workspace's id. */
  view?: string
  /** The workspace an import from the All view starts in (settings.default_import_workspace_id). */
  defaultWorkspaceId?: number | null
  /** The board's columns, offered after "Provider default". */
  columns?: string[]
  onClose: () => void
  onImported: (task: ImportedTask) => void
}

// The workspace the dialog starts with: the board's, else none for the
// Unassigned view, else the configured default.
function startingWorkspace(view: string, defaultWorkspaceId: number | null): number | null {
  if (view === "unassigned") return null
  const id = Number(view)
  if (view !== "all" && view !== "" && Number.isInteger(id)) return id
  return defaultWorkspaceId
}

export function ImportRemoteModal({ open, workspaces = [], view = "all", defaultWorkspaceId = null, columns = [], onClose, onImported }: ImportRemoteModalProps) {
  const { baseUrl } = useServer()
  const [query, setQuery] = useState("")
  const [results, setResults] = useState<RemoteSearchResult[]>([])
  const [cursor, setCursor] = useState(0)
  const [loading, setLoading] = useState(false)
  const [importingID, setImportingID] = useState("")
  const startWorkspace = startingWorkspace(view, defaultWorkspaceId)
  const [workspaceId, setWorkspaceId] = useState<number | null>(startWorkspace)
  const [column, setColumn] = useState("")
  // false: no provider is connected; null: not known (yet, or an older server).
  const [hasProviders, setHasProviders] = useState<boolean | null>(null)
  const [error, setError] = useState("")
  const inputRef = useRef<HTMLInputElement>(null)

  const doSearch = useCallback(
    async (q: string) => {
      if (q.trim().length < 2) {
        setResults([])
        setCursor(0)
        return
      }
      setLoading(true)
      setError("")
      try {
        const r = await remoteSearch(baseUrl, q)
        setResults(r)
        setCursor(0)
      } catch (e) {
        setResults([])
        setError(e instanceof Error ? e.message : "Search failed")
      } finally {
        setLoading(false)
      }
    },
    [baseUrl]
  )

  useEffect(() => {
    if (!open) return
    setQuery("")
    setResults([])
    setCursor(0)
    setWorkspaceId(startWorkspace)
    setColumn("")
    setError("")
    setImportingID("")
    setTimeout(() => inputRef.current?.focus(), 50)
  }, [open, startWorkspace])

  // Whether any ticket provider is connected, asked each time it opens.
  useEffect(() => {
    if (!open) return
    let current = true
    setHasProviders(null)
    fetchTicketProviders(baseUrl)
      .then((providers) => { if (current) setHasProviders(providers.length > 0) })
      .catch(() => { if (current) setHasProviders(null) })
    return () => { current = false }
  }, [open, baseUrl])

  // Debounce search while the dialog is visible.
  useEffect(() => {
    if (!open || hasProviders === false) return
    const t = setTimeout(() => doSearch(query), 400)
    return () => clearTimeout(t)
  }, [open, query, doSearch, hasProviders])

  const handleImport = useCallback(async (r: RemoteSearchResult) => {
    if (importingID) return
    setImportingID(r.id)
    setError("")
    try {
      const task = await remoteImport(baseUrl, {
        ticket_id: r.id,
        ...(r.provider ? { provider: r.provider } : {}),
        workspace_id: workspaceId,
        ...(column ? { column } : {}),
      })
      onImported(task)
      onClose()
    } catch (e) {
      setError(e instanceof Error ? e.message : "Import failed")
    } finally {
      setImportingID("")
    }
  }, [importingID, baseUrl, workspaceId, column, onImported, onClose])

  // Keyboard nav: arrows, so every letter types into the search.
  useEffect(() => {
    if (!open) return
    const handler = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.preventDefault()
        onClose()
      } else if (e.key === "ArrowDown") {
        e.preventDefault()
        setCursor((c) => Math.min(c + 1, results.length - 1))
      } else if (e.key === "ArrowUp") {
        e.preventDefault()
        setCursor((c) => Math.max(c - 1, 0))
      } else if (e.key === "Enter") {
        e.preventDefault()
        if (results[cursor] && !importingID) {
          handleImport(results[cursor])
        }
      }
    }
    window.addEventListener("keydown", handler)
    return () => window.removeEventListener("keydown", handler)
  }, [open, results, cursor, importingID, onClose, handleImport])

  if (!open) return null

  return (
    <div className="fixed inset-0 z-50 flex items-start justify-center bg-black/60 p-4 pt-20" onClick={onClose}>
      <div className="w-full max-w-lg rounded-lg border border-zinc-700 bg-zinc-900 shadow-2xl" onClick={(e) => e.stopPropagation()}>
        <div className="flex items-center justify-between border-b border-zinc-800 px-5 py-3">
          <h2 className="text-sm font-semibold text-zinc-200">Import ticket</h2>
          <button type="button" aria-label="Close" onClick={onClose} className="rounded p-1 text-zinc-500 transition-colors hover:bg-zinc-800 hover:text-zinc-200">
            <X size={16} />
          </button>
        </div>
        {hasProviders === false ? (
          <div className="px-5 py-6 text-sm text-zinc-400">
            No ticket provider is connected. Connect Burnlite or Jira in{" "}
            <Link to="/settings" onClick={onClose} className="text-indigo-400 underline hover:text-indigo-300">Settings</Link>
            , then import its tickets here.
          </div>
        ) : (
        <>
        <div className="grid gap-2 px-4 py-2 sm:grid-cols-[minmax(0,1fr)_9rem_9rem]">
          <input
            ref={inputRef}
            type="text"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Type to search..."
            className="w-full rounded border border-zinc-700 bg-zinc-950 px-3 py-2 text-sm text-zinc-200 outline-none focus:border-indigo-500"
          />
          <select
            aria-label="Workspace"
            value={workspaceId ?? ""}
            onChange={(event) => setWorkspaceId(event.target.value ? Number(event.target.value) : null)}
            className="w-full rounded border border-zinc-700 bg-zinc-950 px-3 py-2 text-sm text-zinc-200 outline-none focus:border-indigo-500"
          >
            <option value="">No workspace</option>
            {workspaces.map((workspace) => <option key={workspace.id} value={workspace.id}>{workspace.name}</option>)}
          </select>
          <select
            aria-label="Column"
            value={column}
            onChange={(event) => setColumn(event.target.value)}
            className="w-full rounded border border-zinc-700 bg-zinc-950 px-3 py-2 text-sm text-zinc-200 outline-none focus:border-indigo-500"
          >
            <option value="">Provider default</option>
            {columns.map((name) => <option key={name} value={name}>{name}</option>)}
          </select>
        </div>
        {error && (
          <div role="alert" className="mx-4 mb-2 rounded border border-red-800 bg-red-950/50 px-3 py-2 text-xs text-red-300">{error}</div>
        )}
        <div className="max-h-80 overflow-y-auto px-2 pb-2">
          {loading && (
            <div className="px-3 py-6 text-center text-xs text-zinc-500">Searching...</div>
          )}
          {!loading && query.trim().length >= 2 && results.length === 0 && (
            <div className="px-3 py-6 text-center text-xs text-zinc-500">No results</div>
          )}
          {!loading &&
            results.map((r, i) => (
              <div
                key={`${r.provider ?? ""}:${r.id}`}
                data-remote-result
                onClick={() => !importingID && handleImport(r)}
                aria-disabled={Boolean(importingID)}
                className={cn(
                  "cursor-pointer rounded px-3 py-2 text-sm transition-colors",
                  importingID && "pointer-events-none opacity-50",
                  i === cursor
                    ? "bg-zinc-800 text-zinc-200"
                    : "text-zinc-400 hover:bg-zinc-900/60 hover:text-zinc-300"
                )}
              >
                <div className="flex items-center gap-2">
                  {r.provider && (
                    <span data-provider title={providerLabel(r.provider)} className="shrink-0 rounded bg-zinc-800 px-1.5 py-0.5 font-mono text-[10px] text-zinc-400">{r.provider}</span>
                  )}
                  <span className="shrink-0 font-mono text-[10px] text-zinc-600">{r.id}</span>
                  <span className="truncate">{importingID === r.id ? "Importing…" : r.summary}</span>
                  <span className="ml-auto text-[10px] text-zinc-600">{r.status}</span>
                </div>
              </div>
            ))}
        </div>
        </>
        )}
        <div className="border-t border-zinc-800 px-5 py-2 text-[10px] text-zinc-600">↑/↓ to choose · Enter to import · Esc to cancel</div>
      </div>
    </div>
  )
}
