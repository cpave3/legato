import { useCallback, useEffect, useRef, useState } from "react"
import { useServer } from "./useServer"
import { useWebSocket, type WSMessage } from "./useWebSocket"
import { fetchTaskActivity, toRelatedWork, emptyActivity, type RelatedWorkItem } from "../lib/task-activity"
import { coalesce, isRunChange } from "../lib/querySync"

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
    const requestId = ++requestIdRef.current
    // No task has no related work: drop the last task's, and any answer
    // still on its way for it.
    if (!taskId) {
      setItems([])
      setLoading(false)
      return
    }
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

  useEffect(() => {
    // A run's changes come several at once: one refetch for them.
    const refreshRuns = coalesce(() => void refresh())
    const unsubscribe = subscribe((message: WSMessage) => {
      if (message.type === "plan_changed" && message.task_id === taskId) return void refresh()
      if (message.type === "review_changed" && message.task_id === taskId) return void refresh()
      // workflow_changed carries no task_id; refetch so workflow-spawned agents
      // reflect step transitions. The activity endpoint filters to this task.
      if (isRunChange(message)) refreshRuns()
    })
    return () => {
      refreshRuns.cancel()
      unsubscribe()
    }
  }, [refresh, subscribe, taskId])

  return { items, loading, error, refresh }
}
