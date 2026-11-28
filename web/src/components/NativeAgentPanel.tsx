import { ReeveStatus, reeveWash } from "./ReeveEntry"
import { AskReeve } from "./AskReeve"
import { parseReeveMessage, reeveBrief } from "../lib/reeve"
import { useState, useCallback, useMemo, memo, Fragment, type ReactNode } from "react"
import { ArrowDown, Bell, Terminal } from "lucide-react"
import type { SandboxSwitch } from "../lib/agentReload"
import { useWebSocket } from "../hooks/useWebSocket"
import { useServer } from "../hooks/useServer"
import { useMacroHotkeys } from "../hooks/useMacroHotkeys"
import { useMacroPicker } from "../hooks/useMacroPicker"
import { macroMessage } from "../lib/macros"
import { dispatchAgentInput } from "../lib/agentInput"
import { useVoiceRecorder } from "../hooks/useVoiceRecorder"
import { ReviewMessageBody } from "./ReviewMessageBody"
import { ChimeraRecovery } from "./ChimeraRecovery"
import { NativeQuestionCard } from "./approvals/NativeQuestionCard"
import { NativeTerminalPrompt } from "./NativeTerminalPrompt"
import { AgentUICallCard } from "./AgentUICallCard"
import { ToolCallCard } from "./NativeToolCallCard"
import { SubagentCard } from "./NativeSubagentCard"
import { AgentChatHeader, type AgentChatState, type SidePanelsProps } from "./AgentChatHeader"
import { buildAgentMenu } from "./nativeAgentMenu"
import { agentChatState, awaitsRequest, isBangText, isChimeraKind, isRecoveryReply, terminalAsks } from "../lib/nativeAgentView"
import type { AgentStageInfo } from "./AgentStageChip"
import type { QueuedEntry } from "./Composer"
import { currentStep } from "../lib/composer"
import { useTurnClock } from "../hooks/useTurnClock"
import { usePageInView } from "../hooks/usePageInView"
import { legatoUICall, forOperatorCall, speakFailed, speakText } from "../lib/agentUi"
import { SaidBubble } from "./SaidBubble"
import { usePastedImages } from "../hooks/usePastedImages"
import { nativeToolName } from "../lib/agentKind"
import { toolDisplayName } from "../lib/mcpToolName"
import { legatoEventSummary, parseLegatoMessage } from "../lib/legatoMessage"
import { plainText } from "../lib/agentGlance"
import {
  type NativeConversationItem,
  type NativePermissionItem,
  type NativeToolItem as ToolCallItem,
} from "../lib/nativeAgentSync"
import { LoadingState } from "./ui/LoadingState"
import { TileDeck } from "./tiles/TileDeck"
import { SubagentLink } from "./tiles/SubagentParts"
import { shellTileForCall } from "./tiles/shellTiles"
import type { StoredBackgroundShell } from "../lib/backgroundShells"
import type { BackgroundJob, PromptState } from "../lib/agentWire"
import { useConversationScroll } from "./useConversationScroll"
import { useDraft } from "./useDraft"
import { useAgentCatalog } from "./useAgentCatalog"
import { useSlashMenu } from "./useSlashMenu"
import { useComposerKeys } from "./useComposerKeys"
import { useDropdown } from "./useDropdown"
import { useSessionSettings } from "./useSessionSettings"
import { useBangCommands } from "./useBangCommands"
import { useQueuedMessages } from "./useQueuedMessages"
import { useTerminalPrompt } from "./useTerminalPrompt"
import { useRequestAnswers } from "./useRequestAnswers"
import { useMessageSending } from "./useMessageSending"
import { useNativeSyncState, useSnapshotResync } from "./useNativeAgentSync"
import { useNativeAgentFeed } from "./useNativeAgentFeed"
import { useSubagentTiles, type FeedTiles } from "./useSubagentTiles"
import { NativeComposer } from "./NativeComposer"

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
  const sync = useNativeSyncState()
  const { syncState, syncStateRef, replaceSyncState, failWith, modelRequestRef, workingUnconfirmedRef, loading, syncing, mode, setMode } = sync
  const scroll = useConversationScroll(loading, syncState.items)
  const { draft, setDraft, textareaRef, appendWords, insertImage, focusBox } = useDraft(agentId, composerEnabled)
  const { modes, models, macros, catalogKey, catalog, slashItems, retryCatalog } = useAgentCatalog(agentId, baseUrl)
  const { isScrolledUp, scrollToBottom, scrollToBottomAfterRender, followLatest, restartInitialScroll } = scroll
  const { items, busy, error, contextUsage, model } = syncState
  const blocks = useMemo(() => conversationBlocks(items), [items])
  const subagents = useSubagentTiles(items, syncState.backgroundShells, backgroundShells, backgroundJobs)
  const { panelRef, tiles } = subagents
  const step = useMemo(() => busy ? currentStep(items) : null, [busy, items])
  const turnSeconds = useTurnClock(busy, !loading)
  // The agent has stopped on a permission prompt or a question of its own.
  const awaitingRequest = awaitsRequest(items)
  // What the agent's terminal is asking, as read off its screen, and whether
  // the panel shows it.
  const terminal = useTerminalPrompt(agentId, connected, send)
  const terminalPrompt = terminal.prompt
  const terminalAsking = terminalAsks(terminalPrompt, awaitingRequest)
  const awaitingPerson = awaitingRequest || terminalAsking
  const { open: modesOpen, ref: modesRef, toggle: toggleModes, close: closeModes } = useDropdown()
  const { open: macrosOpen, ref: macrosRef, toggle: toggleMacros, show: openMacros, close: closeMacros } = useDropdown()
  // Dictation, transcribed into the message.
  const voice = useVoiceRecorder({
    agentId, agentKind, baseUrl, transcriptionOnly: true,
    onTranscription: appendWords,
  })
  const pasted = usePastedImages(agentId, draft, insertImage)
  const slash = useSlashMenu(draft, setDraft, textareaRef, slashItems, catalogKey)
  const { handleModeChange, handleModelChange, handleEffortChange, efforts, effort } = useSessionSettings({
    agentId, baseUrl, syncState, syncStateRef, replaceSyncState, modelRequestRef, setMode, closeModes,
  })
  const resync = useSnapshotResync(agentId, baseUrl, sync, scroll, resyncRef)

  const bang = useBangCommands({ agentId, baseUrl, busy, resync, failWith, scrollToBottomAfterRender, setDraft, textareaRef })
  const { runOrQueue: runOrQueueBang, runNext: runNextBang } = bang
  const isBang = isBangText(draft.trim())
  const queue = useQueuedMessages(items, busy, setDraft, focusBox)
  const { enqueue, settle, failByWords } = queue
  const { sendError, handleSend, handleInterrupt, commandRefused } = useMessageSending({
    agentId, baseUrl, send, busy, draft, setDraft, sync: { syncStateRef, replaceSyncState, workingUnconfirmedRef, failWith },
    enqueue, runOrQueueBang, scrollToBottomAfterRender,
  })
  const queuedEntries: QueuedEntry[] = [...queue.entries, ...bang.entries]

  const { compaction } = useNativeAgentFeed({
    agentId, agentKind, following, send, subscribe, sync, resync, restartInitialScroll, followLatest,
    onTerminalMessage: terminal.onMessage, settleMessage: settle, failByWords, commandRefused, runNextBang,
  })

  const { handlePermission, handleQuestion } = useRequestAnswers(agentId, send, { syncStateRef, replaceSyncState })
  const handleKeyDown = useComposerKeys(slash, busy, handleSend, handleInterrupt)

  const handleMacro = useCallback((keys: string) => {
    dispatchAgentInput(macroMessage(keys), {
      executeBang: runOrQueueBang,
      sendMessage: (content) => send({ type: "agent_message", agent_id: agentId, content }),
    })
    closeMacros()
  }, [agentId, runOrQueueBang, send, closeMacros])

  const macroPicker = useMacroPicker(macros, macrosOpen)
  useMacroHotkeys({ macros: macroPicker.shown, open: macrosOpen, onOpen: openMacros, onClose: closeMacros, onSelect: handleMacro })

  if (loading) {
    return (
      <LoadingState label={`Loading ${agentToolName(agentKind)} session…`} detail={agentId} />
    )
  }

  if (error && items.length === 0) {
    return <NoEndpoint agentKind={agentKind} error={error} />
  }

  const headerState: AgentChatState = agentChatState(awaitingPerson, activity, busy)
  const name = title || agentId
  const menu = buildAgentMenu({
    onRename, onMoveToGroup, onShowInfo,
    onRefresh: () => { void resync() }, refreshing: loading,
    ntfyConfigured, notifyEnabled, onToggleNotify,
    onDowngradeChimera,
    onDowngradeFailed: failWith,
    onReload, switchSandbox, switchHostNetwork, onDisconnect, onKill,
  })

  return (
    <div ref={panelRef} className="relative flex h-full min-w-0 w-full flex-1 flex-col overflow-hidden bg-lw-ground font-lw text-[13px] leading-5 text-lw-text">
      <AgentChatHeader
        state={headerState}
        time={stateTime}
        title={name}
        taskId={agentId}
        agentKind={agentKind}
        engram={engram}
        native
        pullRequest={pullRequest}
        panels={panels}
        backgroundJobs={backgroundJobs}
        subagents={{ total: tiles.length, running: tiles.filter((tile) => tile.state === "running").length, view: subagents.view, onView: subagents.chooseView }}
        onShowRequest={scrollToBottom}
        ask={askReeveAbout(reeve, name, agentId, agentKind, error)}
        menu={menu}
        stage={stage}
        notes={<HeaderNotes reeve={reeve} baseUrl={baseUrl} mode={mode} syncing={syncing} error={error} />}
      />
      <SubagentDeck subagents={subagents} beside={false} />

      {/* The conversation and its composer; subagent tiles may sit beside them,
          in the other half and never more. */}
      <div className="flex min-h-0 flex-1">
      <div className={`flex min-h-0 min-w-0 flex-1 flex-col ${subagents.show && subagents.beside ? "border-r border-lw-line" : ""}`}>
      {/* Conversation viewport owns both scrolling and its floating action. */}
      <ConversationFeed
        scrollRef={scroll.scrollRef}
        onScroll={scroll.onScroll}
        items={items}
        blocks={blocks}
        agentId={agentId}
        agentKind={agentKind}
        tiles={subagents.feed}
        onOpenUIItem={onOpenUIItem}
        onPermission={handlePermission}
        onQuestion={handleQuestion}
        terminal={<TerminalPromptCard asking={terminalAsking} prompt={terminalPrompt} agentKind={agentKind} onChoose={terminal.choose} pending={terminal.pending} notice={terminal.notice} onDowngradeChimera={onDowngradeChimera} />}
        isScrolledUp={isScrolledUp}
        onScrollToBottom={scrollToBottom}
      />

      <NativeComposer
        agentId={agentId}
        baseUrl={baseUrl}
        sandboxed={sandboxed}
        enabled={composerEnabled}
        voiceEnabled={voiceEnabled}
        openItems={openItems}
        busy={busy}
        turnSeconds={turnSeconds}
        step={step}
        contextUsage={contextUsage}
        compaction={compaction}
        sendError={sendError}
        queuedEntries={queuedEntries}
        draft={draft}
        setDraft={setDraft}
        textareaRef={textareaRef}
        isBang={isBang}
        bangRunning={bang.running}
        pasted={pasted}
        voice={voice}
        slash={slash}
        catalog={{ loading: catalog.loading, errors: catalog.errors, empty: slashItems.length === 0, onRetry: retryCatalog }}
        onKeyDown={handleKeyDown}
        onSend={handleSend}
        onInterrupt={handleInterrupt}
        model={{
          agentId, baseUrl, busy, models, model, contextUsage, effort, efforts,
          onModel: handleModelChange, onEffort: handleEffortChange,
          onSaved: (settingsState) => replaceSyncState({ ...syncStateRef.current, model: settingsState.model, settingsState }),
        }}
        modes={{ modes, mode, open: modesOpen, onToggle: toggleModes, onChange: handleModeChange }}
        modesRef={modesRef}
        macros={{ open: macrosOpen, onToggle: toggleMacros, shown: macroPicker.shown, hidden: macroPicker.hidden, onShowAll: macroPicker.showAll, onPick: handleMacro }}
        macrosRef={macrosRef}
      />
      </div>
      <SubagentDeck subagents={subagents} beside />
      </div>
    </div>
  )
}


