import { useQueryClient, type QueryKey } from "@tanstack/react-query"
import { useEffect, useRef, type ReactNode } from "react"
import { queryKeys } from "../lib/query"
import { coalesce, invalidationsFor, type Coalesced } from "../lib/querySync"
import { useServer } from "../hooks/useServer"
import { useWebSocket, type AgentInfo, type WSMessage } from "../hooks/useWebSocket"
import type { NavigationSummary } from "../hooks/useNavigationSummary"
import type { RepositoryCacheStatus } from "../lib/preparingAgents"

function countActiveAgents(agents: AgentInfo[]) {
  return agents.filter((agent) => Boolean(agent.activity)).length
}

export function QuerySyncProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient()
  const { baseUrl } = useServer()
  const { connectionGeneration, subscribe } = useWebSocket()
  const timers = useRef(new Map<string, Coalesced>())
  const connectionState = useRef({ baseUrl, generation: 0, awaitingFirstOpen: true })

  useEffect(() => {
    const pending = timers.current
    // A burst of messages is one refetch of each query (coalesce).
    function invalidate(queryKey: QueryKey, exact = true) {
      const timerKey = JSON.stringify(queryKey)
      let refetch = pending.get(timerKey)
      if (!refetch) {
        refetch = coalesce(() => {
          pending.delete(timerKey)
          void queryClient.invalidateQueries({ queryKey, exact, refetchType: "active" })
        })
        pending.set(timerKey, refetch)
      }
      refetch()
    }

    function onMessage(message: WSMessage) {
      for (const { queryKey, exact } of invalidationsFor(message, baseUrl)) invalidate(queryKey, exact)
      switch (message.type) {
        case "repository_cache":
          // git's progress comes several times a second: the message is
          // the new state, so nothing is asked for again.
          if (message.cache) {
            const status = message.cache
            queryClient.setQueryData<Record<string, RepositoryCacheStatus>>(queryKeys.repositoryCache(baseUrl), (current) => ({
              ...current,
              [status.repo]: status,
            }))
          }
          break
        case "agent_list":
          if (message.agents) {
            queryClient.setQueryData<NavigationSummary>(queryKeys.navigationSummary(baseUrl), (current) => current ? {
              ...current,
              agents: countActiveAgents(message.agents ?? []),
            } : current)
          }
          break
      }
    }

    const unsubscribe = subscribe(onMessage)
    return () => {
      unsubscribe()
      pending.forEach((refetch) => refetch.cancel())
      pending.clear()
    }
  }, [baseUrl, queryClient, subscribe])

  useEffect(() => {
    const state = connectionState.current
    if (state.baseUrl !== baseUrl) {
      connectionState.current = { baseUrl, generation: connectionGeneration, awaitingFirstOpen: true }
      void queryClient.refetchQueries({
        type: "active",
        predicate: (query) => query.queryKey[0] === baseUrl,
      })
      return
    }
    if (connectionGeneration === state.generation) return
    state.generation = connectionGeneration
    if (state.awaitingFirstOpen) {
      state.awaitingFirstOpen = false
      return
    }
    void queryClient.refetchQueries({
      type: "active",
      predicate: (query) => query.queryKey[0] === baseUrl,
    })
  }, [baseUrl, connectionGeneration, queryClient])

  return children
}
