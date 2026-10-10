import { useState, useEffect, useCallback, useRef, useMemo, memo } from "react"
import { ArrowDown, ArrowLeftRight, Check, Cpu, Zap, MoreHorizontal, RefreshCw, Unplug, Skull, Bell, BellOff, Terminal } from "lucide-react"
import { useWebSocket, type PromptState, type WSMessage } from "../hooks/useWebSocket"
import { useServer } from "../hooks/useServer"
import { useMacroHotkeys } from "../hooks/useMacroHotkeys"
import { apiFetch } from "../lib/api"
import { dispatchAgentInput } from "../lib/agentInput"
import { VoiceRecorder, type VoiceRecorderHandle } from "./VoiceRecorder"
import { ReviewMessageBody } from "./ReviewMessageBody"
import { Button } from "./ui/Button"
import { Dropdown, DropdownItem } from "./ui/Dropdown"
import { NativeModelSettings } from "./NativeModelSettings"
import { AgentKindBadge } from "./AgentKindBadge"
import { NativeQuestionCard } from "./NativeQuestionCard"
import { NativeTerminalPrompt } from "./NativeTerminalPrompt"
import { nativeToolName } from "../lib/agentKind"
import { parseLegatoMessage } from "../lib/legatoMessage"
import { modelSettingsResponse, type ModelCapabilities, type ModelSettings, type ModelSettingsState } from "../lib/modelSettings"
import {
  createNativeSyncState,
  reconcileNativeSnapshot,
  reduceNativeEvent,
  updatePermissionState,
  updateQuestionState,
  type NativeConversationItem,
  type NativeEvent as ChimeraEvent,
  type NativePermissionItem,
  type NativeSnapshot as SessionSnapshot,
  type NativeSyncState,
  type NativeToolItem as ToolCallItem,
} from "../lib/nativeAgentSync"

function formatTokens(value: number): string {
  if (value < 1_000) return value.toLocaleString()
  if (value < 1_000_000) return `${Number((value / 1_000).toFixed(1))}k`
  return `${Number((value / 1_000_000).toFixed(2))}M`
}

interface ModeInfo {
  name: string
  description: string
  cycle: boolean
}

interface CommandInfo {
  name: string
  description?: string
  body: string
  path: string
  source: string
}

interface SkillInfo {
  name: string
  description: string
  path: string
  source: string
}

interface ModelInfo {
  capabilities?: ModelCapabilities
  defaults?: ModelSettings
  ref: string
  contextWindow?: number
  maxOutputTokens?: number
  vision?: boolean
  isDefault: boolean
}

type SlashSuggestion = {
  type: "command" | "skill"
  name: string
  description: string
  body?: string
}

function permissionFindingLabel(finding: NonNullable<NativePermissionItem["policyAnalysis"]>["findings"][number]): string {
  if (finding.kind === "risk") return `${finding.level} risk`
  if (finding.kind === "path") return `${finding.operation} path`
  if (finding.kind === "analyzer_error") return "analyzer error"
  if (finding.kind === "blocked") return "blocked"
  return "unresolved"
}

// The native panel serves Chimera sessions and, through Legato's bridges,
// Claude Code and Codex sessions. This is the name its wording uses for each.
const agentToolName = nativeToolName

// How often the panel asks what the agent's terminal is showing, for prompts
// that never reach Legato as requests of their own.
const TERMINAL_PROMPT_POLL_MS = 2000

// The prompt types that ask the person to pick something.
const terminalPromptTypes = new Set<PromptState["type"]>(["menu", "tool_approval", "plan_approval", "question"])

function PermissionCard({
  permission,
  agentKind,
  onResolve,
}: {
  permission: NativePermissionItem
  agentKind?: string
  onResolve: (requestId: string, decision: "allow" | "deny", feedback?: string, option?: string) => void
}) {
  const [feedbackOpen, setFeedbackOpen] = useState(false)
  const [feedback, setFeedback] = useState("")
  const pending = permission.status === "pending"
  const submitting = permission.status === "submitting"
  const isPlan = Boolean(permission.plan)
  const takenOption = permission.options?.find((option) => option.id === permission.option)?.label
  const resolvedLabel = permission.status === "allowed"
    ? (takenOption ? `Allowed · ${takenOption}` : "Allowed")
    : permission.status === "denied"
      ? "Denied"
      : permission.status === "expired"
        ? "Expired"
        : null

  return (
    <section aria-label={`Permission request for ${permission.tool}`} className="mx-4 my-2 min-w-0 rounded-lg border border-yellow-600/40 bg-yellow-900/10 p-3">
      <div className="flex min-w-0 flex-wrap items-center gap-2">
        <div className="text-sm font-semibold text-yellow-200">{isPlan ? "Plan approval" : !agentKind || agentKind === "chimera" ? "Host command approval" : "Tool approval"}</div>
        <span className="rounded bg-yellow-950/70 px-1.5 py-0.5 font-mono text-[11px] text-yellow-300">{permission.tool} · {permission.target}</span>
        {resolvedLabel && <span className="ml-auto text-xs font-medium text-zinc-300">{resolvedLabel}</span>}
      </div>
      {permission.reason && <p className="mt-2 text-xs text-zinc-300">{permission.reason}</p>}
      {permission.plan ? (
        <div className="mt-2 max-h-96 overflow-auto rounded bg-black/30 p-2 text-sm text-zinc-100">
          <ReviewMessageBody body={permission.plan} />
        </div>
      ) : (
        <pre className="mt-2 max-h-48 overflow-auto whitespace-pre-wrap break-words rounded bg-black/30 p-2 font-mono text-xs text-zinc-100">{permission.command}</pre>
      )}
      {permission.policyAnalysis?.findings.length ? (
        <div className="mt-2 space-y-1" aria-label="Policy findings">
          {permission.policyAnalysis.findings.map((finding, index) => (
            <div key={`${finding.stageId}-${finding.kind}-${index}`} className="flex min-w-0 flex-wrap gap-x-2 rounded border border-zinc-700/70 bg-zinc-900/50 px-2 py-1 text-xs">
              <span className="font-medium uppercase tracking-wide text-amber-300">{permissionFindingLabel(finding)}</span>
              <span className="min-w-0 break-words text-zinc-300">{finding.message}</span>
              {finding.kind === "path" && <span className="w-full break-all font-mono text-zinc-400">{finding.resolvedPath || finding.path}</span>}
            </div>
          ))}
        </div>
      ) : null}
      {permission.error && <p role="alert" className="mt-2 text-xs text-red-400">{permission.error}</p>}
      {(pending || submitting) && (
        <div className="mt-3 space-y-2">
          <div className="flex flex-wrap gap-2">
            {permission.options?.length ? permission.options.map((option) => (
              <button key={option.id} disabled={submitting} className="rounded bg-green-700/60 px-2 py-1 text-left text-xs text-white hover:bg-green-600/60 disabled:cursor-wait disabled:opacity-50" onClick={() => onResolve(permission.requestId, "allow", undefined, option.id)}>{option.label}</button>
            )) : (
              <button disabled={submitting} className="rounded bg-green-700/60 px-2 py-1 text-xs text-white hover:bg-green-600/60 disabled:cursor-wait disabled:opacity-50" onClick={() => onResolve(permission.requestId, "allow")}>Allow</button>
            )}
            <button disabled={submitting} className="rounded bg-red-700/60 px-2 py-1 text-xs text-white hover:bg-red-600/60 disabled:cursor-wait disabled:opacity-50" onClick={() => onResolve(permission.requestId, "deny")}>{isPlan ? "Reject" : "Deny"}</button>
            <button disabled={submitting} className="rounded border border-red-700/70 px-2 py-1 text-xs text-red-200 hover:bg-red-950/50 disabled:cursor-wait disabled:opacity-50" onClick={() => setFeedbackOpen(true)}>{isPlan ? "Request changes" : "Deny with feedback"}</button>
            {submitting && <span role="status" className="self-center text-xs text-yellow-300">Submitting…</span>}
          </div>
          {feedbackOpen && (
            <div className="space-y-2">
              <label className="block text-xs text-zinc-300" htmlFor={`permission-feedback-${permission.requestId}`}>Feedback for {agentToolName(agentKind)}</label>
              <textarea id={`permission-feedback-${permission.requestId}`} value={feedback} disabled={submitting} onChange={(event) => setFeedback(event.target.value)} className="min-h-20 w-full resize-y rounded border border-zinc-700 bg-zinc-950 p-2 text-sm text-zinc-200 focus:border-red-500 focus:outline-none" />
              <div className="flex flex-wrap gap-2">
                <button disabled={submitting || !feedback.trim()} className="rounded bg-red-700/60 px-2 py-1 text-xs text-white disabled:opacity-40" onClick={() => onResolve(permission.requestId, "deny", feedback.trim())}>Send denial</button>
                <button disabled={submitting} className="rounded px-2 py-1 text-xs text-zinc-300 hover:bg-zinc-800" onClick={() => { setFeedbackOpen(false); setFeedback("") }}>Cancel</button>
              </div>
            </div>
          )}
        </div>
      )}
    </section>
  )
}

