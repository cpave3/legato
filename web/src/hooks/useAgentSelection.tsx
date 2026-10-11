/* eslint-disable react-refresh/only-export-components -- the provider and its hooks share this file */
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react"
import { onShellReturn, tellShellWhereToReturn } from "../lib/mobileShell"

interface AgentSelection {
  selectedAgentId: string | null
  setSelectedAgentId: (agentId: string | null) => void
}

// picks counts every selection, so picking the agent that is already open can
// be told apart from nothing having happened.
const AgentSelectionContext = createContext<(AgentSelection & { picks: number }) | null>(null)

export function AgentSelectionProvider({ children }: { children: ReactNode }) {
  const [selection, setSelection] = useState<{ agentId: string | null; picks: number }>({ agentId: null, picks: 0 })
  const selectedAgentId = selection.agentId
  const setSelectedAgentId = useCallback((agentId: string | null) => {
    setSelection((current) => ({ agentId, picks: current.picks + 1 }))
  }, [])
  const value = useMemo(
    () => ({ selectedAgentId, setSelectedAgentId, picks: selection.picks }),
    [selectedAgentId, setSelectedAgentId, selection.picks],
  )

  // The open agent is not part of the address, so a reopened page would come
  // back with none: ?task= is the link that opens one.
  useEffect(() => {
    tellShellWhereToReturn(selectedAgentId ? `/agents?task=${encodeURIComponent(selectedAgentId)}` : "/agents")
  }, [selectedAgentId])

  // While the mobile app was in the background the open agent may have moved
  // on unseen, so coming back counts as picking it again.
  useEffect(() => onShellReturn(() => setSelection((current) => ({ ...current, picks: current.picks + 1 }))), [])

  return <AgentSelectionContext.Provider value={value}>{children}</AgentSelectionContext.Provider>
}

export function useAgentSelection(): AgentSelection {
  const selection = useContext(AgentSelectionContext)
  if (!selection) {
    throw new Error("useAgentSelection must be used within AgentSelectionProvider")
  }
  return selection
}

// Runs onReselected when agentId is picked while it is already open: picking an
// agent is also how a person asks for a fresh view of it. Does nothing outside
// an AgentSelectionProvider.
export function useAgentReselected(agentId: string, onReselected: () => void): void {
  const selection = useContext(AgentSelectionContext)
  const picks = selection?.picks
  const picked = selection?.selectedAgentId === agentId
  const seenPicks = useRef(picks)
  useEffect(() => {
    if (picks === seenPicks.current) return
    seenPicks.current = picks
    if (picked) onReselected()
  }, [picks, picked, onReselected])
}
