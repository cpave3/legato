import { ReeveStatus, reeveWash } from "./ReeveEntry"
import { AskReeve } from "./AskReeve"
import { parseReeveMessage, reeveBrief } from "../lib/reeve"
import { useState, useEffect, useCallback, useRef, useMemo, memo, Fragment, type ReactNode } from "react"
import { ArrowDown, ArrowLeftRight, Bell, Check, Terminal, Zap } from "lucide-react"
import type { SandboxSwitch } from "../lib/agentReload"
import { useWebSocket } from "../hooks/useWebSocket"
import { type PromptState, type WSMessage } from "../lib/agentWire"
import { useServer } from "../hooks/useServer"
import { useMacroHotkeys } from "../hooks/useMacroHotkeys"
import { useMacroPicker } from "../hooks/useMacroPicker"
import { MacroPickerItems } from "./macros/MacroPicker"
import { macroMessage } from "../lib/macros"
import { useAgentReselected } from "../hooks/useAgentSelection"
import { apiFetch } from "../lib/api"
import { dispatchAgentInput } from "../lib/agentInput"
import { useVoiceRecorder } from "../hooks/useVoiceRecorder"
import { ReviewMessageBody } from "./ReviewMessageBody"
import { ChimeraRecovery } from "./ChimeraRecovery"
import { NativeModelSettingsForm } from "./NativeModelSettings"
import { ComposerModelMenu } from "./ComposerModelMenu"
import { NativeQuestionCard } from "./approvals/NativeQuestionCard"
import { NativeTerminalPrompt } from "./NativeTerminalPrompt"
import { NativeSlashMenu } from "./NativeSlashMenu"
import { AgentUICallCard } from "./AgentUICallCard"
import { ToolCallCard } from "./NativeToolCallCard"
import { SubagentCard } from "./NativeSubagentCard"
import { PastedImageStrip } from "./PastedImageStrip"
import { AttachImageButton } from "./AttachImageButton"
import { AgentChatHeader, type AgentChatState, type SidePanelsProps } from "./AgentChatHeader"
import { buildAgentMenu } from "./nativeAgentMenu"
import { agentChatState, isBangText, isChimeraKind, isRecoveryReply, reasonText } from "../lib/nativeAgentView"
import type { AgentStageInfo } from "./AgentStageChip"
import { ResourcesButton } from "./ResourcesButton"
import { ContextGauge, ContextNotice, DictateButton, DictationBar, QueuedList, SendButton, WorkingStrip, type QueuedEntry } from "./Composer"
import { CONTEXT_NEARLY_FULL, composerToolButton, currentStep, dictating, formatElapsed, isCommandText } from "../lib/composer"
import { useTurnClock } from "../hooks/useTurnClock"
import { usePageInView } from "../hooks/usePageInView"
import { legatoUICall, forOperatorCall, speakFailed, speakText } from "../lib/agentUi"
import { SaidBubble } from "./SaidBubble"
import { usePastedImages } from "../hooks/usePastedImages"
import { nativeToolName } from "../lib/agentKind"
import { compactAgent, compactCommand, compactionNotice, nextCompaction, type Compaction } from "../lib/compaction"
import { toolDisplayName } from "../lib/mcpToolName"
import { legatoEventSummary, parseLegatoMessage } from "../lib/legatoMessage"
import { plainText, subagentTree, type SubagentNode } from "../lib/agentGlance"
import {
  createNativeSyncState,
  reconcileNativeSnapshot,
  reduceNativeEvent,
  subagentStatus,
  updatePermissionState,
  updateQuestionState,
  type NativeConversationItem,
  type NativeEvent as ChimeraEvent,
  type NativePermissionItem,
  type NativeSnapshot as SessionSnapshot,
  type NativeSyncState,
  type NativeToolItem as ToolCallItem,
} from "../lib/nativeAgentSync"
import { LoadingState } from "./ui/LoadingState"
import { TileDeck, type DeckTile } from "./tiles/TileDeck"
import { SubagentLink } from "./tiles/SubagentParts"
import { saveSubagentView, savedSubagentView, subagentTiles, type SubagentView } from "./tiles/subagentTiles"
import { shellTileForCall, shellTiles } from "./tiles/shellTiles"
import { fromStored, mergeShells, type NativeBackgroundShell, type StoredBackgroundShell } from "../lib/backgroundShells"
import type { BackgroundJob } from "../lib/agentWire"
import { useElementWidth } from "../hooks/useElementWidth"
import { scrollAnchorOf, useConversationScroll } from "./useConversationScroll"
import { useDraft } from "./useDraft"
import { useAgentCatalog } from "./useAgentCatalog"
import { useSlashMenu } from "./useSlashMenu"
import { useComposerKeys } from "./useComposerKeys"
import { useDropdown } from "./useDropdown"
import { useSessionSettings } from "./useSessionSettings"
import { useBangCommands } from "./useBangCommands"
import { useQueuedMessages } from "./useQueuedMessages"

// The least width at which subagent tiles sit beside the conversation; below
// it they are a strip above it.
const tilesBesideWidth = 1000

