import { useQueryClient, type QueryKey } from "@tanstack/react-query"
import { useEffect, useRef, type ReactNode } from "react"
import { queryKeys } from "../lib/query"
import { useServer } from "../hooks/useServer"
import { useWebSocket, type AgentInfo, type WSMessage } from "../hooks/useWebSocket"
import type { NavigationSummary } from "../hooks/useNavigationSummary"

const COALESCE_MS = 75

function countActiveAgents(agents: AgentInfo[]) {
  return agents.filter((agent) => Boolean(agent.activity)).length
}

export function QuerySyncProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient()
  const { baseUrl } = useServer()
  const { connectionGeneration, subscribe } = useWebSocket()
  const timers = useRef(new Map<string, ReturnType<typeof setTimeout>>())
  const connectionState = useRef({ baseUrl, generation: 0, awaitingFirstOpen: true })

  useEffect(() => {
    const pending = timers.current
    function invalidate(queryKey: QueryKey, exact = true) {
      const timerKey = JSON.stringify(queryKey)
      const existing = pending.get(timerKey)
      if (existing) clearTimeout(existing)
      pending.set(timerKey, setTimeout(() => {
        pending.delete(timerKey)
        void queryClient.invalidateQueries({ queryKey, exact, refetchType: "active" })
      }, COALESCE_MS))
    }

    function onMessage(message: WSMessage) {
      switch (message.type) {
        case "review_changed":
          invalidate(queryKeys.reviewQueue(baseUrl, false))
          invalidate(queryKeys.reviewQueue(baseUrl, true))
          if (message.tour_id) {
            invalidate(queryKeys.reviewTour(baseUrl, message.tour_id))
            invalidate(queryKeys.handoff(baseUrl, message.tour_id))
          }
          invalidate(queryKeys.navigationSummary(baseUrl))
          break
        case "plan_changed":
          invalidate(queryKeys.planQueue(baseUrl))
          if (message.plan_id) invalidate(queryKeys.plan(baseUrl, message.plan_id))
          invalidate(queryKeys.navigationSummary(baseUrl))
          break
        case "attention_changed":
          invalidate(queryKeys.attention(baseUrl))
          invalidate(queryKeys.navigationSummary(baseUrl))
          break
        case "approvals_changed":
          invalidate(queryKeys.approvals(baseUrl))
          // A decided approval may enroll or remove a device.
          invalidate(queryKeys.devices(baseUrl))
          invalidate(queryKeys.navigationSummary(baseUrl))
          break
        case "workflow_changed":
          invalidate(queryKeys.workflowRuns(baseUrl))
          if (message.workflow_run_id) invalidate(queryKeys.workflowRun(baseUrl, message.workflow_run_id))
          invalidate(queryKeys.navigationSummary(baseUrl))
          break
        case "agent_ui_changed":
          // An agent showed, replaced or lost an artifact or a card.
          if (message.task_id) invalidate(queryKeys.agentUIItems(baseUrl, message.task_id))
          if (message.item_id) invalidate([baseUrl, "agent-ui-item", message.item_id], false)
          break
        case "cards_changed":
          invalidate([baseUrl, "board"], false)
          invalidate([baseUrl, "task"], false)
          break
        case "agents_changed":
          invalidate(queryKeys.navigationSummary(baseUrl))
          invalidate([baseUrl, "board"], false)
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
      pending.forEach(clearTimeout)
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
