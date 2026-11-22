import { createContext } from "react"

/** PendingDrafts is told whether each field has text typed but not yet
 * written, so the page counts it as unsaved. */
export const PendingDrafts = createContext<(id: string, pending: boolean) => void>(() => {})
