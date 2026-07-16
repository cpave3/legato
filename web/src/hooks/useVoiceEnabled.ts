import { useEffect, useState } from "react"
import { apiFetch } from "../lib/api"

export function useVoiceEnabled(baseUrl: string): boolean {
  const [enabled, setEnabled] = useState(false)

  useEffect(() => {
    let current = true
    apiFetch(baseUrl, "/api/settings")
      .then((response) => response.json())
      .then((settings) => { if (current) setEnabled(Boolean(settings.voice_enabled)) })
      .catch(() => { if (current) setEnabled(false) })
    return () => { current = false }
  }, [baseUrl])

  return enabled
}