function formatTokens(value: number): string {
  if (value < 1_000) return value.toLocaleString()
  if (value < 1_000_000) return `${Number((value / 1_000).toFixed(1))}k`
  return `${Number((value / 1_000_000).toFixed(2))}M`
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

// How the request_id of a message from the box starts, so Legato's answer
// about it is told from one about a permission or a question.
const MESSAGE_REQUEST = "message-"
// How many of the commands it sent a panel remembers, to know a refusal of one.
const SENT_COMMANDS_KEPT = 20

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
    <section aria-label={`Permission request for ${toolDisplayName(permission.tool)}`} className="mx-4 my-2 min-w-0 rounded-lg border border-yellow-600/40 bg-yellow-900/10 p-3">
      <div className="flex min-w-0 flex-wrap items-center gap-2">
        <div className="text-sm font-semibold text-yellow-200">{isPlan ? "Plan approval" : !agentKind || agentKind === "chimera" ? "Host command approval" : "Tool approval"}</div>
        <span title={permission.tool} className="rounded bg-yellow-950/70 px-1.5 py-0.5 font-mono text-[11px] text-yellow-300">{toolDisplayName(permission.tool)} · {permission.target}</span>
        {resolvedLabel && <span className="ml-auto text-xs font-medium text-zinc-300">{resolvedLabel}</span>}
      </div>
      {permission.reason && <p className="mt-2 text-xs text-zinc-300">{permission.reason}</p>}
      {permission.plan ? (
        <div className="mt-2 max-h-96 overflow-auto rounded bg-[var(--theme-canvas,rgba(0,0,0,0.3))] p-2 text-sm text-zinc-100">
          <ReviewMessageBody body={permission.plan} />
        </div>
      ) : (
        <pre className="mt-2 max-h-48 overflow-auto whitespace-pre-wrap break-words rounded bg-[var(--theme-canvas,rgba(0,0,0,0.3))] p-2 font-mono text-xs text-zinc-100">{permission.command}</pre>
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
              <button key={option.id} disabled={submitting} className="rounded bg-[var(--theme-selection,color-mix(in_oklab,var(--color-green-700)_60%,transparent))] px-2 py-1 text-left text-xs text-[var(--theme-text,white)] hover:bg-[var(--theme-selection,color-mix(in_oklab,var(--color-green-600)_60%,transparent))] disabled:cursor-wait disabled:opacity-50" onClick={() => onResolve(permission.requestId, "allow", undefined, option.id)}>{option.label}</button>
            )) : (
              <button disabled={submitting} className="rounded bg-[var(--theme-selection,color-mix(in_oklab,var(--color-green-700)_60%,transparent))] px-2 py-1 text-xs text-[var(--theme-text,white)] hover:bg-[var(--theme-selection,color-mix(in_oklab,var(--color-green-600)_60%,transparent))] disabled:cursor-wait disabled:opacity-50" onClick={() => onResolve(permission.requestId, "allow")}>Allow</button>
            )}
            <button disabled={submitting} className="rounded bg-[var(--theme-selection,color-mix(in_oklab,var(--color-red-700)_60%,transparent))] px-2 py-1 text-xs text-[var(--theme-text,white)] hover:bg-[var(--theme-selection,color-mix(in_oklab,var(--color-red-600)_60%,transparent))] disabled:cursor-wait disabled:opacity-50" onClick={() => onResolve(permission.requestId, "deny")}>{isPlan ? "Reject" : "Deny"}</button>
            <button disabled={submitting} className="rounded border border-red-700/70 px-2 py-1 text-xs text-red-200 hover:bg-red-950/50 disabled:cursor-wait disabled:opacity-50" onClick={() => setFeedbackOpen(true)}>{isPlan ? "Request changes" : "Deny with feedback"}</button>
            {submitting && <span role="status" className="self-center text-xs text-yellow-300">Submitting…</span>}
          </div>
          {feedbackOpen && (
            <div className="space-y-2">
              <label className="block text-xs text-zinc-300" htmlFor={`permission-feedback-${permission.requestId}`}>Feedback for {agentToolName(agentKind)}</label>
              <textarea id={`permission-feedback-${permission.requestId}`} value={feedback} disabled={submitting} onChange={(event) => setFeedback(event.target.value)} className="min-h-20 w-full resize-y rounded border border-zinc-700 bg-zinc-950 p-2 text-sm text-zinc-200 focus:border-red-500 focus:outline-none" />
              <div className="flex flex-wrap gap-2">
                <button disabled={submitting || !feedback.trim()} className="rounded bg-[var(--theme-selection,color-mix(in_oklab,var(--color-red-700)_60%,transparent))] px-2 py-1 text-xs text-[var(--theme-text,white)] disabled:opacity-40" onClick={() => onResolve(permission.requestId, "deny", feedback.trim())}>Send denial</button>
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
  // Where the agent's work stands, for its header; none for the Reeve.
  stage?: AgentStageInfo
  // The agent of this Legato itself: its status bar says so.
  reeve?: boolean
  // The agent runs in Legato's sandbox: giving it a resource reloads it.
  sandboxed?: boolean
  onClickTerminal?: () => void
  onKill?: () => void
  // Names the agent: opens the rename dialog.
  onRename?: () => void
  // Opens the dialog that puts the agent in a group.
  onMoveToGroup?: () => void
  // Opens the dialog that shows what Legato knows about the agent.
  onShowInfo?: () => void
  // Set to this panel's refresh from the snapshot, for a menu outside it.
  resyncRef?: { current: (() => void) | null }
  // Stops the agent and starts it again, resuming its conversation.
  onReload?: () => void
  // Reloads it on the other side of the sandbox, where there is one.
  switchSandbox?: SandboxSwitch
  // Reloads it with or without the host network for its containers.
  switchHostNetwork?: SandboxSwitch
  onDisconnect?: () => void
  onDowngradeChimera?: () => Promise<void>
  ntfyConfigured?: boolean
  notifyEnabled?: boolean
  onToggleNotify?: () => void
  voiceEnabled?: boolean
  agentKind?: string
  // The engram the agent runs as, shown beside its tool.
  engram?: string
  // The agent's open operator items, which the operator's next message
  // closes.
  openItems?: number
  composerEnabled?: boolean
  // Open something the agent showed (an artifact, or a card at full size)
  // in the side panel.
  onOpenUIItem?: (itemId: string, revision: number) => void
  // What the column beside the chat can show, switched from the header.
  panels?: SidePanelsProps
  // The task the agent works on, what it is doing as its hooks report it
  // (error, background), and how long it has been doing it.
  title?: string
  activity?: string
  stateTime?: string
  // The pull request of the agent's task.
  pullRequest?: number
  // The background shells Legato recorded from the agent's mod (Chimera's,
  // without output); its endpoint's own, when it reports them, win.
  backgroundShells?: StoredBackgroundShell[]
  // What the agent runs in the background, for its header's sub-agents.
  backgroundJobs?: BackgroundJob[]
}

export function NativeAgentPanel({ agentId, reeve, sandboxed, onKill, onRename, onMoveToGroup, onShowInfo, resyncRef, onReload, switchSandbox, switchHostNetwork, onDisconnect, onDowngradeChimera, ntfyConfigured, notifyEnabled, onToggleNotify, voiceEnabled, agentKind, engram, openItems = 0, composerEnabled = true, onOpenUIItem, panels, title, activity, stateTime, pullRequest, backgroundShells, backgroundJobs, stage }: NativeAgentPanelProps) {
  const { send, subscribe, connected } = useWebSocket()
  // Out of view the panel stops following the agent and catches up from its
  // cursor on return, so a phone with its screen off does not keep receiving
  // the conversation.
  const inView = usePageInView()
  const following = connected && inView
  const { baseUrl } = useServer()
  const [syncState, setSyncState] = useState<NativeSyncState>(() => createNativeSyncState())
  const syncStateRef = useRef(syncState)
  const modelRequestRef = useRef(0)
  const invalidateModelRequests = useCallback(() => { modelRequestRef.current++ }, [])
  const queuedEventsRef = useRef<ChimeraEvent[] | null>(null)
  // True while the panel shows Working only because the person sent a message
  // to an idle session, before the session says a turn started. A message
  // that was not sent then takes it back: nothing is running (LEG-151).
  const workingUnconfirmedRef = useRef(false)
  // Commands this panel sent, by their words, whose refusal it would show.
  // Legato's stream replays the refusals it kept to a panel that opens; those
  // are not this panel's to show.
  const sentCommandsRef = useRef<string[]>([])
  const syncGenerationRef = useRef(0)
  const runFinishTimerRef = useRef<number | null>(null)
  const hydratedAgentRef = useRef<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [syncing, setSyncing] = useState(false)
  const [mode, setMode] = useState<string>("")
  const scroll = useConversationScroll(loading, syncState.items)
  const { draft, setDraft, textareaRef, appendWords, insertImage, focusBox } = useDraft(agentId, composerEnabled)
  const { modes, models, macros, catalogKey, catalog, slashItems, retryCatalog } = useAgentCatalog(agentId, baseUrl)
  const { scrollRef, isScrolledUp, scrollToBottom, scrollToBottomAfterRender, restoreAfterSync, followLatest, restartInitialScroll } = scroll
  const { items, busy, error, contextUsage, model } = syncState
  const blocks = useMemo(() => conversationBlocks(items), [items])
  const shells = useMemo(() => mergeShells(syncState.backgroundShells, fromStored(backgroundShells)), [syncState.backgroundShells, backgroundShells])
  const tiles = useMemo(() => [...subagentTiles(items, backgroundJobs), ...shellTiles(shells)], [items, shells, backgroundJobs])
  const subagentNodes = useMemo(() => subagentTree(backgroundJobs), [backgroundJobs])
  const deckTitle = shells.length > 0 ? "Sub-agents and shells" : "Sub-agents"
  const [subagentView, setSubagentView] = useState<SubagentView>(savedSubagentView)
  const [focusedTile, setFocusedTile] = useState<string | null>(null)
  const [highlightedTile, setHighlightedTile] = useState<string | null>(null)
  const panelRef = useRef<HTMLDivElement>(null)
  const panelWidth = useElementWidth(panelRef)
  const showTiles = subagentView === "tiles" && tiles.length > 0
  const tilesBeside = panelWidth >= tilesBesideWidth
  const chooseSubagentView = useCallback((view: SubagentView) => {
    setSubagentView(view)
    saveSubagentView(view)
    setFocusedTile(null)
  }, [])
  const step = useMemo(() => busy ? currentStep(items) : null, [busy, items])
  const turnSeconds = useTurnClock(busy, !loading)
  // The agent has stopped on a permission prompt or a question of its own.
  const awaitingRequest = items.some((item) => (item.permission ?? item.question)?.status === "pending")
  // What the agent's terminal is asking, as read off its screen. A request
  // the agent raised itself is drawn by its own card; the terminal shows the
  // same request as a dialog, which is not shown twice.
  const [terminalPrompt, setTerminalPrompt] = useState<PromptState | null>(null)
  const terminalAsking = !awaitingRequest && terminalPrompt !== null && terminalPromptTypes.has(terminalPrompt.type) && (terminalPrompt.actions?.length ?? 0) > 0
  const awaitingPerson = awaitingRequest || terminalAsking
  const modesMenu = useDropdown()
  const macrosMenu = useDropdown()
  const closeMacros = macrosMenu.close
  // Dictation, transcribed into the message.
  const voice = useVoiceRecorder({
    agentId, agentKind, baseUrl, transcriptionOnly: true,
    onTranscription: appendWords,
  })
  const pasted = usePastedImages(agentId, draft, insertImage)
  const slash = useSlashMenu(draft, setDraft, textareaRef, slashItems, catalogKey)
  const { open: slashOpen, matches: slashMatches, index: slashIndex, setIndex: setSlashIndex, listId: slashListId, optionId: slashOptionId, select: handleSlashSelect } = slash

  const replaceSyncState = useCallback((next: NativeSyncState) => {
    syncStateRef.current = next
    setSyncState(next)
  }, [])

  // The message that showed Working was not sent and no turn has started:
  // the session is idle.
  const stopUnconfirmedWorking = useCallback(() => {
    if (!workingUnconfirmedRef.current) return
    workingUnconfirmedRef.current = false
    replaceSyncState({ ...syncStateRef.current, busy: false })
  }, [replaceSyncState])

  const applyEvent = useCallback((event: ChimeraEvent) => {
    if (event.type === "model_changed" || event.type === "mode_changed") modelRequestRef.current++
    if (queuedEventsRef.current) {
      queuedEventsRef.current.push(event)
      return
    }
    replaceSyncState(reduceNativeEvent(syncStateRef.current, event))
  }, [replaceSyncState])

  // Shows why something failed above the conversation.
  const failWith = useCallback((reason: unknown) => {
    replaceSyncState({ ...syncStateRef.current, error: reasonText(reason) })
  }, [replaceSyncState])
  const { handleModeChange, handleModelChange, handleEffortChange, efforts, effort } = useSessionSettings({
    agentId, baseUrl, syncState, syncStateRef, replaceSyncState, modelRequestRef, setMode, closeModes: modesMenu.close,
  })




  const resync = useCallback(async (scrollAfterSync = false, initialHydration = false) => {
    const generation = ++syncGenerationRef.current
    const eventQueue = queuedEventsRef.current ?? []
    const viewport = scrollRef.current
    const scrollAnchor = scrollAnchorOf(viewport)
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
      // The session says a turn runs: Working is no longer only the panel's guess.
      if (snapshot.status === "running" || snapshot.status === "waiting_for_permission" || snapshot.status === "waiting_for_input") workingUnconfirmedRef.current = false
      replaceSyncState(reconcileNativeSnapshot(snapshot, syncStateRef.current, eventQueue))
      restoreAfterSync(scrollAnchor, scrollAfterSync)
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
  }, [agentId, baseUrl, replaceSyncState, scrollRef, restoreAfterSync])
  useEffect(() => {
    if (!resyncRef) return
    resyncRef.current = () => { void resync() }
    return () => { resyncRef.current = null }
  }, [resyncRef, resync])
  // Picking the agent that is already open asks for a fresh view of it.
  useAgentReselected(agentId, resync)

  useEffect(() => {
    replaceSyncState(createNativeSyncState())
    invalidateModelRequests()
    restartInitialScroll()
    hydratedAgentRef.current = null
    queuedEventsRef.current = []
    return () => {
      invalidateModelRequests()
      queuedEventsRef.current = null
      hydratedAgentRef.current = null
      if (runFinishTimerRef.current != null) window.clearTimeout(runFinishTimerRef.current)
    }
  }, [agentId, replaceSyncState, invalidateModelRequests, restartInitialScroll])

  const bang = useBangCommands({ agentId, baseUrl, busy, resync, failWith, scrollToBottomAfterRender, setDraft, textareaRef })
  const { runOrQueue: runOrQueueBang, runNext: runNextBang } = bang
  const isBang = isBangText(draft.trim())
  const queue = useQueuedMessages(items, busy, setDraft, focusBox)
  const { enqueue, settle: settleMessage, failByWords } = queue
  // Why the last message could not leave the page; it stays in the box.
  const [sendError, setSendError] = useState<string | null>(null)
  // The agent's latest compaction, as its events tell it.
  const [compaction, setCompaction] = useState<Compaction>()
  const queuedEntries: QueuedEntry[] = [...queue.entries, ...bang.entries]

  useEffect(() => {
    const unsubscribe = subscribe((msg: WSMessage) => {
      if (msg.type === "agents_changed" && following && syncStateRef.current.items.some(item => item.tool?.toolName === "spawn_agent" && ["spawning", "running"].includes(subagentStatus(item.tool)))) {
        if (runFinishTimerRef.current == null) {
          runFinishTimerRef.current = window.setTimeout(() => {
            runFinishTimerRef.current = null
            void resync()
          }, 100)
        }
        return
      }
      if (msg.agent_id !== agentId) return
      if (msg.type === "prompt_state") {
        setTerminalPrompt(msg.prompt ?? null)
        return
      }
      if (msg.type === "agent_message_sent" && msg.request_id) {
        settleMessage(msg.request_id)
        return
      }
      if (msg.type === "error" && msg.request_id?.startsWith(MESSAGE_REQUEST)) {
        settleMessage(msg.request_id!, msg.error || "The message could not be sent")
        stopUnconfirmedWorking()
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
        if (event.type === "message_failed") {
          const words = event.content ?? ""
          const reason = event.error || "The agent's session would not take it"
          // A command is not listed, so its failure has no row to mark.
          if (isCommandText(words)) {
            const at = sentCommandsRef.current.indexOf(words.trim())
            if (at < 0) return
            sentCommandsRef.current.splice(at, 1)
            setSendError(`Not sent: ${words.trim()}: ${reason}`)
          } else {
            failByWords(words, reason)
          }
          stopUnconfirmedWorking()
          return
        }
        if (event.type === "session_started" || event.type === "user_message") workingUnconfirmedRef.current = false
        setCompaction((current) => nextCompaction(current, event))
        applyEvent(event)
        if (event.type === "run_finished" || ((!agentKind || agentKind === "chimera") && event.type === "compaction_finished")) {
          if (runFinishTimerRef.current != null) window.clearTimeout(runFinishTimerRef.current)
          runFinishTimerRef.current = window.setTimeout(() => {
            runFinishTimerRef.current = null
            void resync()
          }, 100)
        }
        if (event.type === "run_finished") {
          runNextBang()
        }
        followLatest()
      } catch {
        replaceSyncState({ ...syncStateRef.current, error: "Invalid native event; refresh from snapshot" })
      }
    })
    if (following) {
      send({ type: "subscribe_agent", agent_id: agentId, cursor: syncStateRef.current.cursor })
      if (hydratedAgentRef.current !== agentId) {
        hydratedAgentRef.current = agentId
        void resync(true, true)
      }
    }
    return () => {
      unsubscribe()
      if (following) send({ type: "unsubscribe_agent", agent_id: agentId })
    }
  }, [subscribe, agentId, agentKind, applyEvent, replaceSyncState, resync, runNextBang, following, send, stopUnconfirmedWorking, followLatest, settleMessage, failByWords])

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
    if (isBangText(trimmed)) {
      setDraft("")
      runOrQueueBang(trimmed.slice(1))
      return
    }
    // /compact is the agent's own compaction, never a message to it.
    const compact = compactCommand(trimmed)
    if (compact) {
      setDraft("")
      setSendError(null)
      compactAgent(baseUrl, agentId, compact.instructions).catch((reason) => {
        setSendError(reason instanceof Error ? reason.message : String(reason))
      })
      return
    }
    // Intercept built-in slash commands that need API calls, not message sends
    const builtinCommands = ["/model", "/mode"]
    if (builtinCommands.includes(trimmed.split(/\s+/)[0])) {
      setDraft("")
      void handleBuiltinSlash(trimmed)
      return
    }
    const requestId = `${MESSAGE_REQUEST}${crypto.randomUUID()}`
    if (send({ type: "agent_message", agent_id: agentId, content: trimmed, queue: busy, request_id: requestId }) === false) {
      setSendError("Not connected to Legato. Your message is still in the box: send it again once connected.")
      return
    }
    setSendError(null)
    // A command the agent runs is never shown as a message, so it is not
    // listed; it is only answered, and an error for it shows as one. A path
    // at the start (/tmp/a.png what is this?) is a message, not a command.
    if (!isCommandText(trimmed)) enqueue(trimmed, syncStateRef.current.items, requestId)
    else sentCommandsRef.current = [...sentCommandsRef.current.slice(-(SENT_COMMANDS_KEPT - 1)), trimmed]
    setDraft("")
    if (!busy) {
      workingUnconfirmedRef.current = true
      replaceSyncState({ ...syncStateRef.current, busy: true, error: null })
    }
    scrollToBottomAfterRender()
  }, [draft, busy, agentId, baseUrl, send, replaceSyncState, scrollToBottomAfterRender, runOrQueueBang, handleBuiltinSlash, setDraft, enqueue])

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


  const handleKeyDown = useComposerKeys(slash, busy, handleSend, handleInterrupt)

  const handleMacro = useCallback((keys: string) => {
    dispatchAgentInput(macroMessage(keys), {
      executeBang: runOrQueueBang,
      sendMessage: (content) => send({ type: "agent_message", agent_id: agentId, content }),
    })
    closeMacros()
  }, [agentId, runOrQueueBang, send, closeMacros])

  const macroPicker = useMacroPicker(macros, macrosMenu.open)
  useMacroHotkeys({ macros: macroPicker.shown, open: macrosMenu.open, onOpen: macrosMenu.show, onClose: closeMacros, onSelect: handleMacro })

  if (loading) {
    return (
      <LoadingState label={`Loading ${agentToolName(agentKind)} session…`} detail={agentId} />
    )
  }

  if (error && items.length === 0) {
    return <NoEndpoint agentKind={agentKind} error={error} />
  }

  const headerState: AgentChatState = agentChatState(awaitingPerson, activity, busy)
  const menu = buildAgentMenu({
    onRename, onMoveToGroup, onShowInfo,
    onRefresh: () => { void resync() }, refreshing: loading,
    ntfyConfigured, notifyEnabled, onToggleNotify,
    onDowngradeChimera,
    onDowngradeFailed: (reason) => replaceSyncState({ ...syncStateRef.current, error: reasonText(reason) }),
    onReload, switchSandbox, switchHostNetwork, onDisconnect, onKill,
  })
  const modelWindow = models.find((m) => m.ref === model)?.contextWindow ?? contextUsage?.window
  const contextShareOf = contextUsage ? Math.round((contextUsage.used / contextUsage.window) * 100) : null

  return (
    <div ref={panelRef} className="relative flex h-full min-w-0 w-full flex-1 flex-col overflow-hidden bg-lw-ground font-lw text-[13px] leading-5 text-lw-text">
      <AgentChatHeader
        state={headerState}
        time={stateTime}
        title={title || agentId}
        taskId={agentId}
        agentKind={agentKind}
        engram={engram}
        native
        pullRequest={pullRequest}
        panels={panels}
        backgroundJobs={backgroundJobs}
        subagents={{ total: tiles.length, running: tiles.filter((tile) => tile.state === "running").length, view: subagentView, onView: chooseSubagentView }}
        onShowRequest={scrollToBottom}
        ask={reeve ? undefined : <AskReeve brief={reeveBrief({ what: `the agent “${title || agentId}”, which waits for the operator`, ids: { task: agentId, "agent tool": agentKind }, error: error || undefined })} />}
        menu={menu}
        stage={stage}
        notes={<>
          {reeve && <ReeveStatus baseUrl={baseUrl} />}
          {mode && <span className="rounded bg-lw-raised px-1.5 font-lw-mono">{mode}</span>}
          {syncing && <span className="text-lw-link">Syncing…</span>}
          {error && <span className="truncate text-lw-error">{error}</span>}
        </>}
      />
      {showTiles && !tilesBeside && (
        <TileDeck title={deckTitle} tiles={tiles} layout="strip" focused={focusedTile} onFocus={setFocusedTile} />
      )}

      {/* The conversation and its composer; subagent tiles may sit beside them,
          in the other half and never more. */}
      <div className="flex min-h-0 flex-1">
      <div className={`flex min-h-0 min-w-0 flex-1 flex-col ${showTiles && tilesBeside ? "border-r border-lw-line" : ""}`}>
      {/* Conversation viewport owns both scrolling and its floating action. */}
      <ConversationFeed
        scrollRef={scrollRef}
        onScroll={scroll.onScroll}
        items={items}
        blocks={blocks}
        agentId={agentId}
        agentKind={agentKind}
        tiles={{ show: showTiles, all: tiles, shells, nodes: subagentNodes, highlighted: highlightedTile, onHighlight: setHighlightedTile, onFocus: setFocusedTile }}
        onOpenUIItem={onOpenUIItem}
        onPermission={handlePermission}
        onQuestion={handleQuestion}
        terminal={terminalAsking && terminalPrompt && (
            <NativeTerminalPrompt
              prompt={terminalPrompt}
              askedBy={agentToolName(agentKind)}
              onChoose={handleTerminalChoice}
              onOpenTerminal={onDowngradeChimera ? () => { void onDowngradeChimera().catch(() => {}) } : undefined}
            />
          )}
        isScrolledUp={isScrolledUp}
        onScrollToBottom={scrollToBottom}
      />

      {/* Composer: the message box, and under it everything that sets how
          the agent works (model, effort, mode, resources, macros) beside
          how full its context is and the buttons that send and interrupt. */}
      <div className="px-3 pb-3 md:px-6 md:pb-[18px]">
        {/* An image dropped anywhere on the composer joins the message. */}
        <div {...(composerEnabled ? pasted.dropTarget : {})} className={`native-agent-composer relative mx-auto flex w-full max-w-[820px] flex-col rounded-[10px] border bg-lw-surface max-sm:rounded-xl ${dictating(voice) ? "border-lw-danger-line" : pasted.dragging ? "border-lw-accent" : "border-lw-line-strong"}`}>
          {slashOpen && (
            <NativeSlashMenu
              anchorRef={textareaRef}
              onClose={slash.dismiss}
              loading={catalog.loading}
              errors={catalog.errors}
              empty={slashItems.length === 0}
              onRetry={retryCatalog}
              browser={slash.browser.open ? {
                query: slash.browser.query, onQuery: slash.browser.setQuery,
                filter: slash.browser.filter, onFilter: slash.browser.setFilter,
                onKeyDown: handleKeyDown,
                onClose: slash.escape,
              } : undefined}
              listId={slashListId}
              optionId={slashOptionId}
              matches={slashMatches}
              active={slashIndex}
              onHover={setSlashIndex}
              onPick={handleSlashSelect}
            />
          )}
          {composerEnabled && <button type="button" className="self-start min-h-11 px-3.5 text-sm text-lw-text-2 hover:text-lw-text focus-visible:outline-2 focus-visible:outline-lw-accent" aria-expanded={slashOpen} aria-controls={slashOpen ? slashListId : undefined}
            onClick={slash.toggleBrowser}>
            Commands and skills
          </button>}
          {contextUsage && contextShareOf !== null && contextShareOf >= CONTEXT_NEARLY_FULL && (
            <ContextNotice used={formatTokens(contextUsage.used)} window={formatTokens(contextUsage.window)} />
          )}
          {busy && <WorkingStrip elapsed={turnSeconds === null ? undefined : formatElapsed(turnSeconds)} step={step} onInterrupt={handleInterrupt} />}
          {compaction && <p role="status" className={`px-3.5 pt-2 text-xs ${compaction.status === "failed" ? "text-lw-danger" : "text-lw-text-2"}`}>{compactionNotice(compaction)}</p>}
          {sendError && <p role="alert" className="px-3.5 pt-2 text-xs text-lw-danger">{sendError}</p>}
          <QueuedList entries={queuedEntries} />
          <PastedImageStrip pasted={pasted} />
          {composerEnabled && (
            <div data-testid="agent-composer" className="relative">
              {isBang && (
                <div className="pointer-events-none absolute left-3.5 top-2 flex items-center gap-1 text-lw-needs">
                  <Terminal size={13} />
                  <span className="text-[10px] font-semibold uppercase tracking-wide">bash</span>
                </div>
              )}
              <textarea
                ref={textareaRef}
                aria-label="Message"
                value={draft}
                onChange={(e) => setDraft(e.target.value)}
                onKeyDown={handleKeyDown}
                onPaste={pasted.onPaste}
                aria-autocomplete="list"
                aria-controls={slashOpen ? slashListId : undefined}
                aria-activedescendant={slashOpen && slashMatches.length > 0 ? slashOptionId(slashIndex) : undefined}
                placeholder={busy && queuedEntries.length > 0 ? "Queue another" : busy ? "Queue a message. Sent after the current step." : `Message ${agentId}`}
                rows={2}
                className={`block min-h-14 w-full resize-none border-0 bg-transparent px-3.5 pb-1 pt-3 text-sm text-lw-text placeholder:text-lw-faint focus:outline-none disabled:opacity-50 max-sm:text-base ${isBang ? "pt-8 shadow-[inset_0_0_0_1px] shadow-lw-needs/60" : ""}`}
              />
            </div>
          )}
          {composerEnabled && openItems > 0 && (
            <p className="px-3.5 pt-1 text-[11.5px] text-lw-text-2">Your next message to this agent closes its {openItems} {openItems === 1 ? "item" : "items"} in the rail.</p>
          )}
          {dictating(voice) ? <DictationBar recorder={voice} /> : (
          <div data-testid="composer-actions" className="flex min-w-0 items-center gap-1 px-2 pb-2 pt-1.5 text-xs text-lw-text-2 max-sm:gap-0.5 max-sm:px-1.5">
            <div className="flex min-w-0 flex-1 items-center gap-1 max-sm:gap-0.5">
            {composerEnabled && <AttachImageButton onFiles={pasted.attach} />}
            {models.length > 0 && (
              <ComposerModelMenu
                models={models}
                model={model ?? ""}
                window={modelWindow != null ? formatTokens(modelWindow) : undefined}
                effort={effort !== undefined ? String(effort) : undefined}
                efforts={efforts}
                onModel={handleModelChange}
                onEffort={handleEffortChange}
                effortLocked={busy ? "Wait for the current run to finish before changing the effort." : undefined}
                settings={model ? (close) => (
                  <NativeModelSettingsForm agentId={agentId} baseUrl={baseUrl} model={model} busy={busy} onClose={close}
                    onSaved={(settingsState) => replaceSyncState({ ...syncStateRef.current, model: settingsState.model, settingsState })} />
                ) : undefined}
              />
            )}
            {modes.length > 0 && (
              <div className="relative" ref={modesMenu.ref}>
                <button type="button" onClick={modesMenu.toggle} className={composerToolButton(modesMenu.open)} title="Change mode">
                  <ArrowLeftRight size={16} aria-hidden="true" className="sm:hidden" />
                  <span className="hidden sm:inline">Mode</span>
                  {mode && <span className="hidden font-lw-mono text-lw-faint sm:inline">{mode}</span>}
                </button>
                {modesMenu.open && (
                  <div className="absolute bottom-full left-0 z-10 mb-1 min-w-[160px] rounded-md border border-lw-line-strong bg-lw-surface py-1 shadow-xl">
                    {modes.map((m) => (
                      <button
                        key={m.name}
                        onClick={() => handleModeChange(m.name)}
                        className={"flex w-full min-w-0 items-start gap-2 px-3 py-1.5 text-xs transition-colors " + (m.name === mode ? "bg-lw-raised text-lw-text" : "text-lw-text hover:bg-lw-raised")}
                      >
                        <Check size={12} className={m.name === mode ? "mt-0.5 shrink-0 text-lw-text-2" : "mt-0.5 shrink-0 opacity-0"} />
                        <div className="min-w-0 flex-1 text-left">
                          <div className="font-mono">{m.name}</div>
                          {m.description && <div className="truncate text-lw-text-2">{m.description}</div>}
                        </div>
                      </button>
                    ))}
                  </div>
                )}
              </div>
            )}
            <ResourcesButton agentId={agentId} baseUrl={baseUrl} sandboxed={sandboxed} />
            <div className="relative" ref={macrosMenu.ref}>
              <button type="button" onClick={macrosMenu.toggle} className={composerToolButton(macrosMenu.open)} title="Macros (M)" aria-keyshortcuts="m">
                <Zap size={16} aria-hidden="true" className="sm:hidden" />
                <span className="hidden sm:inline">Macros</span>
              </button>
              {macrosMenu.open && (
                <div className="absolute bottom-full left-0 z-10 mb-1 min-w-[180px] max-w-[calc(100vw-2rem)] rounded-md border border-lw-line-strong bg-lw-surface py-1 shadow-xl">
                  <MacroPickerItems shown={macroPicker.shown} hidden={macroPicker.hidden} onShowAll={macroPicker.showAll} onPick={(macro) => handleMacro(macro.keys)} />
                </div>
              )}
            </div>
            </div>
            {contextUsage && contextShareOf !== null && (
              <ContextGauge share={contextShareOf} used={formatTokens(contextUsage.used)} window={formatTokens(contextUsage.window)} />
            )}
            {composerEnabled && voiceEnabled && <DictateButton recorder={voice} />}
            {composerEnabled && (
              <SendButton
                label={bang.running ? "Running…" : isBang ? "Run" : busy ? "Queue" : "Send"}
                queue={busy && !isBang}
                disabled={!draft.trim() || bang.running}
                onClick={handleSend}
              />
            )}
          </div>
          )}
        </div>
      </div>
      </div>
      {showTiles && tilesBeside && (
        <TileDeck
          title={deckTitle}
          tiles={tiles}
          layout="grid"
          focused={focusedTile}
          onFocus={setFocusedTile}
          highlighted={highlightedTile}
          onHighlight={setHighlightedTile}
        />
      )}
      </div>
    </div>
  )
}

