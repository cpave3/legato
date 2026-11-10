import { useCallback } from "react"
import { useServer } from "../hooks/useServer"
import { apiFetch } from "../lib/api"
import { SpawnAgentDialog, type SpawnAgentRequest } from "./SpawnAgentDialog"
import { spawnPlace } from "../lib/spawnPlace"
import { engramFields, readSpawnWarning } from "../lib/engrams"
import { useToast } from "../hooks/useToast"

// SpawnEphemeralModal starts an agent that belongs to no card.
export function SpawnEphemeralModal({
  open,
  onClose,
  onSpawned,
}: {
  open: boolean
  onClose: () => void
  onSpawned?: () => void
}) {
  const { baseUrl } = useServer()
  // Absent outside the app's ToastProvider; the warning is then not shown.
  const toast = useToast()

  const spawn = useCallback(async (request: SpawnAgentRequest) => {
    const response = await apiFetch(baseUrl, "/api/agents/spawn", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        title: request.title || "Ephemeral session",
        agent_kind: request.agentKind,
        ...spawnPlace(request),
        ...(request.resources ? { resources: request.resources } : {}),
        ...(request.sandbox === false ? { sandbox: false } : {}),
        ...(request.prompt ? { prompt: request.prompt } : {}),
        ...engramFields(request),
      }),
    })
    if (!response.ok) throw new Error("The agent was not spawned: " + await response.text())
    const warning = await readSpawnWarning(response)
    if (warning) toast?.addToast(warning, "error")
    onSpawned?.()
    onClose()
  }, [baseUrl, onSpawned, onClose, toast])

  return (
    <SpawnAgentDialog
      open={open}
      onClose={onClose}
      name="Spawn agent"
      description="Ephemeral session, not linked to a card"
      action="Spawn"
      acting="Spawning…"
      titled
      onSubmit={spawn}
    />
  )
}
