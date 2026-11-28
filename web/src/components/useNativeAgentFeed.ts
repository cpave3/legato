import { useEffect, useRef, useState } from "react"
import type { PromptState, WSMessage } from "../lib/agentWire"
import { isCommandText } from "../lib/composer"
import { nextCompaction, type Compaction } from "../lib/compaction"
import { createNativeSyncState, type NativeEvent, type NativeSyncState } from "../lib/nativeAgentSync"
import { endsTurn, MESSAGE_REQUEST, reopenRequest, subagentsRunning } from "../lib/nativeFeed"
import type { NativeSync } from "./useNativeAgentSync"

// What the routing of the panel's WebSocket messages acts on.
interface FeedContext {
  agentId: string
  agentKind?: string
  following: boolean
  state: () => NativeSyncState
  replaceSyncState: (next: NativeSyncState) => void
  applyEvent: (event: NativeEvent) => void
  stopUnconfirmedWorking: () => void
  // The session reported the message: Working is no longer only a guess.
  confirmWorking: () => void
  resync: () => Promise<void>
  timer: { current: number | null }
  setTerminalPrompt: (prompt: PromptState | null) => void
  settleMessage: (requestId: string, error?: string) => void
  failByWords: (words: string, reason: string) => void
  // A command was refused; says whether this panel sent it.
  commandRefused: (words: string, reason: string) => boolean
  setCompaction: (update: (current: Compaction | undefined) => Compaction | undefined) => void
  runNextBang: () => void
  followLatest: () => void
}

// A refresh from the snapshot a moment after something ended, so the
// session has written it.
function startResyncTimer(ctx: FeedContext) {
  ctx.timer.current = window.setTimeout(() => {
    ctx.timer.current = null
    void ctx.resync()
  }, 100)
}

function clearResyncTimer(timer: { current: number | null }) {
  if (timer.current != null) window.clearTimeout(timer.current)
}

// The agent list changed while a subagent the agent spawned may be running:
// it may have ended.
function subagentsMayHaveEnded(msg: WSMessage, ctx: FeedContext): boolean {
  return msg.type === "agents_changed" && ctx.following && subagentsRunning(ctx.state().items)
}

function routeAgentMessage(msg: WSMessage, ctx: FeedContext) {
  if (subagentsMayHaveEnded(msg, ctx)) {
    if (ctx.timer.current == null) startResyncTimer(ctx)
    return
  }
  if (msg.agent_id !== ctx.agentId) return
  routeOwnMessage(msg, ctx)
}

function routeOwnMessage(msg: WSMessage, ctx: FeedContext) {
  if (msg.type === "prompt_state") {
    ctx.setTerminalPrompt(msg.prompt ?? null)
    return
  }
  if (msg.type === "agent_message_sent" && msg.request_id) {
    ctx.settleMessage(msg.request_id)
    return
  }
  if (msg.type === "error") {
    requestFailed(msg, ctx)
    return
  }
  if (msg.type === "chimera_sync_required") {
    void ctx.resync()
    return
  }
  if (msg.type === "chimera_event") applyEventMessage(msg.content, ctx)
}

// Legato could not deliver something the panel sent: a message, or an
// answer to a permission request or a question, which is pending again.
function requestFailed(msg: WSMessage, ctx: FeedContext) {
  const requestId = msg.request_id
  if (!requestId) return
  if (requestId.startsWith(MESSAGE_REQUEST)) {
    ctx.settleMessage(requestId, msg.error || "The message could not be sent")
    ctx.stopUnconfirmedWorking()
    return
  }
  ctx.replaceSyncState(reopenRequest(ctx.state(), requestId, msg.error))
}

function applyEventMessage(content: string | undefined, ctx: FeedContext) {
  try {
    handleEvent(JSON.parse(content || "{}"), ctx)
  } catch {
    ctx.replaceSyncState({ ...ctx.state(), error: "Invalid native event; refresh from snapshot" })
  }
}

function handleEvent(event: NativeEvent, ctx: FeedContext) {
  if (event.type === "message_failed") {
    messageFailed(event, ctx)
    return
  }
  if (event.type === "session_started" || event.type === "user_message") ctx.confirmWorking()
  ctx.setCompaction((current) => nextCompaction(current, event))
  ctx.applyEvent(event)
  if (endsTurn(event, ctx.agentKind)) {
    clearResyncTimer(ctx.timer)
    startResyncTimer(ctx)
  }
  if (event.type === "run_finished") ctx.runNextBang()
  ctx.followLatest()
}

// The session would not take a message.
function messageFailed(event: NativeEvent, ctx: FeedContext) {
  const words = event.content ?? ""
  const reason = event.error || "The agent's session would not take it"
  // A command is not listed, so its failure has no row to mark.
  if (isCommandText(words)) {
    if (!ctx.commandRefused(words, reason)) return
  } else {
    ctx.failByWords(words, reason)
  }
  ctx.stopUnconfirmedWorking()
}

export interface FeedOptions {
  agentId: string
  agentKind?: string
  following: boolean
  send: (message: WSMessage) => unknown
  subscribe: (handler: (message: WSMessage) => void) => () => void
  sync: NativeSync
  resync: (scrollAfterSync?: boolean, initialHydration?: boolean) => Promise<void>
  restartInitialScroll: () => void
  followLatest: () => void
  setTerminalPrompt: (prompt: PromptState | null) => void
  settleMessage: (requestId: string, error?: string) => void
  failByWords: (words: string, reason: string) => void
  commandRefused: (words: string, reason: string) => boolean
  runNextBang: () => void
}

// useNativeAgentFeed follows a native agent over Legato's WebSocket while
// the panel is in view: its events, Legato's answers about what the panel
// sent, and the prompts its terminal shows. It starts over for a new agent.
export function useNativeAgentFeed({ agentId, agentKind, following, send, subscribe, sync, resync, restartInitialScroll, followLatest, setTerminalPrompt, settleMessage, failByWords, commandRefused, runNextBang }: FeedOptions) {
  const runFinishTimerRef = useRef<number | null>(null)
  const hydratedAgentRef = useRef<string | null>(null)
  // The agent's latest compaction, as its events tell it.
  const [compaction, setCompaction] = useState<Compaction>()
  const { syncStateRef, replaceSyncState, applyEvent, stopUnconfirmedWorking, invalidateModelRequests, queuedEventsRef, workingUnconfirmedRef } = sync

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
      clearResyncTimer(runFinishTimerRef)
    }
  }, [agentId, replaceSyncState, invalidateModelRequests, restartInitialScroll, queuedEventsRef])

  useEffect(() => {
    const ctx: FeedContext = {
      agentId, agentKind, following, state: () => syncStateRef.current, replaceSyncState, applyEvent, stopUnconfirmedWorking,
      confirmWorking: () => { workingUnconfirmedRef.current = false },
      resync, timer: runFinishTimerRef, setTerminalPrompt, settleMessage, failByWords, commandRefused, setCompaction, runNextBang, followLatest,
    }
    const unsubscribe = subscribe((msg: WSMessage) => routeAgentMessage(msg, ctx))
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
  }, [subscribe, agentId, agentKind, applyEvent, replaceSyncState, resync, runNextBang, following, send, stopUnconfirmedWorking, followLatest, settleMessage, failByWords, commandRefused, setTerminalPrompt, syncStateRef, workingUnconfirmedRef])

  return { compaction }
}