// What the conversation feed needs to draw the tiles of subagents and
// shells beside their calls.
interface FeedTiles {
  show: boolean
  all: DeckTile[]
  shells: NativeBackgroundShell[]
  nodes: SubagentNode[]
  highlighted: string | null
  onHighlight: (id: string | null) => void
  onFocus: (id: string | null) => void
}

// What every row of the conversation is given besides its item.
interface FeedRowProps {
  agentId: string
  agentKind?: string
  tiles: FeedTiles
  onOpenUIItem?: (itemId: string, revision: number) => void
  onPermission: (requestId: string, decision: "allow" | "deny", feedback?: string, option?: string) => void
  onQuestion: (requestId: string, answers?: Record<string, string[]>) => void
}

// ConversationFeed is the conversation's viewport: the messages and steps,
// a prompt only the terminal shows, and the way back to the latest.
function ConversationFeed({ scrollRef, onScroll, items, blocks, terminal, isScrolledUp, onScrollToBottom, ...row }: FeedRowProps & {
  scrollRef: React.RefObject<HTMLDivElement | null>
  onScroll: (event: React.UIEvent<HTMLDivElement>) => void
  items: NativeConversationItem[]
  blocks: ConversationBlock[]
  terminal: ReactNode
  isScrolledUp: boolean
  onScrollToBottom: () => void
}) {
  return (
      <div role="region" aria-label="Conversation" className="relative min-h-0 flex-1">
        <div data-conversation-scroll ref={scrollRef} className="h-full overflow-y-auto overflow-x-hidden" onScroll={onScroll}>
          {items.length === 0 && (
          <div className="flex h-full items-center justify-center">
            <p className="text-sm text-lw-text-2">
              Session ready — send a message to start
            </p>
          </div>
        )}
        <div data-conversation-column className="mx-auto w-full max-w-[820px] py-3 md:px-2">
        {blocks.map((block) => block.kind === "steps"
          ? <StepsBlock key={block.key} block={block} agentId={row.agentId} tiles={row.tiles} />
          : <ConversationItemView key={block.item.key} item={block.item} {...row} />)}
          {terminal}
        </div>
        </div>

        {/* Scroll to bottom button */}
        {isScrolledUp && (
          <button
            onClick={onScrollToBottom}
            className="absolute bottom-3 right-3 z-10 rounded-full border border-lw-line-strong bg-lw-raised p-2 text-lw-text-2 shadow-lg transition-colors hover:text-lw-text"
            title="Scroll to bottom"
          >
            <ArrowDown size={18} />
          </button>
        )}
      </div>
  )
}

