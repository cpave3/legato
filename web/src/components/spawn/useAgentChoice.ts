import { useQuery } from "@tanstack/react-query"
import { useEffect, useState } from "react"
import { fetchAdapters } from "../../lib/adapters"
import { agentKindLabel } from "../../lib/agentKind"
import { queryKeys } from "../../lib/query"
import { accountsOffered, agentOptionsFor } from "../../lib/spawnAgent"
import type { SessionPolicy } from "../../lib/spawnRequest"

interface AgentChoiceOptions {
  open: boolean
  // Changes whenever the dialog opens afresh; empty while it is closed.
  openKey: string
  baseUrl: string
  // The agent tools that have a saved session for this task.
  resumableAgentKinds: string[]
}

// useAgentChoice is the agent tool, its Claude Code account, and whether to
// resume the tool's saved session for the task.
export function useAgentChoice({ open, openKey, baseUrl, resumableAgentKinds }: AgentChoiceOptions) {
  // Kept from one opening to the next: the person's last choice of tool.
  const [agentKind, setAgentKind] = useState("")
  const [account, setAccount] = useState("")
  const [sessionPolicy, setSessionPolicy] = useState<SessionPolicy>("resume")
  const adapterQuery = useQuery({ queryKey: queryKeys.adapters(baseUrl), queryFn: () => fetchAdapters(baseUrl), enabled: open, staleTime: 0, retry: false })
  const adapters = adapterQuery.data

  useEffect(() => {
    if (openKey) setSessionPolicy("resume")
  }, [openKey])

  // The tool that runs: the one picked, else the default.
  const effectiveAdapter = agentKind || (adapters?.default ?? "")
  const offersSessionChoice = resumableAgentKinds.includes(effectiveAdapter)
  return {
    agentKind,
    setAgentKind,
    // Another tool forgets the account, which is the tool's.
    pickAgent: (kind: string) => {
      setAgentKind(kind)
      setAccount("")
    },
    account,
    setAccount,
    adapters,
    // False until the adapters are known, since the default one decides
    // whether there is a session to resume.
    ready: adapterQuery.isFetched,
    options: agentOptionsFor(adapters, agentKind),
    effectiveAdapter,
    toolLabel: agentKindLabel(effectiveAdapter),
    accountsShown: accountsOffered(effectiveAdapter, adapters),
    sessionPolicy,
    setSessionPolicy,
    offersSessionChoice,
    // A resumed session keeps its own account.
    resuming: offersSessionChoice && sessionPolicy === "resume",
  }
}

export type AgentChoice = ReturnType<typeof useAgentChoice>