interface NativeAgentPanelProps {
  agentId: string
  onClickTerminal?: () => void
  onKill?: () => void
  onDisconnect?: () => void
  onDowngradeChimera?: () => Promise<void>
  ntfyConfigured?: boolean
  notifyEnabled?: boolean
  onToggleNotify?: () => void
  voiceEnabled?: boolean
  agentKind?: string
  composerEnabled?: boolean
}

export function NativeAgentPanel({ agentId, onKill, onDisconnect, onDowngradeChimera, ntfyConfigured, notifyEnabled, onToggleNotify, voiceEnabled, agentKind, composerEnabled = true }: NativeAgentPanelProps) {
  const { send, subscribe, connected } = useWebSocket()
  const { baseUrl } = useServer()
  const [syncState, setSyncState] = useState<NativeSyncState>(() => createNativeSyncState())
  const syncStateRef = useRef(syncState)
  const modelRequestRef = useRef(0)
  const invalidateModelRequests = useCallback(() => { modelRequestRef.current++ }, [])
  const queuedEventsRef = useRef<ChimeraEvent[] | null>(null)
  const syncGenerationRef = useRef(0)
  const runFinishTimerRef = useRef<number | null>(null)
  const hydratedAgentRef = useRef<string | null>(null)
  const [draft, setDraft] = useState("")
  const [loading, setLoading] = useState(true)
  const [syncing, setSyncing] = useState(false)
  const [mode, setMode] = useState<string>("")
  const scrollRef = useRef<HTMLDivElement>(null)
  const draftKey = `legato:chimera-draft:${agentId}`
  const { items, busy, error, contextUsage, model } = syncState
  const blocks = useMemo(() => conversationBlocks(items), [items])
  // The agent has stopped on a permission prompt or a question of its own.
  const awaitingRequest = items.some((item) => (item.permission ?? item.question)?.status === "pending")
  // What the agent's terminal is asking, as read off its screen. A request
  // the agent raised itself is drawn by its own card; the terminal shows the
  // same request as a dialog, which is not shown twice.
  const [terminalPrompt, setTerminalPrompt] = useState<PromptState | null>(null)
  const terminalAsking = !awaitingRequest && terminalPrompt !== null && terminalPromptTypes.has(terminalPrompt.type) && (terminalPrompt.actions?.length ?? 0) > 0
  const awaitingPerson = awaitingRequest || terminalAsking
  // The effort the model runs at, when the session reports one.
  const effort = syncState.settingsState?.effective.reasoningEffort
  const [isScrolledUp, setIsScrolledUp] = useState(false)
  const isScrolledUpRef = useRef(false)
  const [modes, setModes] = useState<ModeInfo[]>([])
  const [modesOpen, setModesOpen] = useState(false)
  const [models, setModels] = useState<ModelInfo[]>([])
  const [modelsOpen, setModelsOpen] = useState(false)
  const [macros, setMacros] = useState<{ name: string; keys: string }[]>([])
  const [macrosOpen, setMacrosOpen] = useState(false)
  const [menuOpen, setMenuOpen] = useState(false)
  const modesRef = useRef<HTMLDivElement>(null)
  const macrosRef = useRef<HTMLDivElement>(null)
  const menuRef = useRef<HTMLDivElement>(null)
  const modelsRef = useRef<HTMLDivElement>(null)
  const voiceRef = useRef<VoiceRecorderHandle>(null)
  const [commands, setCommands] = useState<CommandInfo[]>([])
  const [skills, setSkills] = useState<SkillInfo[]>([])
  const [slashOpen, setSlashOpen] = useState(false)
  const [slashIndex, setSlashIndex] = useState(0)
  const textareaRef = useRef<HTMLTextAreaElement>(null)

  const replaceSyncState = useCallback((next: NativeSyncState) => {
    syncStateRef.current = next
    setSyncState(next)
  }, [])

  const applyEvent = useCallback((event: ChimeraEvent) => {
    if (event.type === "model_changed" || event.type === "mode_changed") modelRequestRef.current++
    if (queuedEventsRef.current) {
      queuedEventsRef.current.push(event)
      return
    }
    replaceSyncState(reduceNativeEvent(syncStateRef.current, event))
  }, [replaceSyncState])

  const scrollToBottom = useCallback(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
      isScrolledUpRef.current = false
      setIsScrolledUp(false)
    }
  }, [])

  const scrollToBottomAfterRender = useCallback(() => {
    requestAnimationFrame(scrollToBottom)
  }, [scrollToBottom])

  // Scroll to bottom once the feed has content. The initial load always
  // scrolls to the latest message regardless of isScrolledUp, because the
  // browser fires a spurious onScroll at scrollTop=0 when content first
  // overflows. After that, scroll to bottom only when the user is near the
  // bottom (within a threshold), respecting deliberate scroll-up.
  const initialScrollDone = useRef(false)
  useEffect(() => {
    if (!loading && scrollRef.current) {
      if (!initialScrollDone.current) {
        scrollRef.current.scrollTop = scrollRef.current.scrollHeight
        isScrolledUpRef.current = false
        setIsScrolledUp(false)
        initialScrollDone.current = true
      } else {
        const el = scrollRef.current
        const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 60
        if (atBottom) {
          el.scrollTop = el.scrollHeight
        }
      }
    }
  }, [loading, syncState.items])

  // Load/save draft
  useEffect(() => {
    const saved = localStorage.getItem(draftKey)
    if (saved) setDraft(saved)
  }, [draftKey])
  useEffect(() => {
    localStorage.setItem(draftKey, draft)
  }, [draftKey, draft])


  // Fetch available modes
  useEffect(() => {
    apiFetch(baseUrl, `/api/agents/${agentId}/chimera/modes`)
      .then((r) => r.ok ? r.json() : [])
      .then((data: ModeInfo[]) => setModes(Array.isArray(data) ? data : []))
      .catch(() => {})
  }, [agentId, baseUrl])

  // Fetch available models
  useEffect(() => {
    apiFetch(baseUrl, `/api/agents/${agentId}/chimera/models`)
      .then((r) => r.ok ? r.json() : [])
      .then((data: ModelInfo[]) => setModels(Array.isArray(data) ? data : []))
      .catch(() => {})
  }, [agentId, baseUrl])

  // Fetch commands and skills for slash autocomplete
  useEffect(() => {
    Promise.all([
      apiFetch(baseUrl, `/api/agents/${agentId}/chimera/commands`).then((r) => r.ok ? r.json() : []).catch(() => []),
      apiFetch(baseUrl, `/api/agents/${agentId}/chimera/skills`).then((r) => r.ok ? r.json() : []).catch(() => []),
    ]).then(([cmds, skls]) => {
      setCommands(Array.isArray(cmds) ? cmds : [])
      setSkills(Array.isArray(skls) ? skls : [])
    })
  }, [agentId, baseUrl])

  // Compute slash suggestions from current draft
  const slashSuggestions: SlashSuggestion[] = useMemo(() => {
    if (!draft.startsWith("/")) return []
    const query = draft.slice(1).toLowerCase()
    const cmdSuggestions: SlashSuggestion[] = commands
      .filter((c) => c.name.toLowerCase().includes(query))
      .map((c) => ({ type: "command" as const, name: c.name, description: c.description || "", body: c.body }))
    const skillSuggestions: SlashSuggestion[] = skills
      .filter((s) => s.name.toLowerCase().includes(query))
      .map((s) => ({ type: "skill" as const, name: s.name, description: s.description }))
    return [...cmdSuggestions, ...skillSuggestions]
  }, [draft, commands, skills])

  // Open/close slash menu based on draft
  useEffect(() => {
    if (draft.startsWith("/") && slashSuggestions.length > 0) {
      setSlashOpen(true)
      setSlashIndex(0)
    } else {
      setSlashOpen(false)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [draft])

  // Fetch macros eagerly so numbered shortcuts are ready when the menu opens.
  useEffect(() => {
    apiFetch(baseUrl, "/api/macros")
      .then((r) => r.ok ? r.json() : { macros: [] })
      .then((data: { macros?: { name: string; keys: string }[] }) => {
        setMacros(Array.isArray(data.macros) ? data.macros : [])
      })
      .catch(() => setMacros([]))
  }, [baseUrl])

  // Close dropdowns on outside click
  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (modesRef.current && !modesRef.current.contains(e.target as Node)) setModesOpen(false)
      if (macrosRef.current && !macrosRef.current.contains(e.target as Node)) setMacrosOpen(false)
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) setMenuOpen(false)
      if (modelsRef.current && !modelsRef.current.contains(e.target as Node)) setModelsOpen(false)
    }
    document.addEventListener("mousedown", handler)
    return () => document.removeEventListener("mousedown", handler)
  }, [])

  const resync = useCallback(async (scrollAfterSync = false, initialHydration = false) => {
    const generation = ++syncGenerationRef.current
    const eventQueue = queuedEventsRef.current ?? []
    const viewport = scrollRef.current
    const scrollAnchor = viewport ? {
      atBottom: viewport.scrollHeight - viewport.scrollTop - viewport.clientHeight < 40,
      top: viewport.scrollTop,
    } : null
    queuedEventsRef.current = eventQueue
    if (viewport) setSyncing(true)
    else setLoading(true)
    try {
      const snapshotPath = `/api/agents/${agentId}/chimera/snapshot${initialHydration ? "?initial=true" : ""}`
      const response = await apiFetch(baseUrl, snapshotPath)
      if (!response.ok) throw new Error(`snapshot: ${response.status}`)
      const snapshot: SessionSnapshot = await response.json()
      if (generation !== syncGenerationRef.current) return
      setMode(snapshot.mode || "")
      replaceSyncState(reconcileNativeSnapshot(snapshot, syncStateRef.current, eventQueue))
      if (scrollAfterSync || scrollAnchor?.atBottom) {
        isScrolledUpRef.current = false
        setIsScrolledUp(false)
      } else if (scrollAnchor) {
        requestAnimationFrame(() => {
          if (scrollRef.current) scrollRef.current.scrollTop = scrollAnchor.top
        })
      }
    } catch (reason) {
      if (generation === syncGenerationRef.current) {
        let next = syncStateRef.current
        for (const event of eventQueue) {
          next = reduceNativeEvent(next, event)
        }
        replaceSyncState({
          ...next,
          error: reason instanceof Error ? reason.message : String(reason),
        })
      }
    } finally {
      if (generation === syncGenerationRef.current) {
        queuedEventsRef.current = null
        setLoading(false)
        setSyncing(false)
      }
    }
  }, [agentId, baseUrl, replaceSyncState])

  useEffect(() => {
    replaceSyncState(createNativeSyncState())
    invalidateModelRequests()
    initialScrollDone.current = false
    hydratedAgentRef.current = null
    queuedEventsRef.current = []
    return () => {
      invalidateModelRequests()
      queuedEventsRef.current = null
      hydratedAgentRef.current = null
      if (runFinishTimerRef.current != null) window.clearTimeout(runFinishTimerRef.current)
    }
  }, [agentId, replaceSyncState, invalidateModelRequests])

  const [bangQueue, setBangQueue] = useState<string[]>([])
  const [bangRunning, setBangRunning] = useState(false)
  const isBang = draft.trim().startsWith("!") && draft.trim().length > 1

  const executeBang = useCallback(async (command: string) => {
    setBangRunning(true)
    try {
      const response = await apiFetch(baseUrl, `/api/agents/${agentId}/chimera/bang`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ command }),
      })
      if (!response.ok) throw new Error(`bang: ${response.status}`)
      await resync()
      scrollToBottomAfterRender()
    } catch (reason) {
      replaceSyncState({ ...syncStateRef.current, error: reason instanceof Error ? reason.message : String(reason) })
    } finally {
      setBangRunning(false)
    }
  }, [agentId, baseUrl, resync, replaceSyncState, scrollToBottomAfterRender])

  useEffect(() => {
    const unsubscribe = subscribe((msg: WSMessage) => {
      if (msg.agent_id !== agentId) return
      if (msg.type === "prompt_state") {
        setTerminalPrompt(msg.prompt ?? null)
        return
      }
      if (msg.type === "error" && msg.request_id) {
        const reopened = updatePermissionState(syncStateRef.current, msg.request_id, (permission) => ({
          ...permission,
          status: "pending",
          error: msg.error || "Permission request failed",
        }))
        replaceSyncState(updateQuestionState(reopened, msg.request_id, (question) => ({
          ...question,
          status: "pending",
          error: msg.error || "The answer could not be delivered",
        })))
        return
      }
      if (msg.type === "chimera_sync_required") {
        void resync()
        return
      }
      if (msg.type !== "chimera_event") return
      try {
        const event: ChimeraEvent = JSON.parse(msg.content || "{}")
        applyEvent(event)
        if (event.type === "run_finished") {
          if (runFinishTimerRef.current != null) window.clearTimeout(runFinishTimerRef.current)
          runFinishTimerRef.current = window.setTimeout(() => {
            runFinishTimerRef.current = null
            void resync()
          }, 100)
          setBangQueue((prev) => {
            if (prev.length > 0) void executeBang(prev[0])
            return prev.slice(1)
          })
        }
        requestAnimationFrame(() => {
          if (scrollRef.current) {
            const el = scrollRef.current
            const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 60
            if (atBottom) {
              el.scrollTop = el.scrollHeight
            }
          }
        })
      } catch {
        replaceSyncState({ ...syncStateRef.current, error: "Invalid native event; refresh from snapshot" })
      }
    })
    if (connected) {
      send({ type: "subscribe_agent", agent_id: agentId, cursor: syncStateRef.current.cursor })
      if (hydratedAgentRef.current !== agentId) {
        hydratedAgentRef.current = agentId
        void resync(true, true)
      }
    }
    return () => {
      unsubscribe()
      if (connected) send({ type: "unsubscribe_agent", agent_id: agentId })
    }
  }, [subscribe, agentId, applyEvent, replaceSyncState, resync, executeBang, connected, send])

  const handleBuiltinSlash = useCallback(async (raw: string) => {
    const parts = raw.split(/\s+/)
    const cmd = parts[0]
    const arg = parts.slice(1).join(" ")
    try {
      if (cmd === "/model") {
        await apiFetch(baseUrl, `/api/agents/${agentId}/chimera/model`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ model: arg }),
        })
      } else if (cmd === "/mode") {
        await apiFetch(baseUrl, `/api/agents/${agentId}/chimera/modes`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ mode: arg }),
        })
      }
    } catch (reason) {
      replaceSyncState({ ...syncStateRef.current, error: reason instanceof Error ? reason.message : String(reason) })
    }
  }, [agentId, baseUrl, replaceSyncState])

  const handleSend = useCallback(() => {
    const trimmed = draft.trim()
    if (!trimmed) return
    if (trimmed.startsWith("!") && trimmed.length > 1) {
      const command = trimmed.slice(1)
      setDraft("")
      if (busy) {
        setBangQueue((prev) => [...prev, command])
      } else {
        void executeBang(command)
      }
      return
    }
    // Intercept built-in slash commands that need API calls, not message sends
    const builtinCommands = ["/model", "/mode"]
    if (builtinCommands.includes(trimmed.split(/\s+/)[0])) {
      setDraft("")
      void handleBuiltinSlash(trimmed)
      return
    }
    send({ type: "agent_message", agent_id: agentId, content: trimmed, queue: busy })
    setDraft("")
    if (!busy) replaceSyncState({ ...syncStateRef.current, busy: true, error: null })
    scrollToBottomAfterRender()
  }, [draft, busy, agentId, send, replaceSyncState, scrollToBottomAfterRender, executeBang, handleBuiltinSlash])

  const handleInterrupt = useCallback(() => {
    send({ type: "agent_interrupt", agent_id: agentId })
  }, [agentId, send])

  const handlePermission = useCallback(
    (requestId: string, decision: "allow" | "deny", feedback?: string, option?: string) => {
      const current = syncStateRef.current.items.find((item) => item.permission?.requestId === requestId)?.permission
      if (!current || current.status !== "pending") return
      replaceSyncState(updatePermissionState(syncStateRef.current, requestId, (permission) => ({
        ...permission,
        status: "submitting",
        error: undefined,
      })))
      send({
        type: "agent_permission",
        agent_id: agentId,
        request_id: requestId,
        decision,
        ...(feedback ? { feedback } : {}),
        ...(option ? { option } : {}),
      })
    },
    [agentId, replaceSyncState, send],
  )

  // A prompt can be up in the terminal without the agent having raised
  // anything: an update notice, a trust question. The server reads the
  // terminal's screen when asked.
  useEffect(() => {
    setTerminalPrompt(null)
    if (!connected) return
    const detect = () => send({ type: "detect_prompt", agent_id: agentId })
    detect()
    const timer = window.setInterval(detect, TERMINAL_PROMPT_POLL_MS)
    return () => window.clearInterval(timer)
  }, [agentId, connected, send])

  const handleTerminalChoice = useCallback((keys: string) => {
    send({ type: "send_keys", agent_id: agentId, keys })
    // The prompt is answered; what the terminal shows next is read at once.
    setTerminalPrompt(null)
    window.setTimeout(() => send({ type: "detect_prompt", agent_id: agentId }), 400)
  }, [agentId, send])

  // Answers a question the agent asked, or closes it unanswered when
  // `answers` is left out.
  const handleQuestion = useCallback(
    (requestId: string, answers?: Record<string, string[]>) => {
      const current = syncStateRef.current.items.find((item) => item.question?.requestId === requestId)?.question
      if (!current || current.status !== "pending") return
      replaceSyncState(updateQuestionState(syncStateRef.current, requestId, (question) => ({
        ...question,
        status: "submitting",
        error: undefined,
      })))
      send({
        type: "agent_question",
        agent_id: agentId,
        request_id: requestId,
        ...(answers ? { answers } : { dismissed: true }),
      })
    },
    [agentId, replaceSyncState, send],
  )

  const handleSlashSelect = useCallback((suggestion: SlashSuggestion) => {
    if (suggestion.type === "command" && suggestion.body) {
      // Expand the command body into the draft and send immediately
      setDraft(suggestion.body)
      setSlashOpen(false)
      // Auto-send the expanded command
      send({ type: "agent_message", agent_id: agentId, content: suggestion.body })
      setDraft("")
    } else {
      // For skills, insert the skill name and let the user continue typing
      setDraft(`/${suggestion.name} `)
      setSlashOpen(false)
      textareaRef.current?.focus()
    }
  }, [agentId, send])

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (slashOpen) {
        if (e.key === "ArrowDown") {
          e.preventDefault()
          setSlashIndex((i) => Math.min(i + 1, slashSuggestions.length - 1))
          return
        }
        if (e.key === "ArrowUp") {
          e.preventDefault()
          setSlashIndex((i) => Math.max(i - 1, 0))
          return
        }
        if (e.key === "Enter" && !e.shiftKey) {
          e.preventDefault()
          handleSlashSelect(slashSuggestions[slashIndex])
          return
        }
        if (e.key === "Escape") {
          e.preventDefault()
          setSlashOpen(false)
          return
        }
      }
      if (e.key === "Enter" && !e.shiftKey) {
        e.preventDefault()
        handleSend()
      }
    },
    [handleSend, slashOpen, slashIndex, slashSuggestions, handleSlashSelect],
  )

  const handleModeChange = useCallback((mode: string) => {
    apiFetch(baseUrl, `/api/agents/${agentId}/chimera/modes`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ mode }),
    }).then(() => setMode(mode)).catch(() => {})
    setModesOpen(false)
  }, [agentId, baseUrl])

  const handleModelChange = useCallback((ref: string) => {
    setModelsOpen(false)
    if (ref === model) return
    const request = ++modelRequestRef.current
    apiFetch(baseUrl, `/api/agents/${agentId}/chimera/model`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ model: ref, expectedModel: model }),
    }).then(modelSettingsResponse).then((body) => {
      if (request !== modelRequestRef.current) return
      if (syncStateRef.current.model !== model && syncStateRef.current.model !== ref) return
      const result = (body ?? {}) as { to?: string; settingsState?: ModelSettingsState }
      replaceSyncState({ ...syncStateRef.current, model: result.settingsState?.model ?? result.to ?? ref, settingsState: result.settingsState })
    }).catch((reason) => {
      if (request !== modelRequestRef.current) return
      replaceSyncState({ ...syncStateRef.current, error: reason instanceof Error ? reason.message : String(reason) })
    })
  }, [agentId, baseUrl, model, replaceSyncState])

  const handleMacro = useCallback((keys: string) => {
    dispatchAgentInput(keys, {
      executeBang: (command) => {
        if (busy) {
          setBangQueue((prev) => [...prev, command])
        } else {
          void executeBang(command)
        }
      },
      sendMessage: (content) => send({ type: "agent_message", agent_id: agentId, content }),
    })
    setMacrosOpen(false)
  }, [agentId, busy, executeBang, send])

  const openMacros = useCallback(() => setMacrosOpen(true), [])
  const closeMacros = useCallback(() => setMacrosOpen(false), [])
  useMacroHotkeys({ macros, open: macrosOpen, onOpen: openMacros, onClose: closeMacros, onSelect: handleMacro })

  if (loading) {
    return (
      <div className="flex h-full items-center justify-center">
        <p className="text-zinc-500">Loading {agentToolName(agentKind)} session...</p>
      </div>
    )
  }

  if (error && items.length === 0) {
    return (
      <div className="flex h-full items-center justify-center">
        <div className="text-center">
          <p className="text-zinc-500 mb-2">{!agentKind || agentKind === "chimera" ? "No Chimera endpoint discovered" : `No ${agentToolName(agentKind)} bridge connected`}</p>
          <p className="text-xs text-zinc-600">{error}</p>
        </div>
      </div>
    )
  }

  return (
    <div className="relative flex h-full min-w-0 w-full flex-1 flex-col overflow-hidden bg-zinc-950">
      {/* Conversation viewport owns both scrolling and its floating action. */}
      <div role="region" aria-label="Conversation" className="relative min-h-0 flex-1">
        <div data-conversation-scroll ref={scrollRef} className="h-full overflow-y-auto overflow-x-hidden" onScroll={(e) => { const el = e.currentTarget; const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 40; isScrolledUpRef.current = !atBottom; setIsScrolledUp(!atBottom) }}>
          {items.length === 0 && (
          <div className="flex h-full items-center justify-center">
            <p className="text-sm text-zinc-500">
              Session ready — send a message to start
            </p>
          </div>
        )}
        {blocks.map((block) => {
          if (block.kind === "tools") return <ToolCallGroup key={block.key} tools={block.tools} />
          const item = block.item
          if (item.role === "user") {
            // Legato's own messages arrive as user turns; they are drawn
            // apart from what the person typed.
            const legato = parseLegatoMessage(item.text)
            if (legato?.peer !== undefined) {
              // Another agent wrote this and Legato only carried it, so it
              // has a look of its own, labelled with who sent it.
              return (
                <div key={item.key} className="flex justify-end px-4 py-2">
                  <div
                    aria-label={legato.peer ? `Message from agent ${legato.peer}` : "Message from another agent"}
                    className="max-w-[75%] rounded-lg border border-fuchsia-800/60 bg-fuchsia-950/30 px-3 py-2 text-sm text-zinc-300 whitespace-pre-wrap break-words"
                  >
                    <div className="mb-1 text-[10px] font-semibold uppercase tracking-wide text-fuchsia-400">
                      Agent{legato.peer && <> · <span className="normal-case">{legato.peer}</span></>}
                    </div>
                    <div>{legato.body}</div>
                  </div>
                </div>
              )
            }
            if (legato) {
              return (
                <div key={item.key} className="flex justify-end px-4 py-2">
                  <div
                    aria-label={`Message from Legato: ${legato.label}`}
                    className="max-w-[75%] rounded-lg border border-teal-800/60 bg-teal-950/30 px-3 py-2 text-sm text-zinc-300 whitespace-pre-wrap break-words"
                  >
                    <div className="mb-1 text-[10px] font-semibold uppercase tracking-wide text-teal-400">Legato · {legato.label}</div>
                    <div>{legato.body}</div>
                  </div>
                </div>
              )
            }
            return (
              <div key={item.key} className="flex justify-end px-4 py-2">
                <div className="max-w-[75%] rounded-lg bg-blue-600/20 px-3 py-2 text-sm text-zinc-200 whitespace-pre-wrap break-words">
                  {item.text}
                </div>
              </div>
            )
          }
          if (item.role === "assistant") {
            return (
              <div key={item.key} className="flex justify-start px-4 py-2">
                <div className={`max-w-[75%] min-w-0 rounded-lg bg-zinc-800 px-3 py-2 text-sm text-zinc-200 ${item.isStreaming ? "opacity-80" : ""}`}>
                  <ReviewMessageBody body={item.text} />
                  {item.isStreaming && <span className="ml-0.5 animate-pulse">▋</span>}
                </div>
              </div>
            )
          }
          if (item.role === "tool" && item.tool) {
            // Every other tool call is in a "tools" block.
            return <SubagentCard key={item.key} tool={item.tool} />
          }
          if (item.role === "bang" && item.bang) {
            const b = item.bang
            const pending = b.exitCode === -1
            const success = b.exitCode === 0
            return (
              <div key={item.key} className={`mx-4 my-1.5 overflow-hidden rounded-lg border bg-zinc-900/50 ${pending ? "border-amber-500/60" : success ? "border-amber-500/40" : "border-red-700/50"}`}>
                <div className="flex items-center gap-2 px-3 py-1.5">
                  <span className="flex items-center gap-1 text-amber-400 shrink-0">
                    <Terminal size={13} />
                    <span className="text-[10px] font-semibold uppercase tracking-wide">bash</span>
                  </span>
                  <span className="font-mono text-xs text-zinc-300 truncate">{b.command}</span>
                  {!pending && (
                    <span className={`text-xs ml-auto shrink-0 ${success ? "text-amber-500/80" : "text-red-500"}`}>
                      exit {b.exitCode}{b.timedOut ? " (timed out)" : ""}
                    </span>
                  )}
                  {pending && <span className="ml-auto text-xs text-amber-400/60 animate-pulse">running…</span>}
                </div>
                {!pending && (b.stdout || b.stderr) && (
                  <div className="border-t border-amber-900/30 px-3 py-2">
                    {b.stdout && <pre className="font-mono text-xs text-zinc-300 whitespace-pre-wrap max-h-60 overflow-y-auto">{b.stdout}</pre>}
                    {b.stderr && <pre className="font-mono text-xs text-red-400 whitespace-pre-wrap max-h-40 overflow-y-auto mt-1">{b.stderr}</pre>}
                  </div>
                )}
              </div>
            )
          }
          if (item.permission) {
            return <PermissionCard key={item.key} permission={item.permission} agentKind={agentKind} onResolve={handlePermission} />
          }
          if (item.question) {
            return <NativeQuestionCard key={item.key} question={item.question} askedBy={agentToolName(agentKind)} onAnswer={handleQuestion} onDismiss={handleQuestion} />
          }
          return null
          })}
          {terminalAsking && terminalPrompt && (
            <NativeTerminalPrompt
              prompt={terminalPrompt}
              askedBy={agentToolName(agentKind)}
              onChoose={handleTerminalChoice}
              onOpenTerminal={onDowngradeChimera ? () => { void onDowngradeChimera().catch(() => {}) } : undefined}
            />
          )}
        </div>

        {/* Scroll to bottom button */}
        {isScrolledUp && (
          <button
            onClick={scrollToBottom}
            className="absolute bottom-3 right-3 z-10 rounded-full bg-zinc-800 border border-zinc-700 p-2 text-zinc-400 shadow-lg transition-colors hover:bg-zinc-700 hover:text-zinc-200 active:bg-zinc-600"
            title="Scroll to bottom"
          >
            <ArrowDown size={18} />
          </button>
        )}
      </div>

      {/* Status bar */}
      <div className="flex items-center gap-3 border-t border-zinc-800 px-4 py-1.5 text-xs text-zinc-500">
        {/* Below md the agent picker above the panel already names the tool. */}
        <AgentKindBadge kind={agentKind} isNative className="hidden md:inline-flex" />
        {awaitingPerson ? (
          // The request is the newest thing in the conversation, which may
          // be scrolled well away from it.
          <button onClick={scrollToBottom} className="flex items-center gap-1.5 text-amber-300 hover:text-amber-200">
            <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-amber-300" />
            Waiting for you
          </button>
        ) : busy ? (
          <span className="flex items-center gap-1.5 text-green-400">
            <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-green-400" />
            Working
          </span>
        ) : (
          <span className="text-zinc-500">Idle</span>
        )}
        {mode && (
          <span className="rounded bg-zinc-800 px-1.5 py-0.5">{mode}</span>
        )}
        {syncing && <span className="text-indigo-400">Syncing…</span>}
        {contextUsage && (() => {
          const pct = contextUsage.used / contextUsage.window
          const pctRounded = Math.round(pct * 100)
          const color = pct < 0.5 ? "bg-green-500" : pct < 0.75 ? "bg-amber-500" : "bg-red-500"
          return (
            <div className="relative flex h-4 w-28 shrink-0 items-center overflow-hidden rounded bg-zinc-800">
              <div className={`absolute inset-y-0 left-0 ${color} opacity-80`} style={{ width: `${pctRounded}%` }} />
              <span className="relative z-10 w-full text-center text-[10px] text-zinc-200">
                {formatTokens(contextUsage.used)} / {formatTokens(contextUsage.window)} ({pctRounded}%)
              </span>
            </div>
          )
        })()}
        {error && <span className="text-red-400">{error}</span>}
        {/* Action buttons */}
        <div className="ml-auto flex items-center gap-1.5 shrink-0">
          {/* Model selector */}
          {models.length > 0 && (
            <Dropdown
              open={modelsOpen}
              onOpenChange={setModelsOpen}
              minWidth={220}
              className="inline-block"
              trigger={
                <button
                  onClick={() => setModelsOpen((v) => !v)}
                  className={"flex items-center gap-1 rounded px-2 py-1 text-xs border transition-colors " + (modelsOpen ? "text-zinc-200 bg-zinc-800 border-zinc-600" : "text-zinc-400 border-zinc-700 hover:bg-zinc-800 hover:text-zinc-200")}
                  title="Change model"
                >
                  <Cpu size={12} />
                  <span className="font-mono hidden sm:inline">{model || "model"}</span>
                  {effort !== undefined && <span className="hidden text-zinc-500 sm:inline">{String(effort)}</span>}
                </button>
              }
            >
              {models.map((m) => (
                <DropdownItem key={m.ref} onClick={() => handleModelChange(m.ref)} active={m.ref === model}>
                  <span className="font-mono">{m.ref}</span>
                  {m.isDefault && <span className="rounded bg-indigo-900/60 px-1 text-[10px] text-indigo-300">default</span>}
                  {m.vision && <span className="text-indigo-400" title="vision">👁</span>}
                  {m.contextWindow != null && <span className="ml-auto text-zinc-500">{formatTokens(m.contextWindow)}</span>}
                </DropdownItem>
              ))}
            </Dropdown>
          )}
          {/* Mode selector */}
          {model && <NativeModelSettings key={`${agentId}:${model}`} agentId={agentId} baseUrl={baseUrl} model={model} busy={busy}
            onSaved={(settingsState) => replaceSyncState({ ...syncStateRef.current, model: settingsState.model, settingsState })} />}
          {modes.length > 0 && (
            <div className="relative hidden sm:block" ref={modesRef}>
              <button
                onClick={() => setModesOpen((v) => !v)}
                className="flex items-center gap-1 rounded px-2 py-1 text-xs text-zinc-400 border border-zinc-700 transition-colors hover:bg-zinc-800 hover:text-zinc-200"
                title="Change mode"
              >
                <ArrowLeftRight size={12} />
                <span>Mode</span>
              </button>
              {modesOpen && (
                <div className="absolute bottom-full right-0 mb-1 rounded border border-zinc-700 bg-zinc-900 shadow-xl py-1 min-w-[160px] z-10">
                  {modes.map((m) => (
                    <button
                      key={m.name}
                      onClick={() => handleModeChange(m.name)}
                      className={"flex w-full min-w-0 items-start gap-2 px-3 py-1.5 text-xs transition-colors " + (m.name === mode ? "text-zinc-200 bg-zinc-800" : "text-zinc-300 hover:bg-zinc-800")}
                    >
                      <Check size={12} className={m.name === mode ? "mt-0.5 shrink-0 text-zinc-400" : "mt-0.5 shrink-0 opacity-0"} />
                      <div className="min-w-0 flex-1 text-left">
                        <div className="font-mono">{m.name}</div>
                        {m.description && <div className="text-zinc-500 truncate">{m.description}</div>}
                      </div>
                    </button>
                  ))}
                </div>
              )}
            </div>
          )}
          {/* Macros dropdown */}
          <div className="relative" ref={macrosRef}>
            <button
              onClick={() => setMacrosOpen((v) => !v)}
              className={"flex items-center gap-1 rounded px-2 py-1 text-xs border transition-colors " + (macrosOpen ? "text-zinc-200 bg-zinc-800 border-zinc-600" : "text-zinc-400 border-zinc-700 hover:bg-zinc-800 hover:text-zinc-200")}
              title="Macros (M)"
              aria-keyshortcuts="m"
            >
              <Zap size={12} />
              <span className="hidden sm:inline">Macros</span>
            </button>
            {macrosOpen && (
              <div className="absolute bottom-full right-0 mb-1 max-w-[calc(100vw-2rem)] rounded border border-zinc-700 bg-zinc-900 shadow-xl py-1 min-w-[180px] z-10">
                {macros.length === 0 ? (
                  <div className="px-3 py-1.5 text-xs text-zinc-500">(no macros configured)</div>
                ) : (
                  macros.map((macro, index) => {
                    const shortcut = index < 9 ? String(index + 1) : undefined
                    return (
                      <button
                        key={macro.name}
                        onClick={() => handleMacro(macro.keys)}
                        aria-keyshortcuts={shortcut}
                        className="flex w-full items-center gap-2 px-3 py-1.5 text-xs text-zinc-300 hover:bg-zinc-800 transition-colors"
                      >
                        {shortcut && <kbd className="w-4 rounded border border-zinc-700 text-center font-mono text-[10px] text-zinc-500">{shortcut}</kbd>}
                        <Zap size={12} />
                        {macro.name}
                      </button>
                    )
                  })
                )}
              </div>
            )}
          </div>
          {/* Overflow menu */}
          <div className="relative" ref={menuRef}>
            <button
              onClick={() => setMenuOpen((v) => !v)}
              className={"flex items-center gap-1 rounded px-2 py-1 text-xs border transition-colors " + (menuOpen ? "text-zinc-200 bg-zinc-800 border-zinc-600" : "text-zinc-400 border-zinc-700 hover:bg-zinc-800 hover:text-zinc-200")}
              title="More actions"
            >
              <MoreHorizontal size={12} />
            </button>
            {menuOpen && (
              <div className="absolute bottom-full right-0 mb-1 max-h-[60vh] max-w-[calc(100vw-2rem)] overflow-y-auto rounded border border-zinc-700 bg-zinc-900 shadow-xl py-1 min-w-[180px] z-10">
                {modes.length > 0 && (
                  <div className="sm:hidden">
                    {modes.map((m) => (
                      <button
                        key={m.name}
                        onClick={() => { handleModeChange(m.name); setMenuOpen(false) }}
                        className={"flex w-full min-w-0 items-start gap-2 px-3 py-1.5 text-xs transition-colors " + (m.name === mode ? "text-zinc-200 bg-zinc-800" : "text-zinc-300 hover:bg-zinc-800")}
                      >
                        <Check size={12} className={m.name === mode ? "mt-0.5 shrink-0 text-zinc-400" : "mt-0.5 shrink-0 opacity-0"} />
                        <div className="min-w-0 flex-1 text-left">
                          <div className="font-mono">{m.name}</div>
                          {m.description && <div className="text-zinc-500 truncate">{m.description}</div>}
                        </div>
                      </button>
                    ))}
                    <div className="my-1 border-t border-zinc-800" />
                  </div>
                )}
                <div className="sm:hidden">
                  {macros.length === 0 ? (
                    <div className="flex items-center gap-2 px-3 py-1.5 text-xs text-zinc-500">
                      <Zap size={12} />
                      No macros
                    </div>
                  ) : (
                    macros.map((macro, index) => {
                      const shortcut = index < 9 ? String(index + 1) : undefined
                      return (
                        <button
                          key={macro.name}
                          onClick={() => { handleMacro(macro.keys); setMenuOpen(false) }}
                          aria-keyshortcuts={shortcut}
                          className="flex w-full items-center gap-2 px-3 py-1.5 text-xs text-zinc-300 hover:bg-zinc-800 transition-colors"
                        >
                          {shortcut && <kbd className="w-4 rounded border border-zinc-700 text-center font-mono text-[10px] text-zinc-500">{shortcut}</kbd>}
                          <Zap size={12} />
                          {macro.name}
                        </button>
                      )
                    })
                  )}
                  <div className="my-1 border-t border-zinc-800" />
                </div>
                <button
                  onClick={() => { setMenuOpen(false); void resync() }}
                  disabled={loading}
                  className="flex w-full items-center gap-2 px-3 py-1.5 text-xs text-zinc-300 hover:bg-zinc-800 transition-colors disabled:opacity-50"
                >
                  <RefreshCw size={12} className={loading ? "animate-spin" : ""} />
                  {loading ? "Refreshing…" : "Refresh from snapshot"}
                </button>
                {ntfyConfigured && onToggleNotify && (
                  <button
                    onClick={() => { onToggleNotify(); setMenuOpen(false) }}
                    className="flex w-full items-center gap-2 px-3 py-1.5 text-xs text-zinc-300 hover:bg-zinc-800 transition-colors"
                  >
                    {notifyEnabled ? <Bell size={12} /> : <BellOff size={12} />}
                    {notifyEnabled ? "Disable notify" : "Enable notify"}
                  </button>
                )}
                {onDowngradeChimera && (
                  <button
                    onClick={() => {
                      setMenuOpen(false)
                      void onDowngradeChimera().catch((reason) => {
                        replaceSyncState({ ...syncStateRef.current, error: reason instanceof Error ? reason.message : String(reason) })
                      })
                    }}
                    className="flex w-full items-center gap-2 px-3 py-1.5 text-xs text-zinc-300 hover:bg-zinc-800 transition-colors"
                  >
                    <Terminal size={12} />
                    Downgrade to tmux
                  </button>
                )}
                {onDisconnect && (
                  <button
                    onClick={() => { onDisconnect(); setMenuOpen(false) }}
                    className="flex w-full items-center gap-2 px-3 py-1.5 text-xs text-zinc-300 hover:bg-zinc-800 transition-colors"
                  >
                    <Unplug size={12} />
                    Disconnect
                  </button>
                )}
                {onKill && (
                  <>
                    <div className="my-1 border-t border-zinc-800" />
                    <button
                      onClick={() => { onKill(); setMenuOpen(false) }}
                      className="flex w-full items-center gap-2 px-3 py-1.5 text-xs text-red-400 hover:bg-red-950 transition-colors"
                    >
                      <Skull size={12} />
                      Kill agent
                    </button>
                  </>
                )}
              </div>
            )}
          </div>
        </div>
      </div>

      {/* Composer */}
      {composerEnabled && <div className="relative border-t border-zinc-800 p-3">
        {slashOpen && (
          <div className="absolute bottom-full left-3 right-3 mb-1 max-h-48 overflow-y-auto rounded-lg border border-zinc-700 bg-zinc-900 shadow-xl z-20">
            {slashSuggestions.map((s, i) => (
              <button
                key={s.type + ":" + s.name}
                onMouseDown={(e) => { e.preventDefault(); handleSlashSelect(s) }}
                onMouseEnter={() => setSlashIndex(i)}
                className={"flex w-full items-center gap-2 px-3 py-2 text-left text-sm transition-colors " + (i === slashIndex ? "bg-zinc-800 text-zinc-100" : "text-zinc-300 hover:bg-zinc-800")}
              >
                <span className={"shrink-0 rounded px-1 text-[10px] font-mono uppercase " + (s.type === "command" ? "bg-indigo-900/60 text-indigo-300" : "bg-amber-900/60 text-amber-300")}>
                  {s.type === "command" ? "cmd" : "skill"}
                </span>
                <span className="font-mono text-zinc-200">{s.name}</span>
                {s.description && <span className="text-zinc-500 truncate">{s.description}</span>}
              </button>
            ))}
          </div>
        )}
        {bangQueue.length > 0 && (
          <div className="mb-2 space-y-1">
            {bangQueue.map((cmd, i) => (
              <div key={i} className="flex items-center gap-2 rounded bg-zinc-800/50 px-2 py-1 text-xs text-zinc-400">
                <span>Queued: !{cmd}</span>
                <button
                  onClick={() => setBangQueue((prev) => prev.filter((_, j) => j !== i))}
                  className="ml-auto text-zinc-600 hover:text-red-400"
                >
                  ×
                </button>
              </div>
            ))}
          </div>
        )}
        <div data-testid="agent-composer" className="flex items-stretch gap-2">
          <div className="relative min-w-0 flex-1">
            {isBang && (
              <div className="absolute left-2.5 top-1.5 flex items-center gap-1 text-amber-400 pointer-events-none">
                <Terminal size={13} />
                <span className="text-[10px] font-semibold uppercase tracking-wide">bash</span>
              </div>
            )}
            <textarea
              ref={textareaRef}
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              onKeyDown={handleKeyDown}
              placeholder={busy ? "Queue a message..." : "Send a message..."}
              rows={2}
              className={`h-full min-h-24 w-full resize-none rounded-lg border bg-zinc-900 px-3 py-2 text-sm text-zinc-200 placeholder:text-zinc-600 focus:outline-none disabled:opacity-50 ${isBang ? "border-amber-500/60 pt-7 focus:border-amber-400" : "border-zinc-700 focus:border-zinc-500"}`}
            />
          </div>
          <div data-testid="composer-actions" className="grid w-24 shrink-0 grid-rows-2 gap-2">
            <Button
              size="sm"
              onClick={handleSend}
              disabled={!draft.trim() || bangRunning}
              className="h-full min-h-11 w-full"
            >
              {bangRunning ? "Running…" : isBang ? "Run" : busy ? "Queue" : "Send"}
            </Button>
            {busy ? (
              <Button variant="destructive" size="sm" onClick={handleInterrupt} className="h-full min-h-11 w-full">
                Interrupt
              </Button>
            ) : voiceEnabled ? (
              <VoiceRecorder
                ref={voiceRef}
                agentId={agentId}
                agentKind={agentKind}
                baseUrl={baseUrl}
                transcriptionOnly
                onTranscription={(text) => {
                  setDraft((prev) => prev ? prev + " " + text : text)
                }}
              />
            ) : <div aria-hidden="true" />}
          </div>
        </div>
      </div>}
    </div>
  )
}

