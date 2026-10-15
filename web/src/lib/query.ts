import { QueryClient } from "@tanstack/react-query"

export const queryKeys = {
  settings: (baseUrl: string) => [baseUrl, "settings"] as const,
  navigationSummary: (baseUrl: string) => [baseUrl, "navigation-summary"] as const,
  board: (baseUrl: string, workspace?: string) => [baseUrl, "board", workspace ?? ""] as const,
  task: (baseUrl: string, taskId: string) => [baseUrl, "task", taskId] as const,
  reviewQueue: (baseUrl: string, includeCapturing: boolean) => [baseUrl, "review-queue", includeCapturing] as const,
  handoff: (baseUrl: string, tourId: string) => [baseUrl, "handoff", tourId] as const,
  reviewTour: (baseUrl: string, tourId: string) => [baseUrl, "review-tour", tourId] as const,
  planQueue: (baseUrl: string) => [baseUrl, "plan-queue"] as const,
  plan: (baseUrl: string, planId: string) => [baseUrl, "plan", planId] as const,
  attention: (baseUrl: string) => [baseUrl, "attention"] as const,
  workflowDefinitions: (baseUrl: string) => [baseUrl, "workflow-definitions"] as const,
  workflowRuns: (baseUrl: string) => [baseUrl, "workflow-runs"] as const,
  workflowRun: (baseUrl: string, runId: string) => [baseUrl, "workflow-run", runId] as const,
  agentUIItems: (baseUrl: string, taskId: string) => [baseUrl, "agent-ui-items", taskId] as const,
  // revision 0 is "the latest", which changes when the agent shows it again.
  agentUIItem: (baseUrl: string, itemId: string, revision = 0) => [baseUrl, "agent-ui-item", itemId, revision] as const,
}

export function createQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: {
        staleTime: Infinity,
        retry: false,
        refetchOnWindowFocus: false,
        refetchOnReconnect: false,
      },
    },
  })
}
