import { useState, useEffect, useLayoutEffect, useCallback, useRef, useMemo } from "react"
import { PreparingAgents } from "../components/PreparingAgents"
import { InterruptedAgents } from "../components/InterruptedAgents"
import { Link, useSearchParams } from "react-router-dom"
import { useWebSocket, type AgentInfo, type WSMessage, type PromptState } from "../hooks/useWebSocket"
import { usePageInView } from "../hooks/usePageInView"
import { AgentSidebar } from "../components/AgentSidebar"
import { AgentContextMenu } from "../components/AgentContextMenu"
import { useAgentGroups } from "../hooks/useAgentGroups"
import { TerminalPanel } from "../components/TerminalPanel"
import { LiveTerminal } from "../components/LiveTerminal"
import { TerminalKeys } from "../components/TerminalKeys"
import { NativeAgentPanel } from "../components/NativeAgentPanel"
import { RelatedWorkPanel } from "../components/RelatedWorkPanel"
import { PromptBar, type PromptBarHandle } from "../components/PromptBar"
import { StartSwarmModal } from "../components/StartSwarmModal"
import { SpawnEphemeralModal } from "../components/SpawnEphemeralModal"
import { SwarmEventLog } from "../components/SwarmEventLog"
import { SwarmCommandCenter } from "../components/SwarmCommandCenter"
import { AgentEnsemble } from "../components/AgentEnsemble"
import { AgentPickerBar } from "../components/AgentSelect"
import { ReeveEntry, ReeveStatus } from "../components/ReeveEntry"
import { AskReeve } from "../components/AskReeve"
import { reeveBrief } from "../lib/reeve"
import { AgentArtifactPanel } from "../components/AgentArtifactPanel"
import { useAgentUIItems } from "../hooks/useAgentUI"
import { useReviewBriefing, useUnseenBriefingPages } from "../hooks/useReviewBriefing"
import { useArtifactPanel } from "../hooks/useArtifactPanel"
import { useServer } from "../hooks/useServer"
import { useSwarmEvents } from "../hooks/useSwarmEvents"
import { useTaskActivity } from "../hooks/useTaskActivity"
import { useTaskCard } from "../hooks/useTaskCard"
import { splitRelated } from "../lib/relatedWork"
import { apiFetch, toggleAgentNotify } from "../lib/api"
import { canBeNative } from "../lib/agentKind"
import { useAgentSelection } from "../hooks/useAgentSelection"
import { isEditableTarget } from "../hooks/useMacroHotkeys"
import { useSettings } from "../hooks/useSettings"
import { useToast } from "../hooks/useToast"
import { useConfirm } from "../hooks/useConfirm"
import { RenameAgentDialog } from "../components/RenameAgentDialog"
import { useAgentContexts } from "../hooks/useAgentContexts"
import { formatAgentTime, stateSeconds } from "../lib/agentList"
import { AgentChatHeader, type AgentChatState, type AgentMenuEntry, type SidePanel, type SidePanelsProps } from "../components/AgentChatHeader"
import { Bell, BellOff, FolderInput, Pencil, RefreshCw, RotateCw, Shield, Skull, Sparkles, Terminal, Unplug } from "lucide-react"
import { useTerminalMenu } from "../hooks/useTerminalMenu"
import { canReloadAgent, reloadAgent, sandboxSwitchLabel, switchAgentSandbox, type SandboxSwitch } from "../lib/agentReload"

const GLITCH_DURATION_MS = 500

