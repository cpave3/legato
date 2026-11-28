import type { RefObject } from "react"

import type { RemoteSearchResult } from "../../lib/board-types"
import { providerLabel } from "../../lib/providers"
import { cn } from "../../lib/utils"

// The Import ticket dialog's sections. See ImportRemoteModal.tsx.

const fieldClass = "w-full rounded border border-zinc-700 bg-zinc-950 px-3 py-2 text-sm text-zinc-200 outline-none focus:border-indigo-500"

// The search, and where an import lands: a workspace (or none) and a
// column (or the provider's default).
export function ImportFields({ inputRef, query, onQuery, workspaces, workspaceId, onWorkspace, columns, column, onColumn }: {
  inputRef: RefObject<HTMLInputElement | null>
  query: string
  onQuery: (query: string) => void
  workspaces: { id: number; name: string; color: string }[]
  workspaceId: number | null
  onWorkspace: (id: number | null) => void
  columns: string[]
  column: string
  onColumn: (column: string) => void
}) {
  return (
    <div className="grid gap-2 px-4 py-2 sm:grid-cols-[minmax(0,1fr)_9rem_9rem]">
      <label className="space-y-1 text-xs text-zinc-400">Search tickets<input
        ref={inputRef}
        data-autofocus
        aria-label="Search"
        type="text"
        value={query}
        onChange={(e) => onQuery(e.target.value)}
        placeholder="Type to search..."
        className={fieldClass}
      />
      </label>
      <label className="space-y-1 text-xs text-zinc-400">Workspace<select
        aria-label="Workspace"
        value={workspaceId ?? ""}
        onChange={(event) => onWorkspace(event.target.value ? Number(event.target.value) : null)}
        className={fieldClass}
      >
        <option value="">No workspace</option>
        {workspaces.map((workspace) => <option key={workspace.id} value={workspace.id}>{workspace.name}</option>)}
      </select></label>
      <label className="space-y-1 text-xs text-zinc-400">Column<select
        aria-label="Column"
        value={column}
        onChange={(event) => onColumn(event.target.value)}
        className={fieldClass}
      >
        <option value="">Provider default</option>
        {columns.map((name) => <option key={name} value={name}>{name}</option>)}
      </select></label>
    </div>
  )
}

function ResultRow({ result, highlighted, importingID, onImport }: { result: RemoteSearchResult; highlighted: boolean; importingID: string; onImport: (result: RemoteSearchResult) => void }) {
  return (
    <div
      data-remote-result
      onClick={() => !importingID && onImport(result)}
      aria-disabled={Boolean(importingID)}
      className={cn(
        "cursor-pointer rounded px-3 py-2 text-sm transition-colors",
        importingID && "pointer-events-none opacity-50",
        highlighted
          ? "bg-zinc-800 text-zinc-200"
          : "text-zinc-400 hover:bg-zinc-900/60 hover:text-zinc-300"
      )}
    >
      <div className="flex items-center gap-2">
        {result.provider && (
          <span data-provider title={providerLabel(result.provider)} className="shrink-0 rounded bg-zinc-800 px-1.5 py-0.5 font-mono text-[10px] text-zinc-400">{result.provider}</span>
        )}
        <span className="shrink-0 font-mono text-[10px] text-zinc-600">{result.id}</span>
        <span className="truncate">{importingID === result.id ? "Importing…" : result.summary}</span>
        <span className="ml-auto text-[10px] text-zinc-600">{result.status}</span>
      </div>
    </div>
  )
}

// The results: Searching while a search runs, No results when a search of
// two letters or more found none, else a row each.
export function ResultList({ loading, searched, results, cursor, importingID, onImport }: {
  loading: boolean
  searched: boolean
  results: RemoteSearchResult[]
  cursor: number
  importingID: string
  onImport: (result: RemoteSearchResult) => void
}) {
  return (
    <div className="max-h-80 overflow-y-auto px-2 pb-2">
      {loading ? <div className="px-3 py-6 text-center text-xs text-zinc-500">Searching...</div> : <>
        {searched && results.length === 0 && (
          <div className="px-3 py-6 text-center text-xs text-zinc-500">No results</div>
        )}
        {results.map((r, i) => <ResultRow key={`${r.provider ?? ""}:${r.id}`} result={r} highlighted={i === cursor} importingID={importingID} onImport={onImport} />)}
      </>}
    </div>
  )
}
