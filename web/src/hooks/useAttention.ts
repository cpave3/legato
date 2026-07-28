import { useCallback, useEffect, useState } from "react"
import { useServer } from "./useServer"
import { useWebSocket } from "./useWebSocket"
import { fetchAttention, type AttentionItem } from "../lib/attention"

export function useAttention() {
  const { baseUrl } = useServer()
  const { subscribe } = useWebSocket()
  const [data, setData] = useState<AttentionItem[] | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    try {
      setError(null)
      setData(await fetchAttention(baseUrl))
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause))
    } finally {
      setLoading(false)
    }
  }, [baseUrl])

  useEffect(() => { void refresh() }, [refresh])
  useEffect(() => subscribe((message) => {
    if (message.type === "attention_changed") void refresh()
  }), [refresh, subscribe])

  return { data, loading, error, refresh }
}