const SubagentCard = memo(function SubagentCard({ tool }: { tool: ToolCallItem }) {
  const [expanded, setExpanded] = useState(true)
  const args = tool.args || {}
  const result = tool.result || {}
  const subagent = tool.subagent
  const purpose = subagent?.purpose || String(args.purpose || "Subagent")
  const prompt = String(args.prompt || "")
  const agent = String(args.agent || "default")
  const model = String(args.model || "inherited")
  const mode = String(args.mode || "default")
  const status = subagent?.status || (result.reason ? (result.reason === "stop" ? "finished" : "error") : "spawning")
  const steps = result.steps as number | undefined
  const toolCalls = result.tool_calls_count as number | undefined

  return (
    <div data-testid={`subagent-card-${tool.key}`} className="mx-4 my-2 overflow-hidden rounded-lg border border-indigo-700/60 bg-indigo-950/20">
      <button className="flex w-full items-center gap-2 px-3 py-2 text-left" onClick={() => setExpanded(!expanded)}>
        <span className="rounded bg-indigo-600/70 px-1.5 py-0.5 font-mono text-xs text-white">subagent</span>
        <span className="truncate text-sm font-medium text-indigo-100">{purpose}</span>
        <span className={`ml-auto text-xs ${status === "error" ? "text-red-400" : status === "finished" ? "text-green-400" : "text-indigo-300"}`}>{status}</span>
        <span className="text-xs text-zinc-500">{expanded ? "▼" : "▶"}</span>
      </button>
      {expanded && (
        <div className="border-t border-indigo-900/60 px-3 py-2">
          <div className="mb-2 flex flex-wrap gap-1.5 text-[11px]">
            <span className="rounded bg-zinc-800 px-1.5 py-0.5 text-zinc-300">agent <span className="font-mono text-zinc-100">{agent}</span></span>
            <span className="rounded bg-zinc-800 px-1.5 py-0.5 text-zinc-300">model <span className="font-mono text-zinc-100">{model}</span></span>
            <span className="rounded bg-zinc-800 px-1.5 py-0.5 text-zinc-300">mode <span className="font-mono text-zinc-100">{mode}</span></span>
          </div>
          {prompt && <div className="mb-2 whitespace-pre-wrap rounded bg-zinc-900/70 px-2 py-1.5 text-xs text-zinc-300">{prompt}</div>}
          {subagent?.activities.length ? (
            <div className="space-y-1 border-l border-indigo-700/50 pl-3">
              {subagent.activities.map((activity) => <div key={activity.key} className="text-xs text-zinc-300">{activity.text}</div>)}
            </div>
          ) : (
            <div className="text-xs text-zinc-500">
              {status === "spawning" || status === "running" ? "Waiting for activity…" : "No live activity captured"}
            </div>
          )}
          {(steps !== undefined || toolCalls !== undefined) && (
            <div className="mt-2 text-[11px] text-zinc-500">
              {steps !== undefined && `${steps} step${steps === 1 ? "" : "s"}`}
              {steps !== undefined && toolCalls !== undefined && " · "}
              {toolCalls !== undefined && `${toolCalls} tool call${toolCalls === 1 ? "" : "s"}`}
            </div>
          )}
        </div>
      )}
    </div>
  )
})