// A run of steps. A background shell's call stays in its group, with a way
// to its tile after it.
function StepsBlock({ block, agentId, tiles }: { block: Extract<ConversationBlock, { kind: "steps" }>; agentId: string; tiles: FeedTiles }) {
  const shellLinks = tiles.show ? block.rows.flatMap((row) => {
    const tile = row.tool ? shellTileForCall(tiles.all, tiles.shells, row.tool.key) : undefined
    return tile ? [tile] : []
  }) : []
  if (shellLinks.length === 0) return <StepGroup agentId={agentId} rows={block.rows} />
  return (
    <Fragment>
      <StepGroup agentId={agentId} rows={block.rows} />
      {shellLinks.map((tile) => (
        <SubagentLink key={tile.id} kind="shell" tile={tile} highlighted={tiles.highlighted === tile.id} onOpen={() => tiles.onFocus(tile.id)} onHighlight={tiles.onHighlight} />
      ))}
    </Fragment>
  )
}

// One item of the conversation: something said, or something the agent did
// or asks.
function ConversationItemView(props: FeedRowProps & { item: NativeConversationItem }) {
  return messageRow(props) ?? actionRow(props)
}

// A message: a reply, what the operator or Legato wrote, or a recovery
// excerpt Chimera keeps.
function messageRow({ item, agentKind }: FeedRowProps & { item: NativeConversationItem }): ReactNode {
  if (isRecoveryReply(item, agentKind)) return <ChimeraRecovery text={item.text} />
  if (item.role === "user") return <UserMessage text={item.text} />
  if (item.role === "assistant") return <AssistantMessage item={item} />
  return null
}

