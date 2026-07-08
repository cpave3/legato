import { useState, useEffect, useCallback, useRef, useMemo } from "react"
import { useWebSocket, type AgentInfo, type WSMessage, type PromptState } from "../hooks/useWebSocket"
import { AgentSidebar } from "../components/AgentSidebar"
import { TerminalPanel } from "../components/TerminalPanel"
import { PromptBar, type PromptBarHandle } from "../components/PromptBar"
import { StartSwarmModal } from "../components/StartSwarmModal"
import { SpawnEphemeralModal } from "../components/SpawnEphemeralModal"
import { SwarmEventLog } from "../components/SwarmEventLog"
import { AgentSelect } from "../components/AgentSelect"
import { useServer } from "../hooks/useServer"
import { useSwarmEvents } from "../hooks/useSwarmEvents"
import { apiFetch, toggleAgentNotify } from "../lib/api"

const GLITCH_DURATION_MS = 500

function getPromptDetectionDefault(): boolean {
  return localStorage.getItem("legato:prompt-detection") !== "false"
}

function getSwitchModifier(): string {
  const stored = localStorage.getItem("legato:switch-modifier")
  if (stored) return stored
  return /Mac|iPhone|iPad/.test(navigator.platform) ? "Alt" : "Control"
}

function isModifierActive(e: globalThis.KeyboardEvent, mod: string): boolean {
  switch (mod) {
    case "Control": return e.ctrlKey
    case "Alt": return e.altKey
    case "Meta": return e.metaKey
    default: return false
  }
}

function isModifierKey(e: globalThis.KeyboardEvent, mod: string): boolean {
  switch (mod) {
    case "Control": return e.key === "Control"
    case "Alt": return e.key === "Alt"
    case "Meta": return e.key === "Meta"
    default: return false
  }
}

