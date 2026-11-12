import { createContext, useContext } from "react"

// What this instance calls its Reeve (reeve.name), in each place a sentence
// can put it: label on its own ("Reeve"), start of a sentence ("The
// Reeve"), the middle of one ("the Reeve"). A named Reeve is its name in
// all three. The role keeps its name: settings keys, routes and the manual
// still say Reeve.
export interface ReeveName {
  // The name set, "" for none.
  name: string
  label: string
  start: string
  mid: string
}

export function reeveName(name?: string | null): ReeveName {
  const n = (name ?? "").trim()
  if (!n) return { name: "", label: "Reeve", start: "The Reeve", mid: "the Reeve" }
  return { name: n, label: n, start: n, mid: n }
}

// This server's Reeve's name, as its settings give it (Layout provides it).
export const ReeveNameContext = createContext("")

// useReeveName is this server's Reeve's name; "the Reeve" outside Layout.
export function useReeveName(): ReeveName {
  return reeveName(useContext(ReeveNameContext))
}
