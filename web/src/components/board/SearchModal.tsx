import { cn } from "../../lib/utils"
import { useState, useEffect, useRef, useCallback } from "react"
import { useServer } from "../../hooks/useServer"
import { searchTasks } from "../../lib/board"
import type { BoardCard } from "../../lib/board-types"

import { X, Search as SearchIcon } from "lucide-react"

interface SearchModalProps {
  open: boolean
  onClose: () => void
  onSelect: (card: BoardCard) => void
}

export function SearchModal({ open, onClose, onSelect }: SearchModalProps) {
  const { baseUrl } = useServer()
  const [query, setQuery] = useState("")
  const [results, setResults] = useState<BoardCard[]>([])
  const [cursor, setCursor] = useState(0)
  const [loading, setLoading] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)
  const debounceRef = useRef<ReturnType<typeof setTimeout>>(null)
  const abortRef = useRef<AbortController | null>(null)

  const doSearch = useCallback(
    async (q: string) => {
      if (!q.trim()) {
        setResults([])
        setCursor(0)
        return
      }
      if (abortRef.current) {
        abortRef.current.abort()
      }
      const controller = new AbortController()
      abortRef.current = controller
      setLoading(true)
      try {
        const cards = await searchTasks(baseUrl, q, controller.signal)
        setResults(cards)
        setCursor(0)
      } catch {
        setResults([])
      } finally {
        setLoading(false)
        if (abortRef.current === controller) {
          abortRef.current = null
        }
      }
    },
    [baseUrl]
  )

  useEffect(() => {
    if (!open) return
    setQuery("")
    setResults([])
    setCursor(0)
    setTimeout(() => inputRef.current?.focus(), 50)
  }, [open])

  useEffect(() => {
    if (debounceRef.current) clearTimeout(debounceRef.current)
    debounceRef.current = setTimeout(() => doSearch(query), 300)
    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current)
    }
  }, [query, doSearch])

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
          onSelect(results[cursor])
        }
      }
    }
    window.addEventListener("keydown", handler)
    return () => window.removeEventListener("keydown", handler)
  }, [open, results, cursor, onSelect])

  if (!open) return null

  return (
    <div className="fixed inset-0 z-50 flex items-start justify-center bg-black/60 p-4 pt-20" onClick={onClose}>
      <div className="w-full max-w-lg rounded-lg border border-zinc-700 bg-zinc-900 shadow-2xl" onClick={(e) => e.stopPropagation()}>
        <div className="flex items-center gap-2 border-b border-zinc-800 px-4 py-2">
          <SearchIcon size={14} className="text-zinc-500" />
          <input
            ref={inputRef}
            type="text"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search..."
            className="flex-1 bg-transparent text-sm text-zinc-200 outline-none placeholder:text-zinc-600"
          />
          <button onClick={onClose} className="rounded p-1 text-zinc-500 transition-colors hover:bg-zinc-800 hover:text-zinc-200">
            <X size={14} />
          </button>
        </div>
        <div className="max-h-80 overflow-y-auto px-2 py-2">
          {loading && (
            <div className="px-3 py-6 text-center text-xs text-zinc-500">Searching...</div>
          )}
          {!loading && query.trim() && results.length === 0 && (
            <div className="px-3 py-6 text-center text-xs text-zinc-500">No results</div>
          )}
          {!loading && results.map((card, i) => (
            <div
              key={card.id}
              onClick={() => onSelect(card)}
              className={cn(
                "cursor-pointer rounded px-3 py-2 text-sm transition-colors",
                i === cursor
                  ? "bg-zinc-800 text-zinc-200"
                  : "text-zinc-400 hover:bg-zinc-900/60 hover:text-zinc-300"
              )}
            >
              <div className="flex items-center gap-2">
                <span className="font-mono text-[10px] text-zinc-600">{card.id}</span>
                <span className="truncate">{card.title}</span>
                <span className="ml-auto text-[10px] text-zinc-600">{card.workspace_name || ""}</span>
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}