export function AgentsPage() {
  const { send, subscribe, connected } = useWebSocket()
  const { baseUrl } = useServer()
  const { fetchState } = useSwarmEvents()
  const [agents, setAgents] = useState<AgentInfo[]>([])
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [promptState, setPromptState] = useState<PromptState | null>(null)
  const [glitching, setGlitching] = useState(false)
  const glitchTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const promptBarRef = useRef<PromptBarHandle>(null)
  const [showStartSwarm, setShowStartSwarm] = useState(false)
  const [startSwarmPreselect, setStartSwarmPreselect] = useState<string | undefined>(undefined)
  const [showSpawn, setShowSpawn] = useState(false)
  const [ntfyConfigured, setNtfyConfigured] = useState(false)
  const [voiceEnabled, setVoiceEnabled] = useState(false)

  // Per-agent prompt detection override. If not in the map, uses the global default.
  const [promptDetectionOverrides, setPromptDetectionOverrides] = useState<Record<string, boolean>>({})

  // Track dismissed prompt type to suppress re-detection of the same prompt.
  const dismissedPromptTypeRef = useRef<string | null>(null)

  const isPromptDetectionEnabled = selectedId
    ? (promptDetectionOverrides[selectedId] ?? getPromptDetectionDefault())
    : getPromptDetectionDefault()

  const triggerGlitch = useCallback(() => {
    if (localStorage.getItem("legato:glitch-effect") === "false") return
    if (glitchTimer.current) clearTimeout(glitchTimer.current)
    setGlitching(true)
    glitchTimer.current = setTimeout(() => setGlitching(false), GLITCH_DURATION_MS)
  }, [])

  // Fetch agents on mount and on agents_changed
  const fetchAgents = useCallback(async () => {
    try {
      const res = await apiFetch(baseUrl, "/api/agents")
      if (res.status === 401) {
        window.location.reload()
        return
      }
      const data: AgentInfo[] = await res.json()
      setAgents(data)
    } catch {
      // ignore fetch errors
    }
  }, [baseUrl])

  useEffect(() => {
    fetchAgents()
    apiFetch(baseUrl, "/api/settings")
      .then((r) => r.json())
      .then((data) => {
        setNtfyConfigured(!!data.ntfy_configured)
        setVoiceEnabled(!!data.voice_enabled)
      })
      .catch(() => {
        setNtfyConfigured(false)
        setVoiceEnabled(false)
      })
    return subscribe((msg: WSMessage) => {
      if (msg.type === "agent_list" && msg.agents) {
        setAgents(msg.agents)
      }
      if (msg.type === "agents_changed") {
        fetchAgents()
      }
      if (msg.type === "prompt_state" && msg.agent_id === selectedId && msg.prompt) {
        // Suppress if same prompt type was dismissed and detection isn't manually triggered.
        if (dismissedPromptTypeRef.current === msg.prompt.type) return
        setPromptState(msg.prompt)
      }
      if (msg.type === "swarm_changed" && msg.parent_task_id) {
        // Refresh agents list in case new workers appeared.
        fetchAgents()
      }
    })
  }, [fetchAgents, subscribe, selectedId, baseUrl])

  const handleSelect = useCallback(
    (taskId: string) => {
      if (selectedId) {
        send({ type: "unsubscribe_agent", agent_id: selectedId })
      }
      setSelectedId(taskId)
      setPromptState(null)
      dismissedPromptTypeRef.current = null
      send({ type: "subscribe_agent", agent_id: taskId })
    },
    [selectedId, send]
  )

  const handleSendKeys = useCallback(
    (keys: string) => {
      if (selectedId) {
        send({ type: "send_keys", agent_id: selectedId, keys })
      }
    },
    [selectedId, send]
  )

  const handleDismissPrompt = useCallback(() => {
    if (promptState) {
      dismissedPromptTypeRef.current = promptState.type
    }
    setPromptState(null)
  }, [promptState])

  const handleSubmitText = useCallback(
    (keys: string) => {
      if (selectedId) {
        send({ type: "send_keys", agent_id: selectedId, keys })
        // User submitted input — clear dismissed state so future prompts show.
        dismissedPromptTypeRef.current = null
      }
    },
    [selectedId, send]
  )

  const handleDetectPrompt = useCallback(() => {
    if (selectedId) {
      // Manual detect — clear dismissed state so the result shows.
      dismissedPromptTypeRef.current = null
      send({ type: "detect_prompt", agent_id: selectedId })
    }
  }, [selectedId, send])

  const handleRefresh = useCallback(() => {
    if (selectedId) {
      send({ type: "refresh_pane", agent_id: selectedId })
    }
  }, [selectedId, send])

  const handleDisconnect = useCallback(() => {
    if (selectedId) {
      send({ type: "unsubscribe_agent", agent_id: selectedId })
    }
    setSelectedId(null)
    setPromptState(null)
    dismissedPromptTypeRef.current = null
  }, [selectedId, send])

  const handleKill = useCallback(async () => {
    if (!selectedId) return
    if (!window.confirm("Kill this agent session?")) return
    localStorage.removeItem(`legato:draft:${selectedId}`)
    try {
      await apiFetch(baseUrl, "/api/agents/kill", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ task_id: selectedId }),
      })
    } catch {
      // agents_changed will handle cleanup
    }
  }, [selectedId, baseUrl])

  const handleSpawn = useCallback(() => {
    setShowSpawn(true)
  }, [])

  const handleTogglePromptDetection = useCallback(() => {
    if (!selectedId) return
    setPromptDetectionOverrides((prev) => ({
      ...prev,
      [selectedId]: !(prev[selectedId] ?? getPromptDetectionDefault()),
    }))
    // If disabling, also dismiss any active prompt.
    const currentlyEnabled = selectedId
      ? (promptDetectionOverrides[selectedId] ?? getPromptDetectionDefault())
      : true
    if (currentlyEnabled) {
      setPromptState(null)
    }
  }, [selectedId, promptDetectionOverrides])

  // Re-subscribe to the selected agent after a WebSocket reconnect.
  // The server-side subscription is lost when the old connection drops.
  useEffect(() => {
    if (connected && selectedId) {
      send({ type: "subscribe_agent", agent_id: selectedId })
    }
  }, [connected, selectedId, send])

  const runningAgents = useMemo(() => agents.filter((a) => a.status === "running"), [agents])

  const selectedAgent = agents.find((a) => a.task_id === selectedId)
  const selectedParentId = selectedAgent?.parent_task_id ?? null

  const handleToggleNotify = useCallback(async () => {
    if (!selectedId) return
    const next = !selectedAgent?.notify_enabled
    try {
      await toggleAgentNotify(baseUrl, selectedId, next)
      fetchAgents()
    } catch {
      // ignore
    }
  }, [selectedId, selectedAgent, baseUrl, fetchAgents])

  // Keyboard agent switching: modifier + digit
  const [modifierHeld, setModifierHeld] = useState(false)
  useEffect(() => {
    const onKeyDown = (e: globalThis.KeyboardEvent) => {
      const mod = getSwitchModifier()
      if (isModifierActive(e, mod)) {
        setModifierHeld(true)
        const digit = parseInt(e.key, 10)
        if (digit >= 1 && digit <= 9) {
          e.preventDefault()
          const agent = runningAgents[digit - 1]
          if (agent && agent.task_id !== selectedId) {
            handleSelect(agent.task_id)
            triggerGlitch()
          }
        }
      }
    }
    const onKeyUp = (e: globalThis.KeyboardEvent) => {
      if (isModifierKey(e, getSwitchModifier())) {
        setModifierHeld(false)
      }
    }
    const onBlur = () => setModifierHeld(false)

    window.addEventListener("keydown", onKeyDown)
    window.addEventListener("keyup", onKeyUp)
    window.addEventListener("blur", onBlur)
    return () => {
      window.removeEventListener("keydown", onKeyDown)
      window.removeEventListener("keyup", onKeyUp)
      window.removeEventListener("blur", onBlur)
    }
  }, [runningAgents, selectedId, handleSelect, triggerGlitch])

  // Mobile: use select dropdown on narrow screens
  const [isMobile, setIsMobile] = useState(false)
  useEffect(() => {
    const check = () => setIsMobile(window.innerWidth <= 768)
    check()
    window.addEventListener("resize", check)
    return () => window.removeEventListener("resize", check)
  }, [])

  // If the selected agent died, deselect it.
  useEffect(() => {
    if (selectedId && selectedAgent && selectedAgent.status !== "running") {
      handleDisconnect()
    }
  }, [selectedId, selectedAgent, handleDisconnect])

  const handleOpenStartSwarm = useCallback(() => {
    setStartSwarmPreselect(selectedParentId ?? undefined)
    setShowStartSwarm(true)
  }, [selectedParentId])

  if (runningAgents.length === 0) {
    return (
      <div className="flex flex-1 flex-col items-center justify-center gap-4">
        <p className="text-zinc-500">No active agents</p>
        <button
          onClick={handleSpawn}
          className="rounded bg-indigo-600 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-indigo-500"
        >
          Spawn Agent
        </button>
        <button
          onClick={handleOpenStartSwarm}
          className="rounded border border-indigo-900 bg-indigo-950 px-4 py-2 text-sm font-medium text-indigo-300 transition-colors hover:bg-indigo-900"
        >
          Start Swarm
        </button>
        <SpawnEphemeralModal
          open={showSpawn}
          onClose={() => setShowSpawn(false)}
          onSpawned={fetchAgents}
        />
        <StartSwarmModal
          open={showStartSwarm}
          onClose={() => setShowStartSwarm(false)}
          preSelectedParentId={startSwarmPreselect}
        />
      </div>
    )
  }

  return (
    <div className="flex h-full flex-col overflow-hidden md:flex-row">
      {isMobile ? (
        <div className="flex shrink-0 gap-2 border-b border-zinc-800 bg-zinc-950 p-2">
          <AgentSelect
            agents={runningAgents}
            selectedId={selectedId}
            onSelect={handleSelect}
          />
          <button
            onClick={handleSpawn}
            className="rounded bg-indigo-600 px-3 py-2 text-sm font-medium text-white transition-colors hover:bg-indigo-500"
          >
            +
          </button>
        </div>
      ) : (
        <AgentSidebar
          agents={runningAgents}
          selectedId={selectedId}
          onSelect={handleSelect}
          onSpawn={handleSpawn}
          onStartSwarm={handleOpenStartSwarm}
          modifierHeld={modifierHeld}
        />
      )}

      <div className="flex min-h-0 flex-1 flex-col">
        {selectedId ? (
          <>
            <div className="relative min-h-0 flex-1">
              <TerminalPanel agentId={selectedId} onGlitch={triggerGlitch} onClickTerminal={() => promptBarRef.current?.focus()} />
              {glitching && (
                <div className="terminal-glitch-overlay" aria-hidden="true" />
              )}
            </div>
            <PromptBar
              ref={promptBarRef}
              promptState={isPromptDetectionEnabled ? promptState : null}
              onSendKeys={handleSendKeys}
              onSubmitText={handleSubmitText}
              onDismissPrompt={handleDismissPrompt}
              onDetectPrompt={handleDetectPrompt}
              onDisconnect={handleDisconnect}
              onKill={handleKill}
              onRefresh={handleRefresh}
              onTogglePromptDetection={handleTogglePromptDetection}
              promptDetectionEnabled={isPromptDetectionEnabled}
              agentId={selectedId}
              agentTitle={selectedAgent?.task_title}
              agentActivity={selectedAgent?.activity}
              agentCommand={selectedAgent?.command}
              agentKind={selectedAgent?.agent_kind}
              connected={connected}
              ntfyConfigured={ntfyConfigured}
              notifyEnabled={selectedAgent?.notify_enabled}
              onToggleNotify={handleToggleNotify}
              voiceEnabled={voiceEnabled}
            />
          </>
        ) : (
          <div className="flex flex-1 items-center justify-center">
            <p className="text-zinc-500">Select an agent to view output</p>
          </div>
        )}
      </div>

      {/* Right sidebar: swarm event log for swarm participants */}
      {selectedParentId && !isMobile && (
        <div className="hidden md:flex w-64 flex-col border-l border-zinc-800">
          <SwarmEventLog parentId={selectedParentId} />
        </div>
      )}

      <SpawnEphemeralModal
        open={showSpawn}
        onClose={() => setShowSpawn(false)}
        onSpawned={fetchAgents}
      />
      <StartSwarmModal
        open={showStartSwarm}
        onClose={() => setShowStartSwarm(false)}
        preSelectedParentId={startSwarmPreselect}
        onStarted={() => {
          if (startSwarmPreselect) {
            fetchState(startSwarmPreselect)
          }
        }}
      />
    </div>
  )
}
