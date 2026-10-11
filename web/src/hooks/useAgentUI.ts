import { useQuery } from "@tanstack/react-query"
import { fetchAgentUIItem, fetchAgentUIItems, type AgentUIItem } from "../lib/agentUi"
import { queryKeys } from "../lib/query"
import { useServer } from "./useServer"

// What one agent session has shown, at each item's latest revision and
// without content. Kept current by `agent_ui_changed`.
export function useAgentUIItems(taskId: string | null | undefined) {
  const { baseUrl } = useServer()
  const query = useQuery<AgentUIItem[], Error>({
    queryKey: queryKeys.agentUIItems(baseUrl, taskId ?? ""),
    queryFn: () => fetchAgentUIItems(baseUrl, taskId ?? ""),
    enabled: Boolean(taskId),
  })
  return { items: query.data ?? [], loading: query.isPending && Boolean(taskId), error: query.error?.message ?? null }
}

// One item with its content. revision 0 reads the latest.
export function useAgentUIItem(itemId: string | null | undefined, revision = 0) {
  const { baseUrl } = useServer()
  const query = useQuery<AgentUIItem, Error>({
    queryKey: queryKeys.agentUIItem(baseUrl, itemId ?? "", revision),
    queryFn: () => fetchAgentUIItem(baseUrl, itemId ?? "", revision),
    enabled: Boolean(itemId),
  })
  return { item: query.data ?? null, loading: query.isPending && Boolean(itemId), error: query.error ?? null }
}
