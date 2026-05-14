import { useState, useEffect, useRef, useCallback } from "react"
import { useServer } from "../../hooks/useServer"
import { remoteSearch, remoteImport } from "../../lib/board"
import type { RemoteSearchResult } from "../../lib/board-types"
import { cn } from "../../lib/utils"
import { X } from "lucide-react"

interface ImportRemoteModalProps {
  open: boolean
  onClose: () => void
  onImported: () => void
}

export function ImportRemoteModal({ open, onClose, onImported }: ImportRemoteModalProps) {
  const { baseUrl } = useServer()
  const [query, setQuery] = useState("")
  const [results, setResults] = useState<RemoteSearchResult[]>([])
  const [cursor, setCursor] = useState(0)
  const [loading, setLoading] = useState(false)
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
    setError("")
    setTimeout(() => inputRef.current?.focus(), 50)
  }, [open])

  // Debounce search.
  useEffect(() => {
    const t = setTimeout(() => doSearch(query), 400)
    return () => clearTimeout(t)
  }, [query, doSearch])

  // Keyboard nav.
  useEffect(() => {
    if (!open) return
    const handler = (e: KeyboardEvent) => {
      if (e.key === "j") {
        e.preventDefault()
        setCursor((c) => Math.min(c + 1, results.length - 1))
      } else if (e.key === "k") {
        e.preventDefault()
        setCursor((c) => Math.max(c - 1, 0))
      } else if (e.key === "Enter") {
        e.preventDefault()
        if (results[cursor]) {
          handleImport(results[cursor])
        }
      }
    }
    window.addEventListener("keydown", handler)
    return () => window.removeEventListener("keydown", handler)
  }, [open, results, cursor])

  const handleImport = async (r: RemoteSearchResult) => {
    try {
      await remoteImport(baseUrl, { ticket_id: r.id })
      onImported()
      onClose()
    } catch (e) {
      setError(e instanceof Error ? e.message : "Import failed")
    }
  }

  if (!open) return null

  return (
    <div className="fixed inset-0 z-50 flex items-start justify-center bg-black/60 p-4 pt-20" onClick={onClose}>
      <div className="w-full max-w-lg rounded-lg border border-zinc-700 bg-zinc-900 shadow-2xl" onClick={(e) => e.stopPropagation()}>
        <div className="flex items-center justify-between border-b border-zinc-800 px-5 py-3">
          <h2 className="text-sm font-semibold text-zinc-200">Import Remote Ticket</h2>
          <button onClick={onClose} className="rounded p-1 text-zinc-500 transition-colors hover:bg-zinc-800 hover:text-zinc-200">
            <X size={16} />
          </button>
        </div>
        <div className="px-4 py-2">
          <input
            ref={inputRef}
            type="text"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Type to search..."
            className="w-full rounded border border-zinc-700 bg-zinc-950 px-3 py-2 text-sm text-zinc-200 outline-none focus:border-indigo-500"
          />
        </div>
        {error && (
          <div className="mx-4 mb-2 rounded border border-red-800 bg-red-950/50 px-3 py-2 text-xs text-red-300">{error}</div>
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
                key={r.id}
                onClick={() => handleImport(r)}
                className={cn(
                  "cursor-pointer rounded px-3 py-2 text-sm transition-colors",
                  i === cursor
                    ? "bg-zinc-800 text-zinc-200"
                    : "text-zinc-400 hover:bg-zinc-900/60 hover:text-zinc-300"
                )}
              >
                <div className="flex items-center gap-2">
                  <span className="font-mono text-[10px] text-zinc-600">{r.id}</span>
                  <span className="truncate">{r.summary}</span>
                  <span className="ml-auto text-[10px] text-zinc-600">{r.status}</span>
                </div>
              </div>
            ))}
        </div>
        <div className="border-t border-zinc-800 px-5 py-2 text-[10px] text-zinc-600">j/k to navigate · Enter to import · esc to cancel</div>
      </div>
    </div>
  )
}