// The state the header of a terminal session shows: what its hooks report.
function terminalHeaderState(activity: string): AgentChatState {
  switch (activity) {
    case "waiting":
      return "needs-you"
    case "working":
      return "running"
    case "background":
      return "background"
    case "error":
      return "error"
    default:
      return "idle"
  }
}

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
  // Terminals are streamed only while the page is in view.
  const inView = usePageInView()
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
  // Counts the terminal's reconnects: a new one attaches a new tmux client,
  // which redraws the whole screen.
  const [terminalAttach, setTerminalAttach] = useState(0)
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
  const { confirm, dialog: confirmDialog } = useConfirm()
  const [renaming, setRenaming] = useState<{ taskId: string; title: string } | null>(null)
  // The selected native agent's own refresh, for its entry in the context menu.
  const nativeResyncRef = useRef<(() => void) | null>(null)
  const contexts = useAgentContexts(agents)
  const settings = useSettings()
  const ntfyConfigured = Boolean(settings.data?.ntfy_configured)
  const swarmEnabled = Boolean(settings.data?.swarm_enabled)
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
      setTerminalAttach((n) => n + 1)
    }
  }, [selectedId, send])

  const upgradeChimeraFor = useCallback(async (taskId: string) => {
    const response = await apiFetch(baseUrl, `/api/agents/${encodeURIComponent(taskId)}/chimera/discover`, {
      method: "POST",
    })
    if (!response.ok) {
      throw new Error((await response.text()).trim() || "Could not upgrade the agent to the native panel")
    }
    await fetchAgents()
  }, [baseUrl, fetchAgents])
  const handleUpgradeChimera = useCallback(async () => {
    if (selectedId) await upgradeChimeraFor(selectedId)
  }, [selectedId, upgradeChimeraFor])

  const downgradeChimeraFor = useCallback(async (taskId: string) => {
    const response = await apiFetch(baseUrl, `/api/agents/${encodeURIComponent(taskId)}/chimera/downgrade`, {
      method: "POST",
    })
    if (!response.ok) {
      throw new Error((await response.text()).trim() || "Could not downgrade the agent to tmux")
    }
    await fetchAgents()
  }, [baseUrl, fetchAgents])
  const handleDowngradeChimera = useCallback(async () => {
    if (selectedId) await downgradeChimeraFor(selectedId)
  }, [selectedId, downgradeChimeraFor])

  const handleDisconnect = useCallback(() => {
    setSelectedId(null)
    setPromptState(null)
    dismissedPromptTypeRef.current = null
  }, [setSelectedId])

  const killAgent = useCallback(async (taskId: string, role?: string) => {
    const swarm = role === "conductor"
    if (!await confirm({
      title: swarm ? "Cancel swarm" : "Kill agent",
      message: swarm ? "Cancel this swarm? This kills the conductor and all workers." : "Kill this agent session?",
      confirmLabel: swarm ? "Cancel swarm" : "Kill agent",
      destructive: true,
    })) return
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
  }, [baseUrl, addToast, confirm])

  const handleKill = useCallback(() => {
    if (selectedId) void killAgent(selectedId, agents.find((agent) => agent.task_id === selectedId)?.role)
  }, [selectedId, agents, killAgent])

  // Stops the selected agent and starts it again, resuming its conversation,
  // so it reads its MCP servers, plugins and hooks afresh. An agent at work is
  // interrupted, so that is asked first.
  const reloadFor = useCallback(async (agent: AgentInfo) => {
    if ((agent.activity === "working" || agent.activity === "background") &&
      !await confirm({
        title: "Reload agent",
        message: "This agent is at work. Reload it anyway? The current turn is stopped.",
        confirmLabel: "Reload anyway",
        destructive: true,
      })) return
    try {
      await reloadAgent(baseUrl, agent.task_id)
      addToast(`Reloaded ${agent.task_id}`, "success")
    } catch (error) {
      addToast(error instanceof Error ? error.message : "The agent could not be reloaded", "error")
    }
    await fetchAgents()
  }, [baseUrl, addToast, fetchAgents, confirm])
  const handleReload = useCallback(async () => {
    const agent = agents.find((candidate) => candidate.task_id === selectedId)
    if (agent) await reloadFor(agent)
  }, [agents, selectedId, reloadFor])
  const canReload = canReloadAgent(agents.find((agent) => agent.task_id === selectedId))

  // Reloads an agent out of the sandbox, or back into it, resuming its
  // conversation: the TUI's U. Asked first when the agent is at work.
  const switchSandboxFor = useCallback(async (agent: AgentInfo) => {
    const label = sandboxSwitchLabel(agent)
    if (!label) return
    if ((agent.activity === "working" || agent.activity === "background") &&
      !await confirm({
        title: label,
        message: "This agent is at work. Reload it anyway? The current turn is stopped.",
        confirmLabel: "Reload anyway",
        destructive: true,
      })) return
    try {
      const sandboxed = await switchAgentSandbox(baseUrl, agent.task_id)
      addToast(`Reloaded ${agent.task_id} ${sandboxed ? "in the sandbox" : "without the sandbox"}`, "success")
    } catch (error) {
      addToast(error instanceof Error ? error.message : "The agent could not be reloaded", "error")
    }
    await fetchAgents()
  }, [baseUrl, addToast, fetchAgents, confirm])
  const sandboxSwitchOf = useCallback((agent: AgentInfo | undefined): SandboxSwitch | undefined => {
    const label = sandboxSwitchLabel(agent)
    return agent && label ? { label, onSelect: () => { void switchSandboxFor(agent) } } : undefined
  }, [switchSandboxFor])
  const selectedSandboxSwitch = sandboxSwitchOf(agents.find((agent) => agent.task_id === selectedId))

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
  // The Reeve is pinned above the list, apart from its groups and numbers.
  const listedAgents = useMemo(() => runningAgents.filter((a) => !a.reeve), [runningAgents])
  const refreshAgents = useCallback(() => { void fetchAgents() }, [fetchAgents])
  const agentGroups = useAgentGroups({ agents: listedAgents, onChanged: refreshAgents })
  const listOrder = agentGroups.listOrder

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
    if (!connected || !inView) return
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
  }, [connected, connectionGeneration, inView, interactiveAgentIds, send])

  const toggleNotifyFor = useCallback(async (agent: AgentInfo) => {
    try {
      await toggleAgentNotify(baseUrl, agent.task_id, !agent.notify_enabled)
      fetchAgents()
    } catch {
      // ignore
    }
  }, [baseUrl, fetchAgents])
  const handleToggleNotify = useCallback(async () => {
    if (selectedAgent) await toggleNotifyFor(selectedAgent)
  }, [selectedAgent, toggleNotifyFor])

  // Keyboard agent switching: modifier + digit
  const [modifierHeld, setModifierHeld] = useState(false)
  useLayoutEffect(() => {
    const onKeyDown = (e: globalThis.KeyboardEvent) => {
      const mod = getSwitchModifier()
      if (isModifierActive(e, mod)) {
        setModifierHeld(true)
        const digit = parseInt(e.key, 10)
        if (digit >= 1 && digit <= 9) {
          e.preventDefault()
          const agent = listOrder(listedAgents)[digit - 1]
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
  }, [listedAgents, listOrder, selectedId, handleSelect, triggerGlitch])

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
  // The review briefing on the agent's branch is listed with the artifacts,
  // and marked new when a page appears or is rebuilt.
  const briefing = useReviewBriefing(selectedId)
  const unseenBriefing = useUnseenBriefingPages(selectedId, briefing.pages, artifacts.open)
  const newArtifacts = (selectedId ? artifacts.unseen[selectedId] ?? 0 : 0) + unseenBriefing
  const openArtifact = artifacts.openItem
  // The column beside the chat shows either the work related to the agent's
  // task or what the agent has shown; the header switches between them.
  const cardId = selectedAgent ? selectedAgent.attribution_task_id || selectedAgent.task_id : ""
  const card = useTaskCard(cardId)
  const pullRequest = card?.id === cardId && card.pr_meta?.pr_number ? card.pr_meta.pr_number : undefined
  const [relatedOpen, setRelatedOpen] = useState(false)
  const artifactsOpen = artifacts.open
  useEffect(() => {
    if (artifactsOpen) setRelatedOpen(false)
  }, [artifactsOpen])
  const closeArtifacts = artifacts.close
  const toggleArtifacts = artifacts.toggle
  const selectPanel = useCallback((panel: SidePanel) => {
    if (panel === "artifacts") {
      setRelatedOpen(false)
      toggleArtifacts()
    } else if (relatedOpen) {
      setRelatedOpen(false)
    } else {
      closeArtifacts()
      setRelatedOpen(true)
    }
  }, [closeArtifacts, relatedOpen, toggleArtifacts])
  const sidePanels: SidePanelsProps = {
    open: artifactsOpen ? "artifacts" : relatedOpen ? "related" : null,
    // The card itself is always linked, and its pull request when it has one.
    related: { count: relatedWork.length + 1 + (pullRequest ? 1 : 0), live: splitRelated(relatedWork).inProgress.length > 0 },
    artifacts: { count: artifactCount + briefing.pages.length, fresh: newArtifacts },
    onSelect: selectPanel,
    studio: studioItem ? { onOpen: () => openArtifact(studioItem.id, 0) } : undefined,
  }

  // The More actions menu of a session driven through its terminal, in the
  // header above it.
  const openRename = useCallback((agent: AgentInfo) => {
    setRenaming({ taskId: agent.task_id, title: agent.task_title || "" })
  }, [])
  const handleRename = useCallback(() => {
    if (selectedAgent) openRename(selectedAgent)
  }, [selectedAgent, openRename])
  const terminalMenu = useTerminalMenu({
    onRename: handleRename,
    onMoveToGroup: selectedAgent && !selectedAgent.reeve ? () => agentGroups.openMove(selectedAgent) : undefined,
    agentKind: selectedAgent?.agent_kind,
    onUpgradeChimera: canBeNative(selectedAgent?.agent_kind) ? handleUpgradeChimera : undefined,
    onRefresh: handleRefresh,
    onDetectPrompt: handleDetectPrompt,
    onTogglePromptDetection: handleTogglePromptDetection,
    promptDetectionEnabled: isPromptDetectionEnabled,
    ntfyConfigured,
    notifyEnabled: selectedAgent?.notify_enabled,
    onToggleNotify: () => { void handleToggleNotify() },
    onReload: canReload ? () => { void handleReload() } : undefined,
    switchSandbox: selectedSandboxSwitch,
    onDisconnect: handleDisconnect,
    onKill: handleKill,
  })

  // What the right-click menu of an agent in the sidebar offers: the entries
  // of the More actions menu of that agent. The selected agent's own menu is
  // reused as it stands; for another agent the actions that act on the open
  // terminal (refreshing it, prompt detection) have nothing to act on, so
  // what is left are the ones that name the agent by itself.
  const contextMenuEntries = (agent: AgentInfo): AgentMenuEntry[] => {
    const isSelected = agent.task_id === selectedId
    const native = Boolean(agent.has_chimera_endpoint)
    if (isSelected && !native) return terminalMenu.entries
    const icon = (Icon: typeof Pencil) => <Icon size={14} />
    const fail = (error: unknown) => addToast(error instanceof Error ? error.message : String(error), "error")
    const entries: AgentMenuEntry[] = [{ label: "Rename", icon: icon(Pencil), onSelect: () => openRename(agent) }]
    if (!agent.reeve) entries.push({ label: "Move to group…", icon: icon(FolderInput), onSelect: () => agentGroups.openMove(agent) })
    if (isSelected && native) {
      entries.push({ label: "Refresh from snapshot", icon: icon(RefreshCw), onSelect: () => nativeResyncRef.current?.() })
    }
    if (ntfyConfigured) {
      entries.push({
        label: agent.notify_enabled ? "Disable notify" : "Enable notify",
        icon: icon(agent.notify_enabled ? Bell : BellOff),
        onSelect: () => toggleNotifyFor(agent),
      })
    }
    if (native) {
      entries.push({ label: "Downgrade to tmux", icon: icon(Terminal), onSelect: () => downgradeChimeraFor(agent.task_id).catch(fail) })
    } else if (canBeNative(agent.agent_kind)) {
      entries.push({
        label: agent.agent_kind === "chimera" ? "Upgrade to native Chimera" : "Upgrade to native panel",
        icon: icon(Sparkles),
        onSelect: () => upgradeChimeraFor(agent.task_id).catch(fail),
      })
    }
    if (canReloadAgent(agent)) entries.push({ label: "Reload agent", icon: icon(RotateCw), onSelect: () => reloadFor(agent) })
    const sandboxSwitch = sandboxSwitchOf(agent)
    if (sandboxSwitch) entries.push({ label: sandboxSwitch.label, icon: icon(Shield), onSelect: sandboxSwitch.onSelect })
    if (isSelected) entries.push({ label: "Disconnect", icon: icon(Unplug), onSelect: handleDisconnect })
    entries.push({ label: "Kill agent", icon: icon(Skull), onSelect: () => killAgent(agent.task_id, agent.role), danger: true })
    return entries
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
  // typed somewhere or a dialog is open. A layout effect, so the key works
  // from the moment the agents are shown, not one turn later.
  const hasAgents = runningAgents.length > 0
  useLayoutEffect(() => {
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
        <InterruptedAgents className="w-full max-w-sm" />
        <PreparingAgents className="w-full max-w-sm" />
        <p className="text-lw-text-2">No active agents</p>
        <button
          onClick={handleSpawn}
          className="rounded bg-lw-accent px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-indigo-500"
        >
          Spawn Agent
        </button>
        {swarmEnabled && <button
          onClick={handleOpenStartSwarm}
          className="rounded border border-indigo-900 bg-lw-selected px-4 py-2 text-sm font-medium text-lw-link transition-colors hover:bg-lw-selected"
        >
          Start Swarm
        </button>}
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
          contexts={contexts}
          onSend={(taskId, content) => {
            // As the agent's own message box does: a native agent at work
            // reads a queued message at its next step.
            const agent = runningAgents.find((candidate) => candidate.task_id === taskId)
            const queue = Boolean(agent?.has_chimera_endpoint) && agent?.activity === "working"
            send({ type: "agent_message", agent_id: taskId, content, ...(queue ? { queue: true } : {}) })
          }}
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
      {workflowRun && <Link to={`/workflows/runs/${workflowRun}`} className="absolute right-3 top-3 z-30 rounded bg-lw-surface px-3 py-2 text-xs text-lw-link shadow">Back to workflow</Link>}
      {isMobile ? (
        <AgentPickerBar
          agents={runningAgents}
          selectedId={selectedId}
          onSelect={handleSelect}
          grouping={agentGroups.grouping}
          reeve={<ReeveEntry baseUrl={baseUrl} compact agent={runningAgents.find((a) => a.reeve)} selected={Boolean(selectedAgent?.reeve)} onSelect={handleSelect} />}
          onEnsemble={() => showEnsemble(true)}
          onSpawn={handleSpawn}
        />
      ) : (
        <AgentSidebar
          agents={runningAgents}
          selectedId={selectedId}
          onSelect={handleSelect}
          onSpawn={handleSpawn}
          onStartSwarm={swarmEnabled ? handleOpenStartSwarm : undefined}
          onShowEnsemble={() => showEnsemble(true)}
          onAgentContextMenu={(agent, position) => setAgentContextMenu({ agent, position })}
          modifierHeld={modifierHeld}
          unseenArtifacts={artifacts.unseen}
          contexts={contexts}
          grouping={agentGroups.grouping}
          preparing={<><InterruptedAgents /><PreparingAgents /></>}
          reeve={<ReeveEntry baseUrl={baseUrl} agent={runningAgents.find((a) => a.reeve)} selected={Boolean(selectedAgent?.reeve)} onSelect={handleSelect} />}
        />
      )}
      {isMobile && <InterruptedAgents className="border-b border-lw-line px-2 pb-2" />}
      {isMobile && <PreparingAgents className="border-b border-lw-line px-2 pb-2" />}

      <div className="relative flex min-h-0 min-w-0 flex-1 flex-col">
        {selectedId ? (
          <>
            {!swarmMode && selectedAgent && !selectedAgent.has_chimera_endpoint && (
              <AgentChatHeader
                state={terminalHeaderState(selectedAgent.activity)}
                time={formatAgentTime(stateSeconds(selectedAgent))}
                title={selectedAgent.task_title || selectedAgent.task_id}
                taskId={selectedAgent.task_id}
                agentKind={selectedAgent.agent_kind}
                engram={selectedAgent.engram}
                pullRequest={pullRequest}
                panels={sidePanels}
                menu={terminalMenu.entries}
                ask={selectedAgent.reeve ? undefined : <AskReeve brief={reeveBrief({ what: `the agent “${selectedAgent.task_title || selectedAgent.task_id}”, which waits for the operator`, ids: { task: selectedAgent.task_id, "agent tool": selectedAgent.agent_kind } })} />}
                notes={<>
                  {selectedAgent.reeve && <ReeveStatus baseUrl={baseUrl} />}
                  {terminalMenu.upgradeError && <span role="alert" className="text-lw-error">{terminalMenu.upgradeError}</span>}
                </>}
              />
            )}
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
                      reeve={agent.reeve}
                      sandboxed={agent.sandboxed}
                      agentKind={agent.agent_kind}
                      engram={agent.engram}
                      composerEnabled={agent.task_id === selectedId}
                      title={agent.task_title}
                      onRename={() => openRename(agent)}
                      activity={agent.activity}
                      stateTime={formatAgentTime(stateSeconds(agent))}
                      onOpenUIItem={agent.task_id === selectedId ? artifacts.openItem : undefined}
                      panels={agent.task_id === selectedId ? sidePanels : undefined}
                      pullRequest={agent.task_id === selectedId ? pullRequest : undefined}
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
                  reeve={selectedAgent?.reeve}
                  sandboxed={selectedAgent?.sandboxed}
                  onKill={handleKill}
                  onRename={handleRename}
                  onMoveToGroup={selectedAgent && !selectedAgent.reeve ? () => agentGroups.openMove(selectedAgent) : undefined}
                  resyncRef={nativeResyncRef}
                  onReload={canReload ? () => { void handleReload() } : undefined}
                  switchSandbox={selectedSandboxSwitch}
                  onDisconnect={handleDisconnect}
                  onDowngradeChimera={handleDowngradeChimera}
                  ntfyConfigured={ntfyConfigured}
                  notifyEnabled={selectedAgent?.notify_enabled}
                  onToggleNotify={handleToggleNotify}
                  voiceEnabled={voiceEnabled}
                  agentKind={selectedAgent?.agent_kind}
                  engram={selectedAgent?.engram}
                  onOpenUIItem={artifacts.openItem}
                  panels={sidePanels}
                  pullRequest={pullRequest}
                  title={selectedAgent?.task_title}
                  activity={selectedAgent?.activity}
                  stateTime={selectedAgent ? formatAgentTime(stateSeconds(selectedAgent)) : undefined}
                />
              ) : (
                <LiveTerminal key={`${selectedId}:${terminalAttach}`} agentId={selectedId} />
              )}
              {glitching && !selectedAgent?.has_chimera_endpoint && (
                <div className="terminal-glitch-overlay" aria-hidden="true" />
              )}
            </div>
            {/* On a desktop the terminal is the one place to type; the menu is in
                the header. A phone keeps the box, with the keys its keyboard lacks,
                and a swarm keeps it because it holds the menu there. */}
            {!selectedAgent?.has_chimera_endpoint && isMobile && <TerminalKeys onSendKeys={handleSendKeys} />}
            {!selectedAgent?.has_chimera_endpoint && (isMobile || swarmMode) && (
              <PromptBar
                ref={promptBarRef}
                promptState={isPromptDetectionEnabled ? promptState : null}
                onSendKeys={handleSendKeys}
                onSubmitMessage={handleSubmitMessage}
                onDismissPrompt={handleDismissPrompt}
                onDetectPrompt={handleDetectPrompt}
                onDisconnect={handleDisconnect}
                onKill={handleKill}
                onReload={canReload ? () => { void handleReload() } : undefined}
                  switchSandbox={selectedSandboxSwitch}
                onRefresh={handleRefresh}
                onTogglePromptDetection={handleTogglePromptDetection}
                promptDetectionEnabled={isPromptDetectionEnabled}
                agentId={selectedId}
                agentTitle={selectedAgent?.task_title}
                agentActivity={selectedAgent?.activity}
                agentCommand={selectedAgent?.command}
                agentKind={selectedAgent?.agent_kind}
                engram={selectedAgent?.engram}
                connected={connected}
                ntfyConfigured={ntfyConfigured}
                notifyEnabled={selectedAgent?.notify_enabled}
                onToggleNotify={handleToggleNotify}
                voiceEnabled={voiceEnabled}
                onUpgradeChimera={canBeNative(selectedAgent?.agent_kind) ? handleUpgradeChimera : undefined}
                panels={sidePanels}
                menuInHeader={!swarmMode}
              />
            )}
          </>
        ) : (
          <div className="flex flex-1 items-center justify-center">
            <p className="text-lw-text-2">Select an agent to view output</p>
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
      {selectedId && cardId && relatedOpen && !artifacts.open && (
        <RelatedWorkPanel
          items={relatedWork}
          card={card?.id === cardId ? card : null}
          cardId={cardId}
          onClose={() => setRelatedOpen(false)}
          mobile={artifactSheet}
        />
      )}

      {agentContextMenu && (
        <AgentContextMenu
          agent={agentContextMenu.agent}
          position={agentContextMenu.position}
          onClose={() => setAgentContextMenu(null)}
          entries={contextMenuEntries(agentContextMenu.agent)}
        />
      )}
      {renaming && (
        <RenameAgentDialog
          taskId={renaming.taskId}
          title={renaming.title}
          onRenamed={() => { void fetchAgents() }}
          onClose={() => setRenaming(null)}
        />
      )}
      {confirmDialog}
      {agentGroups.dialogs}

      {/* Right sidebar: swarm event log for swarm participants */}
      {selectedParentId && (isMobile ? (
        <details className="fixed inset-x-2 bottom-2 z-30 max-h-[45vh] overflow-y-auto rounded-lg border border-lw-line-strong bg-lw-ground shadow-2xl">
          <summary className="cursor-pointer px-3 py-2 text-xs font-medium text-[#c9c8d0]">Swarm events</summary>
          <div className="max-h-[38vh] overflow-y-auto border-t border-lw-line">
            <SwarmEventLog parentId={selectedParentId} />
          </div>
        </details>
      ) : (
        <div className="hidden w-64 flex-col border-l border-lw-line md:flex">
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