// Something the agent did or asks: a tool call standing alone, a shell
// command, a permission request or a question.
function actionRow({ item, agentKind, onPermission, onQuestion, ...row }: FeedRowProps & { item: NativeConversationItem }): ReactNode {
  // Every other tool call is in a "tools" block.
  if (item.role === "tool" && item.tool) return <ToolRow tool={item.tool} {...row} />
  if (item.role === "bang" && item.bang) return <BangResult bang={item.bang} />
  if (item.permission) return <PermissionCard permission={item.permission} agentKind={agentKind} onResolve={onPermission} />
  if (item.question) return <NativeQuestionCard question={item.question} askedBy={agentToolName(agentKind)} onAnswer={onQuestion} onDismiss={onQuestion} />
  return null
}

// A user turn. Legato's own messages arrive as user turns; they are drawn
// apart from what the person typed.
function UserMessage({ text }: { text: string }) {
  const peerReeve = parseReeveMessage(text)
  // Another instance's Reeve wrote this: its own block, with the Reeve's
  // violet edge.
  if (peerReeve) return <PeerReeveMessage message={peerReeve} />
  const legato = parseLegatoMessage(text)
  if (legato) return <LegatoMessage message={legato} />
  return (
    <div className="flex justify-end px-4 py-2">
      <div className="max-w-[min(520px,85%)] rounded-[10px_10px_2px_10px] border border-lw-line-strong bg-lw-selected px-3 py-2 text-[13px] text-lw-text whitespace-pre-wrap break-words">
        {text}
      </div>
    </div>
  )
}

