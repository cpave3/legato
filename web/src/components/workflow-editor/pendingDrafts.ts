import { createContext, useCallback, useRef } from "react"

/** PendingDrafts is told whether each field has text typed but not yet
 * written, so the page counts it as unsaved. */
export const PendingDrafts = createContext<(id: string, pending: boolean) => void>(() => {})

/** usePendingReport is the report PendingDrafts gives its fields: it
 * tells onPending only when whether any field has a draft changes. */
export function usePendingReport(onPending?: (pending: boolean) => void) {
  const pending = useRef(new Set<string>())
  return useCallback((id: string, isPending: boolean) => {
    const had = pending.current.size > 0
    if (isPending) pending.current.add(id)
    else pending.current.delete(id)
    if (had !== pending.current.size > 0) onPending?.(pending.current.size > 0)
  }, [onPending])
}
