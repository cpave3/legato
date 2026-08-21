import { useCallback, useEffect, useRef, useState } from "react"
import { useServer } from "./useServer"
import { apiFetch } from "../lib/api"

export interface AgentPreviewData {
  task_id: string
  content: string
  captured_at: string
}

export function useAgentPreview(agentId: string | null, enabled: boolean) {
  const { baseUrl } = useServer()
  const [preview, setPreview] = useState<AgentPreviewData | null>(null)
  const [visible, setVisible] = useState(true)
  const elementRef = useRef<HTMLElement | null>(null)

  const refresh = useCallback(async () => {
    if (!agentId || !enabled || document.hidden || !visible) return
    try {
      const response = await apiFetch(baseUrl, `/api/agents/${encodeURIComponent(agentId)}/preview`)
      if (!response.ok) return
      setPreview(await response.json() as AgentPreviewData)
    } catch {
      // Keep the last successful preview while the agent or server reconnects.
    }
  }, [agentId, baseUrl, enabled, visible])

  useEffect(() => {
    setPreview(null)
    if (!enabled || !agentId) return
    void refresh()
    const timer = window.setInterval(() => void refresh(), 5000)
    const onVisibility = () => { if (!document.hidden) void refresh() }
    document.addEventListener("visibilitychange", onVisibility)
    return () => {
      window.clearInterval(timer)
      document.removeEventListener("visibilitychange", onVisibility)
    }
  }, [agentId, enabled, refresh])

  const observe = useCallback((element: HTMLElement | null) => {
    elementRef.current = element
  }, [])

  useEffect(() => {
    const element = elementRef.current
    if (!element || typeof IntersectionObserver === "undefined") return
    const observer = new IntersectionObserver(([entry]) => setVisible(entry.isIntersecting))
    observer.observe(element)
    return () => observer.disconnect()
  }, [agentId])

  return { preview, observe, refresh }
}