// One file of a "patch" tool call: what changed in it, as a unified diff, or
// its whole content when the file is new or gone.
interface PatchChange {
  path: string
  kind: "add" | "delete" | "update" | string
  diff: string
  movePath?: string
}

// The lines of a change as a diff reads: a new file is all additions and a
// deleted one all removals.
function patchLines(change: PatchChange): string[] {
  const lines = change.diff.replace(/\n$/, "").split("\n")
  if (change.kind === "add") return lines.map((line) => `+${line}`)
  if (change.kind === "delete") return lines.map((line) => `-${line}`)
  return lines
}

function patchLineClass(line: string): string {
  if (line.startsWith("@@")) return "text-sky-300"
  if (line.startsWith("+")) return "bg-green-900/30 text-green-300"
  if (line.startsWith("-")) return "bg-red-900/30 text-red-300"
  return "text-zinc-400"
}

type ConversationBlock =
  | { kind: "item"; item: NativeConversationItem }
  | { kind: "tools"; key: string; tools: ToolCallItem[] }

// Tool calls made back to back are drawn as one block. A subagent spawn keeps
// its own card, so it also ends the run before it.
function conversationBlocks(items: NativeConversationItem[]): ConversationBlock[] {
  const blocks: ConversationBlock[] = []
  for (const item of items) {
    if (item.role === "tool" && item.tool && item.tool.toolName !== "spawn_agent") {
      const last = blocks[blocks.length - 1]
      if (last?.kind === "tools") last.tools.push(item.tool)
      else blocks.push({ kind: "tools", key: item.key, tools: [item.tool] })
      continue
    }
    blocks.push({ kind: "item", item })
  }
  return blocks
}

