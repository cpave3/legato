import { Dialog, DialogPanel, DialogHeader, DialogTitle, DialogBody, DialogFooter } from "../ui/Dialog"
import { useState, useEffect, useRef, useCallback } from "react"
import { useServer } from "../../hooks/useServer"
import { remoteImport, type ImportedTask } from "../../lib/board"
import type { RemoteSearchResult } from "../../lib/board-types"
import { Link } from "react-router-dom"
import { X } from "lucide-react"
import { ImportFields, ResultList } from "./ImportRemoteParts"
import { resultKey, useRemoteSearch, useTicketProviders } from "./useRemoteSearch"

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

// What an import sends: the ticket, its provider when the result names one,
// the workspace (or none), and a column only when one was picked.
function importRequest(r: RemoteSearchResult, workspaceId: number | null, column: string) {
  return {
    ticket_id: r.id,
    ...(r.provider ? { provider: r.provider } : {}),
    workspace_id: workspaceId,
    ...(column ? { column } : {}),
  }
}

export function ImportRemoteModal({ open, workspaces = [], view = "all", defaultWorkspaceId = null, columns = [], onClose, onImported }: ImportRemoteModalProps) {
  const { baseUrl } = useServer()
  const [query, setQuery] = useState("")
  const [importingID, setImportingID] = useState("")
  const startWorkspace = startingWorkspace(view, defaultWorkspaceId)
  const [workspaceId, setWorkspaceId] = useState<number | null>(startWorkspace)
  const [column, setColumn] = useState("")
  const [error, setError] = useState("")
  const inputRef = useRef<HTMLInputElement>(null)
  const hasProviders = useTicketProviders(open, baseUrl)
  const { results, cursor, setCursor, loading, clear } = useRemoteSearch(open, baseUrl, query, hasProviders, setError)

  useEffect(() => {
    if (!open) return
    setQuery("")
    clear()
    setWorkspaceId(startWorkspace)
    setColumn("")
    setError("")
    setImportingID("")
    // clear only sets state.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, startWorkspace])

  const handleImport = useCallback(async (r: RemoteSearchResult) => {
    if (importingID) return
    setImportingID(r.id)
    setError("")
    try {
      const task = await remoteImport(baseUrl, importRequest(r, workspaceId, column))
      onImported(task)
      onClose()
    } catch (e) {
      setError(e instanceof Error ? e.message : "Import failed")
    } finally {
      setImportingID("")
    }
  }, [importingID, baseUrl, workspaceId, column, onImported, onClose])

  // Arrows move through the results, so every letter types into the search,
  // and Enter imports the highlighted one unless an import is under way.
  useEffect(() => {
    if (!open) return
    const handler = (e: KeyboardEvent) => resultKey(e, results, cursor, setCursor, Boolean(importingID), handleImport)
    window.addEventListener("keydown", handler)
    return () => window.removeEventListener("keydown", handler)
  }, [open, results, cursor, setCursor, importingID, handleImport])

  if (!open) return null

  return (
    <Dialog open={open} onClose={onClose}>
      <DialogPanel className="w-full max-w-lg rounded-lg border border-zinc-700 bg-zinc-900 shadow-2xl">
        <DialogHeader className="flex items-center justify-between border-b border-zinc-800 px-5 py-3">
          <DialogTitle className="text-sm font-semibold text-zinc-200">Import ticket</DialogTitle>
          <button type="button" aria-label="Close" onClick={onClose} className="rounded p-1 text-zinc-500 transition-colors hover:bg-zinc-800 hover:text-zinc-200">
            <X size={16} />
          </button>
        </DialogHeader>
        <DialogBody className="space-y-0 p-0">
        {hasProviders === false ? (
          <div className="px-5 py-6 text-sm text-zinc-400">
            No ticket provider is connected. Connect Burnlite or Jira in{" "}
            <Link to="/settings" onClick={onClose} className="text-indigo-400 underline hover:text-indigo-300">Settings</Link>
            , then import its tickets here.
          </div>
        ) : (
        <>
        <ImportFields inputRef={inputRef} query={query} onQuery={setQuery} workspaces={workspaces} workspaceId={workspaceId} onWorkspace={setWorkspaceId} columns={columns} column={column} onColumn={setColumn} />
        {error && (
          <div role="alert" className="mx-4 mb-2 rounded border border-red-800 bg-red-950/50 px-3 py-2 text-xs text-red-300">{error}</div>
        )}
        <ResultList loading={loading} searched={query.trim().length >= 2} results={results} cursor={cursor} importingID={importingID} onImport={handleImport} />
        </>
        )}
        </DialogBody>
        <DialogFooter className="border-t border-zinc-800 px-5 py-2 text-[10px] text-zinc-600">↑/↓ to choose · Enter to import · Esc to cancel</DialogFooter>
      </DialogPanel>
    </Dialog>
  )
}
