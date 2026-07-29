import { useCallback, useEffect, useRef, useState } from "react"
import { useServer } from "./useServer"
import { useWebSocket, type WSMessage } from "./useWebSocket"
import { fetchTaskActivity, toRelatedWork, emptyActivity, type RelatedWorkItem } from "../lib/task-activity"

interface AsyncState {
  items: RelatedWorkItem[]
  loading: boolean
  error: string | null
  refresh: () => Promise<void>
}

/**
 * Fetches related plans/reviews/workflows for a task and keeps them fresh via
 * plan_changed / review_changed / workflow_changed WebSocket events targeting
 * the same task. Workflow events don't carry a task_id, so a workflow_changed
 * for any run triggers a refetch (the activity endpoint filters by task).
 */
export function useTaskActivity(taskId: string): AsyncState {
  const { baseUrl } = useServer()
  const { subscribe } = useWebSocket()
  const [items, setItems] = useState<RelatedWorkItem[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const requestIdRef = useRef(0)

  const refresh = useCallback(async () => {
    if (!taskId) return
    const requestId = ++requestIdRef.current
    try {
      setError(null)
      const activity = await fetchTaskActivity(baseUrl, taskId)
      if (requestId === requestIdRef.current) setItems(toRelatedWork(activity))
    } catch (cause) {
      if (requestId === requestIdRef.current) {
        setError(cause instanceof Error ? cause.message : String(cause))
        setItems(toRelatedWork(emptyActivity()))
      }
    } finally {
      if (requestId === requestIdRef.current) setLoading(false)
    }
  }, [baseUrl, taskId])

  useEffect(() => { void refresh() }, [refresh])

  useEffect(() => subscribe((message: WSMessage) => {
    if (message.type === "plan_changed" && message.task_id === taskId) return void refresh()
    if (message.type === "review_changed" && message.task_id === taskId) return void refresh()
    // workflow_changed carries no task_id; refetch so workflow-spawned agents
    // reflect step transitions. The activity endpoint filters to this task.
    if (message.type === "workflow_changed") return void refresh()
  }), [refresh, subscribe, taskId])

  return { items, loading, error, refresh }
}