// How many calls of a long run stay in view; the ones before them fold behind a count.
const VISIBLE_TOOL_CALLS = 5

// "3 read · 2 bash": the tools some calls used, the most used first.
function toolCounts(tools: ToolCallItem[]): string {
  const counts = new Map<string, number>()
  for (const tool of tools) counts.set(tool.toolName, (counts.get(tool.toolName) ?? 0) + 1)
  return [...counts].sort((a, b) => b[1] - a[1]).map(([name, count]) => `${count} ${name}`).join(" · ")
}

const ToolCallGroup = memo(function ToolCallGroup({ tools }: { tools: ToolCallItem[] }) {
  const [showEarlier, setShowEarlier] = useState(false)
  const grouped = tools.length > 1
  // A lone read stays a bare line, as light as it was before grouping.
  const bare = !grouped && tools[0].toolName === "read"
  // The count takes a line of its own, so it must stand for at least two calls.
  const earlier = tools.length > VISIBLE_TOOL_CALLS + 1 ? tools.length - VISIBLE_TOOL_CALLS : 0
  const visible = earlier > 0 && !showEarlier ? tools.slice(-VISIBLE_TOOL_CALLS) : tools

  return (
    <div
      role={grouped ? "group" : undefined}
      aria-label={grouped ? `${tools.length} tool calls` : undefined}
      className={bare ? "mx-4 my-1" : "mx-4 my-1.5 divide-y divide-zinc-800 overflow-hidden rounded-lg border border-zinc-700 bg-zinc-900/50"}
    >
      {earlier > 0 && (
        <button
          aria-expanded={showEarlier}
          className="flex w-full items-center gap-2 px-3 py-1 text-left text-xs text-zinc-500 transition-colors hover:text-zinc-300"
          onClick={() => setShowEarlier(!showEarlier)}
        >
          <span className="shrink-0">{earlier} earlier tool calls</span>
          <span className="min-w-0 truncate text-zinc-600">{toolCounts(tools.slice(0, earlier))}</span>
          <span className="ml-auto shrink-0">{showEarlier ? "▼" : "▶"}</span>
        </button>
      )}
      {visible.map((tool) => <ToolCallCard key={tool.key} tool={tool} grouped={grouped} />)}
    </div>
  )
}, (prev, next) => prev.tools.length === next.tools.length && prev.tools.every((tool, index) => tool === next.tools[index]))