// Notes in the header: the Reeve's status in its own panel, the mode, a
// refresh running, and an error.
function HeaderNotes({ reeve, baseUrl, mode, syncing, error }: { reeve?: boolean; baseUrl: string; mode: string; syncing: boolean; error: string | null }) {
  return (
    <>
          {reeve && <ReeveStatus baseUrl={baseUrl} />}
          {mode && <span className="rounded bg-lw-raised px-1.5 font-lw-mono">{mode}</span>}
          {syncing && <span className="text-lw-link">Syncing…</span>}
          {error && <span className="truncate text-lw-error">{error}</span>}
    </>
  )
}

// A way to ask the Reeve about the agent; none in the Reeve's own panel.
function askReeveAbout(reeve: boolean | undefined, name: string, agentId: string, agentKind: string | undefined, error: string | null): ReactNode {
  if (reeve) return undefined
  return <AskReeve brief={reeveBrief({ what: `the agent “${name}”, which waits for the operator`, ids: { task: agentId, "agent tool": agentKind }, error: error || undefined })} />
}

// The deck of subagents and shells, when it is shown: a grid beside the
// conversation on a wide panel, else a strip above it.
function SubagentDeck({ subagents, beside }: { subagents: ReturnType<typeof useSubagentTiles>; beside: boolean }) {
  if (!subagents.show || subagents.beside !== beside) return null
  if (!beside) return <TileDeck title={subagents.deckTitle} tiles={subagents.tiles} layout="strip" focused={subagents.focused} onFocus={subagents.setFocused} />
  return (
        <TileDeck
          title={subagents.deckTitle}
          tiles={subagents.tiles}
          layout="grid"
          focused={subagents.focused}
          onFocus={subagents.setFocused}
          highlighted={subagents.highlighted}
          onHighlight={subagents.setHighlighted}
        />
  )
}