function PeerReeveMessage({ message }: { message: NonNullable<ReturnType<typeof parseReeveMessage>> }) {
  return (
    <div className="flex justify-end px-4 py-2">
      <div
        aria-label={`Message from the Reeve on ${message.instance}`}
        className={`max-w-[75%] rounded-lg border-l-2 border-l-[var(--theme-keyword,#8b5cf6)] px-3 py-2 text-sm text-zinc-300 whitespace-pre-wrap break-words ${reeveWash}`}
      >
        <div className="mb-1 text-[10px] font-semibold uppercase tracking-wide text-[var(--theme-keyword,#c4b5fd)]">
          From the Reeve on <span className="normal-case">{message.instance}</span> · <span className="normal-case">{message.role}</span>
        </div>
        <div>{message.body}</div>
      </div>
    </div>
  )
}

// A message Legato wrote or carried.
function LegatoMessage({ message }: { message: NonNullable<ReturnType<typeof parseLegatoMessage>> }) {
  // Something that happened (a change set ran, Legato restarted): one line
  // among the steps, not a message anyone wrote.
  if (message.event) return <LegatoEventRow label={message.label} body={message.body} />
  // Another agent wrote this and Legato only carried it, so it has a look of
  // its own, labelled with who sent it.
  if (message.peer !== undefined) return <PeerAgentMessage peer={message.peer} body={message.body} />
  return (
    <div className="flex justify-end px-4 py-2">
      <div
        aria-label={`Message from Legato: ${message.label}`}
        className="max-w-[75%] rounded-lg border border-teal-800/60 bg-teal-950/30 px-3 py-2 text-sm text-zinc-300 whitespace-pre-wrap break-words"
      >
        <div className="mb-1 text-[10px] font-semibold uppercase tracking-wide text-teal-400">Legato · {message.label}</div>
        <div>{message.body}</div>
      </div>
    </div>
  )
}