// One row of a ToolCallGroup, which draws the border around it.
const ToolCallCard = memo(function ToolCallCard({ tool, grouped }: { tool: ToolCallItem; grouped: boolean }) {
  // An edit opens its diff unasked only when it stands alone; in a group every
  // call starts as one line. A click overrides either default.
  const [override, setExpanded] = useState<boolean | null>(null)
  const expanded = override ?? ((tool.toolName === "edit" || tool.toolName === "patch") && !grouped)

  const name = tool.toolName
  const headerClass = `flex w-full items-center gap-2 px-3 text-left ${grouped ? "py-1" : "py-1.5"}`
  const args = tool.args || {}
  const summary = tool.summary || ""
  const result = tool.result || {}
  const durationMs = tool.durationMs

  // For edit tool calls, render a diff
  if (name === "edit") {
    const path = args.path as string || ""
    const oldStr = args.old_string as string || ""
    const newStr = args.new_string as string || ""
    const replacements = result.replacements as number | undefined
    const filename = path.split("/").pop() || path

    return (
      <div>
        <button
          className={headerClass}
          onClick={() => setExpanded(!expanded)}
        >
          <span className="rounded bg-purple-700/60 px-1.5 py-0.5 font-mono text-xs text-white">edit</span>
          <span className="text-xs text-zinc-300 font-mono truncate">{filename}</span>
          {replacements !== undefined && (
            <span className="shrink-0 text-xs text-zinc-500">({replacements} replacement{replacements !== 1 ? "s" : ""})</span>
          )}
          {durationMs !== undefined && <span className="text-xs text-zinc-600">{durationMs}ms</span>}
          <span className="ml-auto text-xs text-zinc-500">{expanded ? "▼" : "▶"}</span>
        </button>
        {expanded && (
          <div className="border-t border-zinc-800 px-3 py-2">
            {path !== filename && <div className="text-xs text-zinc-500 mb-1">{path}</div>}
            <div className="font-mono text-xs whitespace-pre">
              {oldStr.split("\n").map((line, i) => (
                <div key={`old-${i}`} className="bg-red-900/30 text-red-300 px-2">
                  - {line}
                </div>
              ))}
              {newStr.split("\n").map((line, i) => (
                <div key={`new-${i}`} className="bg-green-900/30 text-green-300 px-2">
                  + {line}
                </div>
              ))}
            </div>
          </div>
        )}
      </div>
    )
  }

  // A change to one or more files, as the diff of each
  if (name === "patch") {
    const changes = (Array.isArray(args.changes) ? args.changes : []) as PatchChange[]
    const failure = result.error as string | undefined
    return (
      <div>
        <button
          className={headerClass}
          onClick={() => setExpanded(!expanded)}
        >
          <span className="rounded bg-purple-700/60 px-1.5 py-0.5 font-mono text-xs text-white">patch</span>
          <span className="text-xs text-zinc-300 font-mono truncate">{summary || changes.map((change) => change.path.split("/").pop()).join(", ")}</span>
          {failure && <span className="shrink-0 text-xs text-red-400">not applied</span>}
          {durationMs !== undefined && <span className="text-xs text-zinc-600">{durationMs}ms</span>}
          <span className="ml-auto text-xs text-zinc-500">{expanded ? "▼" : "▶"}</span>
        </button>
        {expanded && (
          <div className="space-y-2 border-t border-zinc-800 px-3 py-2">
            {failure && <div className="text-xs text-red-400">{failure}</div>}
            {changes.map((change) => (
              <div key={change.path}>
                <div className="mb-1 flex min-w-0 flex-wrap gap-x-2 text-xs text-zinc-500">
                  <span className="uppercase tracking-wide">{change.kind}</span>
                  <span className="min-w-0 break-all">{change.path}</span>
                  {change.movePath && <span className="min-w-0 break-all">→ {change.movePath}</span>}
                </div>
                <div className="font-mono text-xs whitespace-pre overflow-x-auto">
                  {patchLines(change).map((line, i) => (
                    <div key={i} className={`px-2 ${patchLineClass(line)}`}>{line}</div>
                  ))}
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    )
  }

  // For write tool calls, show the path and content preview
  if (name === "write") {
    const path = args.path as string || ""
    const content = args.content as string || ""
    const bytesWritten = result.bytes_written as number | undefined
    const created = result.created as boolean | undefined
    const filename = path.split("/").pop() || path

    return (
      <div>
        <button
          className={headerClass}
          onClick={() => setExpanded(!expanded)}
        >
          <span className="rounded bg-green-700/60 px-1.5 py-0.5 font-mono text-xs text-white">write</span>
          <span className="text-xs text-zinc-300 font-mono truncate">{filename}</span>
          {created && <span className="text-xs text-green-400">created</span>}
          {bytesWritten !== undefined && <span className="text-xs text-zinc-500">{bytesWritten}B</span>}
          {durationMs !== undefined && <span className="text-xs text-zinc-600">{durationMs}ms</span>}
          <span className="ml-auto text-xs text-zinc-500">{expanded ? "▼" : "▶"}</span>
        </button>
        {expanded && (
          <div className="border-t border-zinc-800 px-3 py-2">
            {path !== filename && <div className="text-xs text-zinc-500 mb-1">{path}</div>}
            <pre className="font-mono text-xs text-zinc-300 whitespace-pre-wrap max-h-60 overflow-y-auto">{content}</pre>
          </div>
        )}
      </div>
    )
  }

  // For bash tool calls, show the command and output
  if (name === "bash") {
    const command = args.command as string || ""
    const stdout = result.stdout as string || ""
    const stderr = result.stderr as string || ""
    const exitCode = result.exit_code as number | undefined

    return (
      <div>
        <button
          className={headerClass}
          onClick={() => setExpanded(!expanded)}
        >
          <span className="rounded bg-zinc-600 px-1.5 py-0.5 font-mono text-xs text-white">bash</span>
          <span className="text-xs text-zinc-300 font-mono truncate">{command.slice(0, 80)}{command.length > 80 ? "…" : ""}</span>
          {exitCode !== undefined && (
            <span className={`shrink-0 text-xs ${exitCode === 0 ? "text-green-500" : "text-red-400"}`}>exit {exitCode}</span>
          )}
          {durationMs !== undefined && <span className="text-xs text-zinc-600">{durationMs}ms</span>}
          <span className="ml-auto text-xs text-zinc-500">{expanded ? "▼" : "▶"}</span>
        </button>
        {expanded && (
          <div className="border-t border-zinc-800 px-3 py-2">
            <div className="font-mono text-xs text-zinc-500 mb-1">$ {command}</div>
            {stdout && <pre className="font-mono text-xs text-zinc-300 whitespace-pre-wrap max-h-60 overflow-y-auto">{stdout}</pre>}
            {stderr && <pre className="font-mono text-xs text-red-400 whitespace-pre-wrap max-h-40 overflow-y-auto">{stderr}</pre>}
          </div>
        )}
      </div>
    )
  }

  // For read tool calls, show the path
  if (name === "read") {
    const path = args.path as string || ""
    const filename = path.split("/").pop() || path
    return (
      <div className={`text-xs text-zinc-400 ${grouped ? "px-3 py-1" : "px-1 py-0.5"}`}>
        <div className="flex items-center gap-2">
          <span className="rounded bg-blue-700/40 px-1.5 py-0.5 font-mono text-white">read</span>
          <span className="font-mono truncate">{filename}</span>
        </div>
        {tool.skillName && (
          <div className="ml-12 mt-0.5 text-indigo-300">📚 skill: {tool.skillName}</div>
        )}
      </div>
    )
  }

  // Default: compact tool card
  return (
    <div>
      <button
        className={headerClass}
        onClick={() => setExpanded(!expanded)}
      >
        <span className="rounded bg-zinc-600 px-1.5 py-0.5 font-mono text-xs text-white">{name}</span>
        <span className="text-xs text-zinc-300 truncate">{summary}</span>
        {durationMs !== undefined && <span className="text-xs text-zinc-600">{durationMs}ms</span>}
        <span className="ml-auto text-xs text-zinc-500">{expanded ? "▼" : "▶"}</span>
      </button>
      {expanded && (
        <div className="border-t border-zinc-800 px-3 py-2">
          {Object.keys(args).length > 0 && (
            <>
              <div className="text-xs text-zinc-500 mb-1">Args</div>
              <pre className="font-mono text-xs text-zinc-300 whitespace-pre-wrap max-h-40 overflow-y-auto">{JSON.stringify(args, null, 2)}</pre>
            </>
          )}
          {Object.keys(result).length > 0 && (
            <>
              <div className="text-xs text-zinc-500 mt-2 mb-1">Result</div>
              <pre className="font-mono text-xs text-zinc-300 whitespace-pre-wrap max-h-40 overflow-y-auto">{JSON.stringify(result, null, 2)}</pre>
            </>
          )}
        </div>
      )}
    </div>
  )
})
