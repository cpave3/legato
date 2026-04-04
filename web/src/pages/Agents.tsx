import { useState, useEffect, useCallback, useRef, useMemo } from "react"
import { useWebSocket, type AgentInfo, type WSMessage, type PromptState } from "../hooks/useWebSocket"
import { AgentSidebar } from "../components/AgentSidebar"
import { TerminalPanel } from "../components/TerminalPanel"
import { PromptBar, type PromptBarHandle } from "../components/PromptBar"
import { useServer } from "../hooks/useServer"
import { apiFetch } from "../lib/api"

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
  const [agents, setAgents] = useState<AgentInfo[]>([])
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [promptState, setPromptState] = useState<PromptState | null>(null)
  const [glitching, setGlitching] = useState(false)
  const glitchTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const promptBarRef = useRef<PromptBarHandle>(null)

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
    })
  }, [fetchAgents, subscribe, selectedId])

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

  const handleSpawn = useCallback(async () => {
    const title = window.prompt("Agent title:", "Ephemeral session")
    if (title === null) return // cancelled
    try {
      const res = await apiFetch(baseUrl, "/api/agents/spawn", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ title: title || "Ephemeral session" }),
      })
      if (!res.ok) {
        const text = await res.text()
        window.alert("Failed to spawn agent: " + text)
      }
      // agents_changed WebSocket message will trigger a refresh + we can auto-select
    } catch {
      window.alert("Failed to spawn agent")
    }
  }, [baseUrl])

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

  const selectedAgent = agents.find((a) => a.task_id === selectedId)

  // If the selected agent died, deselect it.
  useEffect(() => {
    if (selectedId && selectedAgent && selectedAgent.status !== "running") {
      handleDisconnect()
    }
  }, [selectedId, selectedAgent, handleDisconnect])

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
      </div>
    )
  }

  return (
    <div className="flex h-full flex-col overflow-hidden md:flex-row">
      {isMobile ? (
        <div className="flex shrink-0 gap-2 border-b border-zinc-800 bg-zinc-950 p-2">
          <select
            className="flex-1 rounded bg-zinc-900 px-3 py-2 text-sm text-zinc-200 border border-zinc-700"
            value={selectedId ?? ""}
            onChange={(e) => e.target.value && handleSelect(e.target.value)}
          >
            <option value="">Select agent...</option>
            {runningAgents.map((a) => (
              <option key={a.task_id} value={a.task_id}>
                {a.task_id} — {a.task_title || a.command}
              </option>
            ))}
          </select>
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
              connected={connected}
            />
          </>
        ) : (
          <div className="flex flex-1 items-center justify-center">
            <p className="text-zinc-500">Select an agent to view output</p>
          </div>
        )}
      </div>
    </div>
  )
}
