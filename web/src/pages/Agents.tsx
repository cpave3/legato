import { useState, useEffect, useCallback, useRef, useMemo } from "react"
import { LayoutGrid } from "lucide-react"
import { Link, useSearchParams } from "react-router-dom"
import { useWebSocket, type AgentInfo, type WSMessage, type PromptState } from "../hooks/useWebSocket"
import { AgentSidebar } from "../components/AgentSidebar"
import { AgentContextMenu } from "../components/AgentContextMenu"
import { TerminalPanel } from "../components/TerminalPanel"
import { NativeAgentPanel } from "../components/NativeAgentPanel"
import { RelatedWorkStrip } from "../components/RelatedWorkStrip"
import { PromptBar, type PromptBarHandle } from "../components/PromptBar"
import { StartSwarmModal } from "../components/StartSwarmModal"
import { SpawnEphemeralModal } from "../components/SpawnEphemeralModal"
import { SwarmEventLog } from "../components/SwarmEventLog"
import { SwarmCommandCenter } from "../components/SwarmCommandCenter"
import { AgentEnsemble } from "../components/AgentEnsemble"
import { AgentSelect } from "../components/AgentSelect"
import { AgentArtifactPanel } from "../components/AgentArtifactPanel"
import { useAgentUIItems } from "../hooks/useAgentUI"
import { useArtifactPanel } from "../hooks/useArtifactPanel"
import { useServer } from "../hooks/useServer"
import { useSwarmEvents } from "../hooks/useSwarmEvents"
import { useTaskActivity } from "../hooks/useTaskActivity"
import { apiFetch, toggleAgentNotify } from "../lib/api"
import { canBeNative } from "../lib/agentKind"
import { useAgentSelection } from "../hooks/useAgentSelection"
import { isEditableTarget } from "../hooks/useMacroHotkeys"
import { useSettings } from "../hooks/useSettings"
import { useToast } from "../hooks/useToast"

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
  const { send, subscribe, connected, connectionGeneration } = useWebSocket()
  const { baseUrl } = useServer()
  const { states: swarmStates, fetchState } = useSwarmEvents()
  const [agents, setAgents] = useState<AgentInfo[]>([])
  const { selectedAgentId: selectedId, setSelectedAgentId: setSelectedId } = useAgentSelection()
  const [searchParams, setSearchParams] = useSearchParams()
  const linkedTask = searchParams.get("task")
  const workflowRun = searchParams.get("workflow")
  // The ensemble shows every agent at once, in place of the list and the one
  // agent in view. It is part of the address, so Back returns to it.
  const ensemble = searchParams.get("view") === "ensemble"
  const showEnsemble = useCallback((show: boolean) => {
    const next = new URLSearchParams(searchParams)
    if (show) next.set("view", "ensemble")
    else next.delete("view")
    setSearchParams(next)
  }, [searchParams, setSearchParams])
  const [promptState, setPromptState] = useState<PromptState | null>(null)
  const [glitching, setGlitching] = useState(false)
  const glitchTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const promptBarRef = useRef<PromptBarHandle>(null)
  const [showStartSwarm, setShowStartSwarm] = useState(false)
  const [startSwarmPreselect, setStartSwarmPreselect] = useState<string | undefined>(undefined)
  const [showSpawn, setShowSpawn] = useState(false)
  const [singleAgentOverride, setSingleAgentOverride] = useState(false)
  const [activeSwarmParentId, setActiveSwarmParentId] = useState<string | null>(null)
  const subscribedAgentIds = useRef<Set<string>>(new Set())
  const [agentContextMenu, setAgentContextMenu] = useState<{
    agent: AgentInfo
    position: { x: number; y: number }
  } | null>(null)
  const { addToast } = useToast()
  const settings = useSettings()
  const ntfyConfigured = Boolean(settings.data?.ntfy_configured)
  const voiceEnabled = Boolean(settings.data?.voice_enabled)
  const voiceEnabledRef = useRef(false)

  useEffect(() => {
    voiceEnabledRef.current = voiceEnabled
  }, [voiceEnabled])

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
      if (msg.type === "swarm_changed" && msg.parent_task_id) {
        // Refresh agents list in case new workers appeared.
        fetchAgents()
      }
    })
  }, [fetchAgents, subscribe, selectedId, baseUrl])

  // Treat ?task= as a one-shot deep link: select it once, then clear the param
  // so it doesn't reassert on every agents update and fight sidebar selection.
  // &artifact= names something that agent showed (the TUI links to it, since
  // it cannot draw it): it is handed to the artifact panel below.
  const [artifactLink, setArtifactLink] = useState<{ agentId: string; itemId: string } | null>(null)
  useEffect(() => {
    if (linkedTask && agents.some((agent) => agent.task_id === linkedTask)) {
      const linkedItem = searchParams.get("artifact")
      if (linkedItem) setArtifactLink({ agentId: linkedTask, itemId: linkedItem })
      setSelectedId(linkedTask)
      const next = new URLSearchParams(searchParams)
      next.delete("task")
      next.delete("artifact")
      setSearchParams(next, { replace: true })
    }
  }, [agents, linkedTask, setSelectedId, searchParams, setSearchParams])

  // Global keyboard handler for voice dictation: 'v' toggles record/send,
  // Escape cancels recording. Only active when the textarea is not focused
  // and voice is enabled.
  useEffect(() => {
    const onKeyDown = (e: globalThis.KeyboardEvent) => {
      if (!voiceEnabledRef.current || !selectedId) return
      const target = e.target as HTMLElement
      const isTyping = target.tagName === "TEXTAREA" || target.tagName === "INPUT" || target.isContentEditable
      if (isTyping) return

      if (e.key === "v" || e.key === "V") {
        e.preventDefault()
        if (promptBarRef.current?.voiceIsRecording()) {
          promptBarRef.current.voiceSend()
        } else {
          promptBarRef.current?.voiceStart()
        }
      } else if (e.key === "Escape" && promptBarRef.current?.voiceIsRecording()) {
        e.preventDefault()
        promptBarRef.current.voiceCancel()
      }
    }
    window.addEventListener("keydown", onKeyDown)
    return () => window.removeEventListener("keydown", onKeyDown)
  }, [selectedId])

  const handleSelect = useCallback(
    (taskId: string) => {
      setSingleAgentOverride(false)
      setSelectedId(taskId)
      setPromptState(null)
      dismissedPromptTypeRef.current = null
    },
    [setSelectedId]
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

  const handleSubmitMessage = useCallback(
    (content: string) => {
      if (selectedId) {
        send({ type: "agent_message", agent_id: selectedId, content })
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

  const handleUpgradeChimera = useCallback(async () => {
    if (!selectedId) return
    const response = await apiFetch(baseUrl, `/api/agents/${encodeURIComponent(selectedId)}/chimera/discover`, {
      method: "POST",
    })
    if (!response.ok) {
      throw new Error((await response.text()).trim() || "Could not upgrade the agent to the native panel")
    }
    await fetchAgents()
  }, [selectedId, baseUrl, fetchAgents])

  const handleDowngradeChimera = useCallback(async () => {
    if (!selectedId) return
    const response = await apiFetch(baseUrl, `/api/agents/${encodeURIComponent(selectedId)}/chimera/downgrade`, {
      method: "POST",
    })
    if (!response.ok) {
      throw new Error((await response.text()).trim() || "Could not downgrade the agent to tmux")
    }
    await fetchAgents()
  }, [selectedId, baseUrl, fetchAgents])

  const handleDisconnect = useCallback(() => {
    setSelectedId(null)
    setPromptState(null)
    dismissedPromptTypeRef.current = null
  }, [setSelectedId])

  const killAgent = useCallback(async (taskId: string, role?: string) => {
    const confirmation = role === "conductor"
      ? "Cancel this swarm? This kills the conductor and all workers."
      : "Kill this agent session?"
    if (!window.confirm(confirmation)) return
    localStorage.removeItem(`legato:draft:${taskId}`)
    try {
      const response = await apiFetch(baseUrl, "/api/agents/kill", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ task_id: taskId }),
      })
      if (!response.ok) {
        throw new Error((await response.text()).trim() || "Failed to kill agent")
      }
    } catch (error) {
      addToast(error instanceof Error ? error.message : "Failed to kill agent", "error")
    }
  }, [baseUrl, addToast])

  const handleKill = useCallback(() => {
    if (selectedId) void killAgent(selectedId, agents.find((agent) => agent.task_id === selectedId)?.role)
  }, [selectedId, agents, killAgent])

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

  const runningAgents = useMemo(() => agents.filter((a) => a.status === "running"), [agents])

  const selectedAgent = agents.find((a) => a.task_id === selectedId)
  const selectedParentId = selectedAgent?.parent_task_id ?? activeSwarmParentId
  const swarmMode = Boolean(selectedParentId && !singleAgentOverride)
  const conductor = selectedParentId
    ? runningAgents.find((agent) => agent.parent_task_id === selectedParentId && agent.role === "conductor")
    : null
  const interactiveAgentIds = useMemo(() => {
    // The ensemble shows no terminal, so none is streamed for it.
    if (!selectedId || ensemble) return []
    const ids = swarmMode ? [conductor?.task_id, selectedId] : [selectedId]
    return Array.from(new Set(ids.filter((id): id is string => Boolean(id))))
      .filter((id) => !agents.find((agent) => agent.task_id === id)?.has_chimera_endpoint)
  }, [agents, conductor?.task_id, ensemble, selectedId, swarmMode])
  const { items: relatedWork } = useTaskActivity(selectedId ?? "")

  useEffect(() => {
    if (selectedAgent?.parent_task_id && !singleAgentOverride) {
      setActiveSwarmParentId(selectedAgent.parent_task_id)
    } else if (!selectedId || singleAgentOverride || !selectedAgent?.parent_task_id) {
      setActiveSwarmParentId(null)
    }
  }, [selectedAgent?.parent_task_id, selectedId, singleAgentOverride])

  useEffect(() => {
    if (selectedId && (!selectedAgent || selectedAgent.status !== "running") && conductor) {
      setSelectedId(conductor.task_id)
      setPromptState(null)
    }
  }, [conductor, selectedAgent, selectedId, setSelectedId])

  useEffect(() => {
    if (!selectedParentId || !swarmMode) return
    void fetchState(selectedParentId)
  }, [fetchState, selectedParentId, swarmMode])

  useEffect(() => {
    if (!connected) return
    const next = new Set(interactiveAgentIds)
    for (const id of subscribedAgentIds.current) {
      if (!next.has(id)) send({ type: "unsubscribe_agent", agent_id: id })
    }
    for (const id of next) {
      if (!subscribedAgentIds.current.has(id)) send({ type: "subscribe_agent", agent_id: id })
    }
    subscribedAgentIds.current = next
    return () => {
      for (const id of next) send({ type: "unsubscribe_agent", agent_id: id })
      subscribedAgentIds.current = new Set()
    }
  }, [connected, connectionGeneration, interactiveAgentIds, send])

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
  // The artifact panel needs room beside the conversation (and beside the
  // swarm event log, when there is one). Without it, it is a full-screen
  // sheet, as on a phone.
  const [viewportWidth, setViewportWidth] = useState(() => window.innerWidth)
  useEffect(() => {
    const check = () => {
      setIsMobile(window.innerWidth <= 768)
      setViewportWidth(window.innerWidth)
    }
    check()
    window.addEventListener("resize", check)
    return () => window.removeEventListener("resize", check)
  }, [])
  const artifactSheet = viewportWidth < (selectedParentId ? 1500 : 1100)
  const artifacts = useArtifactPanel(selectedId, !artifactSheet)
  const showLinkedArtifact = artifacts.showFor
  useEffect(() => {
    if (!artifactLink) return
    showLinkedArtifact(artifactLink.agentId, artifactLink.itemId)
    setArtifactLink(null)
  }, [artifactLink, showLinkedArtifact])
  const { items: shownItems } = useAgentUIItems(selectedId)
  // A native session draws its cards in the conversation; a terminal session
  // has only the panel to show them in.
  // A studio is a tool the person works in, not something the agent made to
  // be read: it is not counted with the artifacts, and has its own way in.
  const madeItems = shownItems.filter((item) => item.format !== "studio")
  const studioItem = [...shownItems].reverse().find((item) => item.format === "studio")
  const artifactCount = selectedAgent?.has_chimera_endpoint
    ? madeItems.filter((item) => item.placement === "panel").length
    : madeItems.length
  const newArtifacts = selectedId ? artifacts.unseen[selectedId] ?? 0 : 0
  const openArtifact = artifacts.openItem
  const artifactsButton = {
    count: artifactCount, fresh: newArtifacts, onToggle: artifacts.toggle,
    studio: studioItem ? { onOpen: () => openArtifact(studioItem.id, 0) } : undefined,
  }

  // If the selected agent died, deselect it.
  useEffect(() => {
    if (selectedId && selectedAgent && selectedAgent.status !== "running" && !conductor) {
      handleDisconnect()
    }
  }, [selectedId, selectedAgent, conductor, handleDisconnect])

  const handleOpenStartSwarm = useCallback(() => {
    setStartSwarmPreselect(selectedParentId ?? undefined)
    setShowStartSwarm(true)
  }, [selectedParentId])

  // e changes between the list and the ensemble, unless the key is being
  // typed somewhere or a dialog is open.
  const hasAgents = runningAgents.length > 0
  useEffect(() => {
    if (!hasAgents) return
    const onKeyDown = (event: globalThis.KeyboardEvent) => {
      if (event.defaultPrevented || event.repeat || event.ctrlKey || event.altKey || event.metaKey || event.shiftKey) return
      if (event.key.toLowerCase() !== "e" || isEditableTarget(event.target)) return
      if (document.querySelector('[role="dialog"]')) return
      event.preventDefault()
      showEnsemble(!ensemble)
    }
    window.addEventListener("keydown", onKeyDown)
    return () => window.removeEventListener("keydown", onKeyDown)
  }, [ensemble, hasAgents, showEnsemble])

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

  if (ensemble) {
    return (
      <div className="flex h-full flex-col overflow-hidden">
        <AgentEnsemble
          agents={runningAgents}
          onOpen={(taskId) => {
            handleSelect(taskId)
            showEnsemble(false)
          }}
          onSpawn={handleSpawn}
          onShowList={() => showEnsemble(false)}
        />
        <SpawnEphemeralModal
          open={showSpawn}
          onClose={() => setShowSpawn(false)}
          onSpawned={fetchAgents}
        />
      </div>
    )
  }

  return (
    <div className="flex h-full flex-col overflow-hidden md:flex-row">
      {workflowRun && <Link to={`/workflows/runs/${workflowRun}`} className="absolute right-3 top-3 z-30 rounded bg-zinc-900 px-3 py-2 text-xs text-indigo-300 shadow">Back to workflow</Link>}
      {isMobile ? (
        <div className="flex shrink-0 gap-2 border-b border-zinc-800 bg-zinc-950 p-2">
          <AgentSelect
            agents={runningAgents}
            selectedId={selectedId}
            onSelect={handleSelect}
          />
          <button
            onClick={() => showEnsemble(true)}
            aria-label="Ensemble"
            title="Ensemble: every agent at once"
            className="flex items-center rounded border border-zinc-700 px-3 py-2 text-zinc-300 transition-colors hover:bg-zinc-800"
          >
            <LayoutGrid size={16} aria-hidden="true" />
          </button>
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
          onShowEnsemble={() => showEnsemble(true)}
          onAgentContextMenu={(agent, position) => setAgentContextMenu({ agent, position })}
          modifierHeld={modifierHeld}
          unseenArtifacts={artifacts.unseen}
        />
      )}

      <div className="relative flex min-h-0 min-w-0 flex-1 flex-col">
        <RelatedWorkStrip items={relatedWork} />
        {selectedId ? (
          <>
            <div className="relative flex min-h-0 min-w-0 flex-1 overflow-hidden">
              {swarmMode && selectedParentId ? (
                <SwarmCommandCenter
                  parentId={selectedParentId}
                  agents={runningAgents}
                  status={swarmStates[selectedParentId]?.status ?? null}
                  focusedId={selectedId}
                  mobile={isMobile}
                  onFocus={handleSelect}
                  onOpenSingle={(taskId) => {
                    setSingleAgentOverride(true)
                    setSelectedId(taskId)
                    setPromptState(null)
                  }}
                  renderParticipant={(agent) => agent.has_chimera_endpoint ? (
                    <NativeAgentPanel
                      key={agent.task_id}
                      agentId={agent.task_id}
                      agentKind={agent.agent_kind}
                      composerEnabled={agent.task_id === selectedId}
                      onOpenUIItem={agent.task_id === selectedId ? artifacts.openItem : undefined}
                      artifacts={agent.task_id === selectedId ? artifactsButton : undefined}
                    />
                  ) : (
                    <TerminalPanel agentId={agent.task_id} onGlitch={triggerGlitch} onClickTerminal={() => {
                      handleSelect(agent.task_id)
                      promptBarRef.current?.focus()
                    }} />
                  )}
                />
              ) : selectedAgent?.has_chimera_endpoint ? (
                <NativeAgentPanel
                  key={selectedId}
                  agentId={selectedId}
                  onKill={handleKill}
                  onDisconnect={handleDisconnect}
                  onDowngradeChimera={handleDowngradeChimera}
                  ntfyConfigured={ntfyConfigured}
                  notifyEnabled={selectedAgent?.notify_enabled}
                  onToggleNotify={handleToggleNotify}
                  voiceEnabled={voiceEnabled}
                  agentKind={selectedAgent?.agent_kind}
                  onOpenUIItem={artifacts.openItem}
                  artifacts={artifactsButton}
                />
              ) : (
                <TerminalPanel agentId={selectedId} onGlitch={triggerGlitch} onClickTerminal={() => promptBarRef.current?.focus()} />
              )}
              {glitching && !selectedAgent?.has_chimera_endpoint && (
                <div className="terminal-glitch-overlay" aria-hidden="true" />
              )}
            </div>
            {!selectedAgent?.has_chimera_endpoint && (
              <PromptBar
                ref={promptBarRef}
                promptState={isPromptDetectionEnabled ? promptState : null}
                onSendKeys={handleSendKeys}
                onSubmitMessage={handleSubmitMessage}
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
                onUpgradeChimera={canBeNative(selectedAgent?.agent_kind) ? handleUpgradeChimera : undefined}
                artifacts={artifactsButton}
              />
            )}
          </>
        ) : (
          <div className="flex flex-1 items-center justify-center">
            <p className="text-zinc-500">Select an agent to view output</p>
          </div>
        )}
      </div>

      {selectedId && artifacts.open && (
        <AgentArtifactPanel
          agentId={selectedId}
          focus={artifacts.focus}
          onFocus={artifacts.openItem}
          onClose={artifacts.close}
          mobile={artifactSheet}
        />
      )}

      {agentContextMenu && (
        <AgentContextMenu
          agent={agentContextMenu.agent}
          position={agentContextMenu.position}
          onClose={() => setAgentContextMenu(null)}
          onKill={() => {
            const taskId = agentContextMenu.agent.task_id
            setAgentContextMenu(null)
            void killAgent(taskId, agentContextMenu.agent.role)
          }}
        />
      )}

      {/* Right sidebar: swarm event log for swarm participants */}
      {selectedParentId && (isMobile ? (
        <details className="fixed inset-x-2 bottom-2 z-30 max-h-[45vh] overflow-y-auto rounded-lg border border-zinc-700 bg-zinc-950 shadow-2xl">
          <summary className="cursor-pointer px-3 py-2 text-xs font-medium text-zinc-300">Swarm events</summary>
          <div className="max-h-[38vh] overflow-y-auto border-t border-zinc-800">
            <SwarmEventLog parentId={selectedParentId} />
          </div>
        </details>
      ) : (
        <div className="hidden w-64 flex-col border-l border-zinc-800 md:flex">
          <SwarmEventLog parentId={selectedParentId} />
        </div>
      ))}

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
