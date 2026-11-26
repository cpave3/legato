import { useQuery, type UseQueryResult } from "@tanstack/react-query"
import { fetchWorkflowRun, fetchWorkflowRuns, fetchWorkflowRunsOf, fetchWorkflowStats, fetchWorkflows, type WorkflowDiscovery, type WorkflowRun, type WorkflowRunView, type WorkflowStats } from "../lib/workflow"
import { queryKeys } from "../lib/query"
import { useServer } from "./useServer"
import { useNow } from "./useNow"

function queryState<T>(query: UseQueryResult<T, Error>) {
  return {
    data: query.data ?? null,
    loading: query.isPending,
    refreshing: query.isFetching,
    error: query.error?.message ?? null,
    refresh: async () => { await query.refetch() },
  }
}

export function useWorkflows() {
  const { baseUrl } = useServer()
  return queryState(useQuery<WorkflowDiscovery, Error>({
    queryKey: queryKeys.workflowDefinitions(baseUrl),
    queryFn: () => fetchWorkflows(baseUrl),
  }))
}

export function useWorkflowRuns() {
  const { baseUrl } = useServer()
  return queryState(useQuery<WorkflowRun[], Error>({
    queryKey: queryKeys.workflowRuns(baseUrl),
    queryFn: () => fetchWorkflowRuns(baseUrl),
  }))
}

// useWorkflowStats is each workflow's newest run and its last 7 days.
export function useWorkflowStats() {
  const { baseUrl } = useServer()
  const now = new Date(useNow(60_000))
  const day = `${now.getFullYear()}-${now.getMonth() + 1}-${now.getDate()}`
  return queryState(useQuery<WorkflowStats, Error>({
    queryKey: queryKeys.workflowStats(baseUrl, day),
    queryFn: () => fetchWorkflowStats(baseUrl),
  }))
}

export function useWorkflowRun(id: string) {
  const { baseUrl } = useServer()
  return queryState(useQuery<WorkflowRunView, Error>({
    queryKey: queryKeys.workflowRun(baseUrl, id),
    queryFn: () => fetchWorkflowRun(baseUrl, id),
    enabled: Boolean(id),
  }))
}

// useWorkflowRunsOf is one workflow's newest runs, limit of them.
export function useWorkflowRunsOf(name: string, limit: number) {
  const { baseUrl } = useServer()
  return queryState(useQuery<WorkflowRun[], Error>({
    queryKey: queryKeys.workflowRunsOf(baseUrl, name),
    queryFn: () => fetchWorkflowRunsOf(baseUrl, name, limit),
    enabled: Boolean(name),
  }))
}
