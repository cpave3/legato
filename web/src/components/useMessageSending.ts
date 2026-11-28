import { useCallback, useRef, useState, type RefObject } from "react"
import type { WSMessage } from "../lib/agentWire"
import { compactAgent, compactCommand } from "../lib/compaction"
import { isCommandText } from "../lib/composer"
import { postAgentJson } from "../lib/nativeAgentApi"
import { isBangText, reasonText } from "../lib/nativeAgentView"
import { MESSAGE_REQUEST } from "../lib/nativeFeed"
import type { NativeConversationItem, NativeSyncState } from "../lib/nativeAgentSync"

// How many of the commands it sent a panel remembers, to know a refusal of one.
const SENT_COMMANDS_KEPT = 20

const NOT_CONNECTED = "Not connected to Legato. Your message is still in the box: send it again once connected."

// /model and /mode change the session's settings through its endpoints and
// are never sent to it as messages.
function isBuiltinSlash(text: string): boolean {
  return ["/model", "/mode"].includes(text.split(/\s+/)[0])
}

async function runBuiltinSlash(baseUrl: string, agentId: string, raw: string) {
  const [cmd, ...rest] = raw.split(/\s+/)
  const arg = rest.join(" ")
  if (cmd === "/model") await postAgentJson(baseUrl, agentId, "model", { model: arg })
  else if (cmd === "/mode") await postAgentJson(baseUrl, agentId, "modes", { mode: arg })
}

export interface SendingOptions {
  agentId: string
  baseUrl: string
  send: (message: WSMessage) => boolean
  busy: boolean
  draft: string
  setDraft: (draft: string) => void
  sync: {
    syncStateRef: RefObject<NativeSyncState>
    replaceSyncState: (next: NativeSyncState) => void
    workingUnconfirmedRef: RefObject<boolean>
    failWith: (reason: unknown) => void
  }
  enqueue: (text: string, shown: NativeConversationItem[], requestId: string) => void
  runOrQueueBang: (command: string) => void
  scrollToBottomAfterRender: () => void
}

// useMessageSending sends what is in the message box: a shell command, the
// agent's own compaction, a settings command, or a message, queued while
// the agent works. It says why a message could not leave the page.
export function useMessageSending({ agentId, baseUrl, send, busy, draft, setDraft, sync, enqueue, runOrQueueBang, scrollToBottomAfterRender }: SendingOptions) {
  // Commands this panel sent, by their words, whose refusal it would show.
  // Legato's stream replays the refusals it kept to a panel that opens; those
  // are not this panel's to show.
  const sentCommandsRef = useRef<string[]>([])
  // Why the last message could not leave the page; it stays in the box.
  const [sendError, setSendError] = useState<string | null>(null)
  const { syncStateRef, replaceSyncState, workingUnconfirmedRef, failWith } = sync

  const sendMessage = useCallback((text: string) => {
    const requestId = `${MESSAGE_REQUEST}${crypto.randomUUID()}`
    if (send({ type: "agent_message", agent_id: agentId, content: text, queue: busy, request_id: requestId }) === false) {
      setSendError(NOT_CONNECTED)
      return
    }
    setSendError(null)
    // A command the agent runs is never shown as a message, so it is not
    // listed; it is only answered, and an error for it shows as one. A path
    // at the start (/tmp/a.png what is this?) is a message, not a command.
    if (!isCommandText(text)) enqueue(text, syncStateRef.current.items, requestId)
    else sentCommandsRef.current = [...sentCommandsRef.current.slice(-(SENT_COMMANDS_KEPT - 1)), text]
    setDraft("")
    if (!busy) {
      workingUnconfirmedRef.current = true
      replaceSyncState({ ...syncStateRef.current, busy: true, error: null })
    }
    scrollToBottomAfterRender()
  }, [agentId, busy, send, enqueue, setDraft, replaceSyncState, scrollToBottomAfterRender, syncStateRef, workingUnconfirmedRef])

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
      compactAgent(baseUrl, agentId, compact.instructions).catch((reason) => setSendError(reasonText(reason)))
      return
    }
    if (isBuiltinSlash(trimmed)) {
      setDraft("")
      void runBuiltinSlash(baseUrl, agentId, trimmed).catch(failWith)
      return
    }
    sendMessage(trimmed)
  }, [draft, agentId, baseUrl, setDraft, runOrQueueBang, failWith, sendMessage])

  // The session refused a command: shown as an error when this panel sent it.
  const commandRefused = useCallback((words: string, reason: string) => {
    const at = sentCommandsRef.current.indexOf(words.trim())
    if (at < 0) return false
    sentCommandsRef.current.splice(at, 1)
    setSendError(`Not sent: ${words.trim()}: ${reason}`)
    return true
  }, [])

  const handleInterrupt = useCallback(() => {
    send({ type: "agent_interrupt", agent_id: agentId })
  }, [agentId, send])

  return { sendError, handleSend, handleInterrupt, commandRefused }
}
