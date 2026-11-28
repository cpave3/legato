import { useCallback, useEffect, useMemo, useState } from "react"
import { apiFetch } from "../lib/api"
import {
  catalogError, catalogItems, catalogStatus, listOrEmpty, slashItemsOf, startCatalog,
  type Catalog, type MacroInfo, type ModeInfo, type ModelInfo,
} from "../lib/nativeCatalog"

// A list an agent endpoint answers; nothing when it fails.
function fetchList<T>(baseUrl: string, path: string, set: (list: T[]) => void) {
  apiFetch(baseUrl, path)
    .then((r) => r.ok ? r.json() : [])
    .then((data: unknown) => set(listOrEmpty<T>(data)))
    .catch(() => {})
}

function fetchMacros(baseUrl: string, set: (macros: MacroInfo[]) => void) {
  apiFetch(baseUrl, "/api/macros")
    .then((r) => r.ok ? r.json() : { macros: [] })
    .then((data: { macros?: MacroInfo[] }) => set(listOrEmpty<MacroInfo>(data.macros)))
    .catch(() => set([]))
}

// useAgentCatalog loads what a native session offers its operator: its
// modes, models, commands and skills, and Legato's macros.
export function useAgentCatalog(agentId: string, baseUrl: string) {
  const [modes, setModes] = useState<ModeInfo[]>([])
  const [models, setModels] = useState<ModelInfo[]>([])
  const [macros, setMacros] = useState<MacroInfo[]>([])
  const catalogKey = JSON.stringify([baseUrl, agentId])
  const [catalog, setCatalog] = useState<Catalog>({ key: catalogKey, commands: [], skills: [], loading: true, errors: [] })
  const [catalogAttempt, setCatalogAttempt] = useState(0)

  useEffect(() => {
    fetchList(baseUrl, `/api/agents/${agentId}/chimera/modes`, setModes)
  }, [agentId, baseUrl])

  useEffect(() => {
    fetchList(baseUrl, `/api/agents/${agentId}/chimera/models`, setModels)
  }, [agentId, baseUrl])

  useEffect(() => {
    let current = true
    setCatalog((previous) => startCatalog(previous, catalogKey))
    let pending = 2
    for (const kind of ["commands", "skills"] as const) {
      apiFetch(baseUrl, `/api/agents/${agentId}/chimera/${kind}`)
        .then(async (response) => {
          const items = await catalogItems(response)
          if (current) setCatalog((previous) => ({ ...previous, [kind]: items }))
        })
        .catch((error: unknown) => {
          if (current) setCatalog((previous) => ({ ...previous, errors: [...previous.errors, catalogError(kind, error)] }))
        })
        .finally(() => {
          pending--
          if (current && pending === 0) setCatalog((previous) => ({ ...previous, loading: false }))
        })
    }
    return () => { current = false }
  }, [agentId, baseUrl, catalogKey, catalogAttempt])

  // Fetch macros eagerly so numbered shortcuts are ready when the menu opens.
  useEffect(() => {
    fetchMacros(baseUrl, setMacros)
  }, [baseUrl])

  const slashItems = useMemo(() => slashItemsOf(catalog, catalogKey), [catalog, catalogKey])
  const retryCatalog = useCallback(() => setCatalogAttempt((attempt) => attempt + 1), [])

  return { modes, models, macros, catalogKey, catalog: catalogStatus(catalog, catalogKey), slashItems, retryCatalog }
}
