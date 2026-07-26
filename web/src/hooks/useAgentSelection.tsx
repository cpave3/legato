import { createContext, useContext, useMemo, useState, type ReactNode } from "react"

interface AgentSelection {
  selectedAgentId: string | null
  setSelectedAgentId: (agentId: string | null) => void
}

const AgentSelectionContext = createContext<AgentSelection | null>(null)

export function AgentSelectionProvider({ children }: { children: ReactNode }) {
  const [selectedAgentId, setSelectedAgentId] = useState<string | null>(null)
  const value = useMemo(() => ({ selectedAgentId, setSelectedAgentId }), [selectedAgentId])
  return <AgentSelectionContext.Provider value={value}>{children}</AgentSelectionContext.Provider>
}

export function useAgentSelection(): AgentSelection {
  const selection = useContext(AgentSelectionContext)
  if (!selection) {
    throw new Error("useAgentSelection must be used within AgentSelectionProvider")
  }
  return selection
}
