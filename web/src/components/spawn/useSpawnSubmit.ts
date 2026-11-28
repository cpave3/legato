import { useEffect, useState } from "react"
import type { SpawnAgentRequest } from "../../lib/spawnRequest"

// useSpawnSubmit starts the agent through onSubmit: loading while it is
// started, and why it failed when onSubmit (or saving the path first)
// rejects. Both start over each time the dialog opens.
export function useSpawnSubmit(openKey: string, onSubmit: (request: SpawnAgentRequest) => Promise<void>) {
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState("")

  useEffect(() => {
    if (!openKey) return
    setError("")
    setLoading(false)
  }, [openKey])

  // savePreset, when given, runs first, and a failure there starts nothing.
  const run = async (savePreset: (() => Promise<unknown>) | null, request: () => SpawnAgentRequest) => {
    setLoading(true)
    setError("")
    try {
      if (savePreset) await savePreset()
      await onSubmit(request())
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    } finally {
      setLoading(false)
    }
  }

  return { loading, error, run }
}