function PeerAgentMessage({ peer, body }: { peer: string; body: string }) {
  return (
    <div className="flex justify-end px-4 py-2">
      <div
        aria-label={peer ? `Message from agent ${peer}` : "Message from another agent"}
        className="max-w-[75%] rounded-lg border border-fuchsia-800/60 bg-fuchsia-950/30 px-3 py-2 text-sm text-zinc-300 whitespace-pre-wrap break-words"
      >
        <div className="mb-1 text-[10px] font-semibold uppercase tracking-wide text-fuchsia-400">
          Agent{peer && <> · <span className="normal-case">{peer}</span></>}
        </div>
        <div>{body}</div>
      </div>
    </div>
  )
}

function AssistantMessage({ item }: { item: NativeConversationItem }) {
  return (
    <div className="flex justify-start px-4 py-2">
      <div className={`min-w-0 max-w-full text-[13px] leading-[21px] text-[var(--theme-text,#c9c8d0)] ${item.isStreaming ? "opacity-80" : ""}`}>
        <ReviewMessageBody body={item.text} />
        {item.isStreaming && <span className="ml-0.5 animate-pulse">▋</span>}
      </div>
    </div>
  )
}

// A tool call that stands alone: something the agent showed or said, a line
// for the operator's rail, or a subagent, as its tile's link or its card.
function ToolRow({ tool, agentId, tiles, onOpenUIItem }: Omit<FeedRowProps, "onPermission" | "onQuestion"> & { tool: ToolCallItem }) {
  const uiCall = shownUICall(tool)
  if (uiCall) return <AgentUICallCard call={uiCall} agentId={agentId} onOpen={onOpenUIItem} />
  const said = speakText(tool)
  if (said !== null) return <SaidBubble text={said} status={speakFailed(tool) ? "not_spoken" : undefined} />
  const posted = forOperatorCall(tool)
  if (posted) return <ForOperatorLine kind={posted.kind} title={posted.title} />
  const tile = tiles.show ? tiles.all.find((each) => each.id === tool.key) : undefined
  if (tile) {
    return <SubagentLink tile={tile} highlighted={tiles.highlighted === tile.id} onOpen={() => tiles.onFocus(tile.id)} onHighlight={tiles.onHighlight} />
  }

  return <SubagentCard tool={tool} nodes={tiles.nodes} />
}

// A shell command the operator ran from the message box, and what it printed.
function BangResult({ bang: b }: { bang: NonNullable<NativeConversationItem["bang"]> }) {
  const pending = b.exitCode === -1
  const success = b.exitCode === 0
  return (
    <div className={`mx-4 my-1.5 overflow-hidden rounded-lg border bg-zinc-900/50 ${bangBorder(pending, success)}`}>
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
      {!pending && <BangOutput stdout={b.stdout} stderr={b.stderr} />}
    </div>
  )
}

function bangBorder(pending: boolean, success: boolean): string {
  if (pending) return "border-amber-500/60"
  return success ? "border-amber-500/40" : "border-red-700/50"
}

function BangOutput({ stdout, stderr }: { stdout: string; stderr: string }) {
  if (!stdout && !stderr) return null
  return (
    <div className="border-t border-amber-900/30 px-3 py-2">
      {stdout && <pre className="font-mono text-xs text-zinc-300 whitespace-pre-wrap max-h-60 overflow-y-auto">{stdout}</pre>}
      {stderr && <pre className="font-mono text-xs text-red-400 whitespace-pre-wrap max-h-40 overflow-y-auto mt-1">{stderr}</pre>}
    </div>
  )
}

// Shown in place of the conversation when the panel found no session to
// speak to.
function NoEndpoint({ agentKind, error }: { agentKind?: string; error: string }) {
  return (
    <div className="flex h-full items-center justify-center">
      <div className="text-center">
        <p className="text-zinc-500 mb-2">{isChimeraKind(agentKind) ? "No Chimera endpoint discovered" : `No ${agentToolName(agentKind)} bridge connected`}</p>
        <p className="text-xs text-zinc-600">{error}</p>
      </div>
    </div>
  )
}

