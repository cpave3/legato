import { useState, useEffect, useCallback } from "react"
import { useWebSocket } from "./useWebSocket"
import { useServer } from "./useServer"
import { fetchBoard } from "../lib/board"
import type { BoardColumn, Workspace } from "../lib/board-types"

export function useBoard(workspace?: string) {
  const { subscribe } = useWebSocket()
  const { baseUrl } = useServer()
  const [columns, setColumns] = useState<BoardColumn[]>([])
  const [workspaces, setWorkspaces] = useState<Workspace[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    try {
      setError(null)
      const data = await fetchBoard(baseUrl, workspace)
      setColumns(data.columns || [])
      setWorkspaces(data.workspaces || [])
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setLoading(false)
    }
  }, [baseUrl, workspace])

  useEffect(() => {
    refresh()
  }, [refresh])

  useEffect(() => {
    return subscribe((msg) => {
      if (msg.type === "cards_changed") {
        refresh()
      }
    })
  }, [subscribe, refresh])

  return { columns, workspaces, loading, error, refresh }
}
