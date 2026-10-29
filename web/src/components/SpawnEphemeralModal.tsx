import { useCallback } from "react"
import { useServer } from "../hooks/useServer"
import { apiFetch } from "../lib/api"
import { SpawnAgentDialog, type SpawnAgentRequest } from "./SpawnAgentDialog"

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

  const spawn = useCallback(async (request: SpawnAgentRequest) => {
    const response = await apiFetch(baseUrl, "/api/agents/spawn", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        title: request.title || "Ephemeral session",
        agent_kind: request.agentKind,
        working_dir: request.workingDir,
        ...(request.resources ? { resources: request.resources } : {}),
        ...(request.sandbox === false ? { sandbox: false } : {}),
        ...(request.prompt ? { prompt: request.prompt } : {}),
      }),
    })
    if (!response.ok) throw new Error("The agent was not spawned: " + await response.text())
    onSpawned?.()
    onClose()
  }, [baseUrl, onSpawned, onClose])

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
