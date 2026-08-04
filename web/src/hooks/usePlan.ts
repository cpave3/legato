import { useQuery, type UseQueryResult } from "@tanstack/react-query"
import { fetchPlan, fetchPlanQueue, type PlanQueueItem, type PlanView } from "../lib/plan"
import { queryKeys } from "../lib/query"
import { useServer } from "./useServer"

function queryState<T>(query: UseQueryResult<T, Error>) {
  return {
    data: query.data ?? null,
    loading: query.isPending,
    error: query.error?.message ?? null,
    refresh: async () => { await query.refetch() },
  }
}

export function usePlanQueue() {
  const { baseUrl } = useServer()
  return queryState(useQuery<PlanQueueItem[], Error>({
    queryKey: queryKeys.planQueue(baseUrl),
    queryFn: () => fetchPlanQueue(baseUrl),
  }))
}

export function usePlan(planId: string) {
  const { baseUrl } = useServer()
  return queryState(useQuery<PlanView, Error>({
    queryKey: queryKeys.plan(baseUrl, planId),
    queryFn: () => fetchPlan(baseUrl, planId),
    enabled: Boolean(planId),
  }))
}
