import { useCallback, useMemo } from "react"
import { useSearchParams } from "react-router-dom"
import { parseStageFilter, type WorkStage } from "../lib/agentStage"

// Where the viewer's stage filter is remembered in this browser.
export const STAGE_FILTER_KEY = "legato.agents.stage"

function remembered(): WorkStage | null {
  try {
    return parseStageFilter(localStorage.getItem(STAGE_FILTER_KEY))
  } catch {
    return null
  }
}

// useStageFilter is the agent list's stage filter: the address's ?stage=
// when it names one (so a link opens the filter), else the one this viewer
// last chose. Choosing one keeps it in both; null is all agents.
export function useStageFilter(): { stage: WorkStage | null; setStage: (stage: WorkStage | null) => void } {
  const [params, setParams] = useSearchParams()
  const inAddress = params.get("stage")
  const stage = useMemo(() => (inAddress !== null ? parseStageFilter(inAddress) : remembered()), [inAddress])
  const setStage = useCallback((next: WorkStage | null) => {
    try {
      if (next) localStorage.setItem(STAGE_FILTER_KEY, next)
      else localStorage.removeItem(STAGE_FILTER_KEY)
    } catch { /* not remembered */ }
    setParams((current) => {
      const updated = new URLSearchParams(current)
      if (next) updated.set("stage", next)
      else updated.delete("stage")
      return updated
    }, { replace: true })
  }, [setParams])
  return { stage, setStage }
}