// What the agent's terminal asks, when the panel shows it, at the end of the
// conversation.
function TerminalPromptCard({ asking, prompt, agentKind, onChoose, pending, notice, onDowngradeChimera }: {
  asking: boolean
  prompt: PromptState | null
  agentKind?: string
  onChoose: (keys: string, text?: string) => void
  pending: boolean
  notice?: string
  onDowngradeChimera?: () => Promise<void>
}) {
  if (!asking || !prompt) return null
  return (
            <NativeTerminalPrompt
              prompt={prompt}
              askedBy={agentToolName(agentKind)}
              onChoose={onChoose}
              pending={pending}
              notice={notice}
              onOpenTerminal={onDowngradeChimera ? () => { void onDowngradeChimera().catch(() => {}) } : undefined}
            />
  )
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
          // One item of the conversation: something said, or something the
          // agent did or asks.
          : <Fragment key={block.item.key}>{messageRow({ ...row, item: block.item }) ?? actionRow({ ...row, item: block.item })}</Fragment>)}
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

// A message: a reply, what the operator or Legato wrote, or a recovery
// excerpt Chimera keeps.
function messageRow({ item, agentKind }: FeedRowProps & { item: NativeConversationItem }): ReactNode {
  if (isRecoveryReply(item, agentKind)) return <ChimeraRecovery text={item.text} />
  if (item.role === "user") return <UserMessage text={item.text} />
  if (item.role === "assistant") {
    return (
      <div className="flex justify-start px-4 py-2">
        <div className={`min-w-0 max-w-full text-[13px] leading-[21px] text-[var(--theme-text,#c9c8d0)] ${item.isStreaming ? "opacity-80" : ""}`}>
          <ReviewMessageBody body={item.text} />
          {item.isStreaming && <span className="ml-0.5 animate-pulse">▋</span>}
        </div>
      </div>
    )
  }
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
  if (peerReeve) {
    return (
      <div className="flex justify-end px-4 py-2">
        <div
          aria-label={`Message from the Reeve on ${peerReeve.instance}`}
          className={`max-w-[75%] rounded-lg border-l-2 border-l-[var(--theme-keyword,#8b5cf6)] px-3 py-2 text-sm text-zinc-300 whitespace-pre-wrap break-words ${reeveWash}`}
        >
          <div className="mb-1 text-[10px] font-semibold uppercase tracking-wide text-[var(--theme-keyword,#c4b5fd)]">
            From the Reeve on <span className="normal-case">{peerReeve.instance}</span> · <span className="normal-case">{peerReeve.role}</span>
          </div>
          <div>{peerReeve.body}</div>
        </div>
      </div>
    )
  }
  // A message Legato wrote or carried.
  const legato = parseLegatoMessage(text)
  if (legato) {
    // Something that happened (a change set ran, Legato restarted): one line
    // among the steps, not a message anyone wrote.
    if (legato.event) return <LegatoEventRow label={legato.label} body={legato.body} />
    // Another agent wrote this and Legato only carried it, so it has a look
    // of its own, labelled with who sent it.
    const { peer } = legato
    if (peer !== undefined) {
      return (
        <div className="flex justify-end px-4 py-2">
          <div
            aria-label={peer ? `Message from agent ${peer}` : "Message from another agent"}
            className="max-w-[75%] rounded-lg border border-fuchsia-800/60 bg-fuchsia-950/30 px-3 py-2 text-sm text-zinc-300 whitespace-pre-wrap break-words"
          >
            <div className="mb-1 text-[10px] font-semibold uppercase tracking-wide text-fuchsia-400">
              Agent{peer && <> · <span className="normal-case">{peer}</span></>}
            </div>
            <div>{legato.body}</div>
          </div>
        </div>
      )
    }
    return (
      <div className="flex justify-end px-4 py-2">
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
    <div className="flex justify-end px-4 py-2">
      <div className="max-w-[min(520px,85%)] rounded-[10px_10px_2px_10px] border border-lw-line-strong bg-lw-selected px-3 py-2 text-[13px] text-lw-text whitespace-pre-wrap break-words">
        {text}
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
