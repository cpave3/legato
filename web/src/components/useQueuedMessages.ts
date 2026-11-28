import { useCallback, useState } from "react"
import type { QueuedEntry } from "./Composer"
import { failQueuedByWords, queueMessage, settleQueued, unreadQueued, type QueuedMessage } from "../lib/composer"
import type { NativeConversationItem } from "../lib/nativeAgentSync"

interface QueuedActions {
  edit: (entry: QueuedMessage) => void
  remove: (id: string) => void
}

// What can be done with a listed message: one that failed can be taken back
// into the box or dropped; one sent to an idle agent that never showed it
// can stop being listed.
function queuedEntryActions(entry: QueuedMessage, busy: boolean, actions: QueuedActions): Partial<QueuedEntry> {
  if (entry.state === "failed") {
    return { onEdit: () => actions.edit(entry), onRemove: () => actions.remove(entry.id) }
  }
  if (!busy && entry.state === "sent") {
    return { onRemove: () => actions.remove(entry.id), removeLabel: "Stop listing this message" }
  }
  return {}
}

// useQueuedMessages lists the messages sent from the box until the
// conversation shows them. A turn that ends is no proof: what it did not
// read is the agent's next prompt.
export function useQueuedMessages(items: NativeConversationItem[], busy: boolean, setDraft: (draft: string) => void, focusBox: () => void) {
  const [queued, setQueued] = useState<QueuedMessage[]>([])
  const unread = unreadQueued(queued, items)
  // Forget what the conversation shows, so a conversation started over (a
  // /clear) does not bring it back.
  if (unread.length !== queued.length) setQueued(unread)

  const enqueue = useCallback((text: string, shown: NativeConversationItem[], requestId: string) => {
    setQueued((current) => queueMessage(current, text, shown, requestId))
  }, [])
  // Legato answered about a message: delivered, or why it could not be.
  const settle = useCallback((requestId: string, error?: string) => {
    setQueued((current) => settleQueued(current, requestId, error))
  }, [])
  const failByWords = useCallback((words: string, reason: string) => {
    setQueued((current) => failQueuedByWords(current, words, reason))
  }, [])
  const remove = useCallback((id: string) => {
    setQueued((current) => current.filter((other) => other.id !== id))
  }, [])
  const edit = useCallback((entry: QueuedMessage) => {
    remove(entry.id)
    setDraft(entry.text)
    focusBox()
  }, [remove, setDraft, focusBox])

  const entries: QueuedEntry[] = unread.map((entry) => ({
    key: entry.id,
    text: entry.text,
    state: entry.state,
    error: entry.error,
    ...queuedEntryActions(entry, busy, { edit, remove }),
  }))

  return { entries, enqueue, settle, failByWords }
}
