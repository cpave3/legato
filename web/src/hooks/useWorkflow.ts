import { useQuery, type UseQueryResult } from "@tanstack/react-query"
import { fetchWorkflowRun, fetchWorkflowRuns, fetchWorkflowRunsOf, fetchWorkflows, type WorkflowDiscovery, type WorkflowRun, type WorkflowRunView } from "../lib/workflow"
import { queryKeys } from "../lib/query"
import { useServer } from "./useServer"

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
