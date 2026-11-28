import { useCallback, useEffect, useState, type Dispatch, type SetStateAction } from "react"

import { fetchTicketProviders, remoteSearch } from "../../lib/board"
import type { RemoteSearchResult } from "../../lib/board-types"

// The Import ticket dialog's state machines: whether a provider is
// connected, the debounced search with its highlighted result, and the keys
// that move through the results. See ImportRemoteModal.tsx.

// Whether any ticket provider is connected, asked each time the dialog
// opens. false: none is; null: not known (yet, or an older server).
export function useTicketProviders(open: boolean, baseUrl: string): boolean | null {
  const [hasProviders, setHasProviders] = useState<boolean | null>(null)
  useEffect(() => {
    if (!open) return
    let current = true
    setHasProviders(null)
    fetchTicketProviders(baseUrl)
      .then((providers) => { if (current) setHasProviders(providers.length > 0) })
      .catch(() => { if (current) setHasProviders(null) })
    return () => { current = false }
  }, [open, baseUrl])
  return hasProviders
}

// The search of the typed query, 400ms after typing stops while the dialog
// is visible and a provider may be connected. Under two letters nothing is
// searched and the results empty.
export function useRemoteSearch(open: boolean, baseUrl: string, query: string, hasProviders: boolean | null, setError: (error: string) => void) {
  const [results, setResults] = useState<RemoteSearchResult[]>([])
  const [cursor, setCursor] = useState(0)
  const [loading, setLoading] = useState(false)

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
        setResults(await remoteSearch(baseUrl, q))
        setCursor(0)
      } catch (e) {
        setResults([])
        setError(e instanceof Error ? e.message : "Search failed")
      } finally {
        setLoading(false)
      }
    },
    // setError is a state setter, stable for the dialog's life.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [baseUrl]
  )

  useEffect(() => {
    if (!open || hasProviders === false) return
    const t = setTimeout(() => doSearch(query), 400)
    return () => clearTimeout(t)
  }, [open, query, doSearch, hasProviders])

  const clear = () => {
    setResults([])
    setCursor(0)
  }
  return { results, cursor, setCursor, loading, clear }
}

function resultKey(e: KeyboardEvent, results: RemoteSearchResult[], cursor: number, setCursor: Dispatch<SetStateAction<number>>, busy: boolean, onChoose: (result: RemoteSearchResult) => void) {
  if (e.key === "ArrowDown") {
    e.preventDefault()
    setCursor((c) => Math.min(c + 1, results.length - 1))
  } else if (e.key === "ArrowUp") {
    e.preventDefault()
    setCursor((c) => Math.max(c - 1, 0))
  } else if (e.key === "Enter") {
    e.preventDefault()
    if (results[cursor] && !busy) onChoose(results[cursor])
  }
}

// Arrows move through the results, so every letter types into the search,
// and Enter imports the highlighted one unless an import is under way.
export function useResultKeys(open: boolean, results: RemoteSearchResult[], cursor: number, setCursor: Dispatch<SetStateAction<number>>, busy: boolean, onChoose: (result: RemoteSearchResult) => void) {
  useEffect(() => {
    if (!open) return
    const handler = (e: KeyboardEvent) => resultKey(e, results, cursor, setCursor, busy, onChoose)
    window.addEventListener("keydown", handler)
    return () => window.removeEventListener("keydown", handler)
  }, [open, results, cursor, setCursor, busy, onChoose])
}