// A step is something the agent did on its way to a reply: a tool call, or a
// thought.
type ConversationBlock =
  | { kind: "item"; item: NativeConversationItem }
  | { kind: "steps"; key: string; rows: NativeConversationItem[] }

// shownUICall returns the call when a tool call asked Legato to show
// something and has not failed: that call is drawn as what it shows. A failed
// one stays an ordinary tool row, where its error can be read.
function shownUICall(tool: ToolCallItem) {
  const call = legatoUICall(tool)
  return call && call.state !== "failed" ? call : null
}

// Tool calls made back to back are drawn as one block, with what the agent
// thought between them. A subagent spawn keeps its own card, and so does a
// call that showed a card or an artifact or said something out loud, so each
// also ends the run before it.
function conversationBlocks(items: NativeConversationItem[]): ConversationBlock[] {
  const blocks: ConversationBlock[] = []
  for (const item of items) {
    const step = item.role === "reasoning"
      || (item.role === "tool" && item.tool && item.tool.toolName !== "spawn_agent" && !shownUICall(item.tool) && speakText(item.tool) === null && forOperatorCall(item.tool) === null)
    if (step) {
      const last = blocks[blocks.length - 1]
      if (last?.kind === "steps") last.rows.push(item)
      else blocks.push({ kind: "steps", key: item.key, rows: [item] })
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
  for (const tool of tools) {
    const name = toolDisplayName(tool.toolName)
    counts.set(name, (counts.get(name) ?? 0) + 1)
  }
  return [...counts].sort((a, b) => b[1] - a[1]).map(([name, count]) => `${count} ${name}`).join(" · ")
}

const StepGroup = memo(function StepGroup({ agentId, rows }: { agentId: string; rows: NativeConversationItem[] }) {
  const [showEarlier, setShowEarlier] = useState(false)
  const tools = rows.flatMap((row) => row.tool ? [row.tool] : [])
  // Several rows share one border. What a tool call does when it stands alone
  // depends on the other calls only: a thought beside it changes nothing.
  const boxed = rows.length > 1
  const grouped = tools.length > 1
  // A lone read or a lone thought stays a bare line.
  const bare = !boxed && (rows[0].role === "reasoning" || rows[0].tool?.toolName === "read")
  // The count takes a line of its own, so it must stand for at least two calls.
  const earlier = tools.length > VISIBLE_TOOL_CALLS + 1 ? tools.length - VISIBLE_TOOL_CALLS : 0
  let first = 0
  if (earlier > 0 && !showEarlier) {
    // The first call that stays in view, and the thoughts that led to it.
    first = rows.indexOf(rows.filter((row) => row.tool)[earlier])
    while (first > 0 && rows[first - 1].role === "reasoning") first--
  }
  const label = tools.length === 0 ? "Thinking" : tools.length === 1 ? "1 tool call" : `${tools.length} tool calls`

  return (
    <div
      role={boxed ? "group" : undefined}
      aria-label={boxed ? label : undefined}
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
      {rows.slice(first).map((row) => row.tool
        ? <ToolCallCard key={row.key} agentId={agentId} tool={row.tool} grouped={grouped} />
        : <ThinkingRow key={row.key} item={row} boxed={boxed} />)}
    </div>
  )
}, (prev, next) => prev.agentId === next.agentId && prev.rows.length === next.rows.length && prev.rows.every((row, index) => row === next.rows[index]))

// What the agent thought, as one quiet line that opens to all of it. While
// the thought is still arriving the line follows it; afterwards it shows how
// the thought began.
const ThinkingRow = memo(function ThinkingRow({ item, boxed }: { item: NativeConversationItem; boxed: boolean }) {
  const [expanded, setExpanded] = useState(false)
  const lines = plainText(item.text).split("\n")
  const line = item.isStreaming ? lines[lines.length - 1] : lines[0]

  return (
    <div>
      <button
        aria-expanded={expanded}
        className={`flex w-full items-center gap-2 text-left text-xs ${boxed ? "px-3 py-1" : "px-1 py-0.5"}`}
        onClick={() => setExpanded(!expanded)}
      >
        <span className="shrink-0 rounded bg-zinc-800 px-1.5 py-0.5 italic text-zinc-400">Thinking</span>
        <span className="min-w-0 truncate italic text-zinc-500">{line}</span>
        {item.isStreaming && <span aria-hidden="true" className="shrink-0 animate-pulse text-zinc-500">▋</span>}
        <span aria-hidden="true" className="ml-auto shrink-0 text-zinc-500">{expanded ? "▼" : "▶"}</span>
      </button>
      {expanded && (
        <div className={`text-xs text-zinc-400 ${boxed ? "border-t border-zinc-800 px-3 py-2" : "px-1 py-1"}`}>
          <ReviewMessageBody body={item.text} />
        </div>
      )}
    </div>
  )
})

// One row of a StepGroup, which draws the border around it.
// LegatoEventRow is an event Legato told the agent of, drawn as a tool call
// is: one line with a summary, which opens to the whole text.
function LegatoEventRow({ label, body }: { label: string; body: string }) {
  const [expanded, setExpanded] = useState(false)
  const summary = legatoEventSummary(label, body)
  const failed = /\b\d+ failed\b|denied|rolled back/.test(summary)
  return (
    <div className="px-4 py-1">
      <div className="rounded-md border border-zinc-800/80 bg-zinc-900/30">
        <button
          type="button"
          aria-expanded={expanded}
          className="flex w-full min-w-0 items-center gap-2 px-3 py-1 text-left"
          onClick={() => setExpanded(!expanded)}
        >
          <span className="shrink-0 rounded bg-teal-950/60 px-1.5 py-0.5 font-mono text-[10px] uppercase tracking-wide text-teal-400">Legato · {label}</span>
          <span className={`min-w-0 truncate font-mono text-xs ${failed ? "text-red-400" : "text-zinc-400"}`}>{summary}</span>
          <span className="ml-auto shrink-0 text-xs text-zinc-500">{expanded ? "▼" : "▶"}</span>
        </button>
        {expanded && (
          <div className="max-h-96 overflow-auto border-t border-zinc-800 px-3 py-2 font-mono text-xs text-zinc-300 whitespace-pre-wrap break-words">
            {body.trim()}
          </div>
        )}
      </div>
    </div>
  )
}

// ForOperatorLine is an agent's for_operator call, drawn as one line: what
// it put in the operator's rail.
function ForOperatorLine({ kind, title }: { kind: string; title: string }) {
  return (
    <div className="px-4 py-1.5">
      <div className="inline-flex max-w-full items-center gap-2 rounded-lg border border-lw-line bg-lw-surface px-2.5 py-1.5 text-xs text-lw-text-2">
        <Bell size={13} aria-hidden="true" className="shrink-0 text-lw-needs" />
        <span className="min-w-0 truncate"><b className="font-medium text-lw-text">for_operator</b>{kind ? ` · ${kind}` : ""} · {title}</span>
        <span className="shrink-0 text-lw-needs">→ in your rail</span>
      </div>
    </div>
  )
}
