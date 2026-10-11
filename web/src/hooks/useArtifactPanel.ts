import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import type { ArtifactFocus } from "../components/AgentArtifactPanel"
import { useWebSocket, type WSMessage } from "./useWebSocket"

interface PanelState {
  open: boolean
  focus: ArtifactFocus | null
  // Artifacts shown but not yet looked at, per agent, oldest first.
  unseen: Record<string, string[]>
}

function withoutAgent(unseen: Record<string, string[]>, agentId: string | null): Record<string, string[]> {
  if (!agentId || !(agentId in unseen)) return unseen
  const rest = { ...unseen }
  delete rest[agentId]
  return rest
}

// useArtifactPanel decides when the artifact side panel is open and on what.
// A new artifact from the agent in view opens the panel on it; one from any
// other agent is counted, for a badge, and opens when the person goes to that
// agent. Cards never open the panel: they are drawn in the conversation.
//
// autoOpen is false where the panel covers the whole screen (a phone): there
// a new artifact is only marked as new, so it cannot land on top of what the
// person is typing.
export function useArtifactPanel(selectedId: string | null, autoOpen: boolean) {
  const { subscribe } = useWebSocket()
  const [state, setState] = useState<PanelState>({ open: false, focus: null, unseen: {} })
  const selectedRef = useRef(selectedId)
  const autoOpenRef = useRef(autoOpen)
  useEffect(() => {
    autoOpenRef.current = autoOpen
  }, [autoOpen])
  // An item a link asked for, for an agent that is not in view yet.
  const linkedRef = useRef<{ agentId: string; itemId: string } | null>(null)

  // Going to another agent: its newest unseen artifact opens, else the panel
  // closes rather than show the previous agent's content.
  useEffect(() => {
    selectedRef.current = selectedId
    const linked = linkedRef.current
    if (linked && linked.agentId === selectedId) {
      // The person followed a link to this item, so it opens wherever the
      // panel is, a full-screen sheet included.
      linkedRef.current = null
      setState((current) => ({ open: true, focus: { itemId: linked.itemId, revision: 0 }, unseen: withoutAgent(current.unseen, selectedId) }))
      return
    }
    setState((current) => {
      const waiting = selectedId ? current.unseen[selectedId] ?? [] : []
      if (waiting.length > 0 && autoOpenRef.current) {
        return { open: true, focus: { itemId: waiting[waiting.length - 1], revision: 0 }, unseen: withoutAgent(current.unseen, selectedId) }
      }
      return { open: false, focus: null, unseen: current.unseen }
    })
  }, [selectedId])

  useEffect(() => subscribe((message: WSMessage) => {
    if (message.type !== "agent_ui_changed" || !message.task_id || !message.item_id) return
    const taskId = message.task_id
    const itemId = message.item_id
    if (message.kind === "deleted") {
      setState((current) => ({
        ...current,
        focus: current.focus?.itemId === itemId ? null : current.focus,
        unseen: taskId in current.unseen
          ? { ...current.unseen, [taskId]: current.unseen[taskId].filter((id) => id !== itemId) }
          : current.unseen,
      }))
      return
    }
    if (message.placement !== "panel") return
    setState((current) => {
      if (taskId === selectedRef.current && (autoOpenRef.current || current.open)) {
        return { open: true, focus: { itemId, revision: 0 }, unseen: withoutAgent(current.unseen, taskId) }
      }
      const waiting = current.unseen[taskId] ?? []
      return waiting.includes(itemId) ? current : { ...current, unseen: { ...current.unseen, [taskId]: [...waiting, itemId] } }
    })
  }), [subscribe])

  const openItem = useCallback((itemId: string, revision: number) => {
    setState((current) => ({ open: true, focus: { itemId, revision }, unseen: withoutAgent(current.unseen, selectedRef.current) }))
  }, [])

  // showFor opens the panel on one item of one agent, for a link that names
  // both. The agent may not be in view yet: the item then opens when it is.
  const showFor = useCallback((agentId: string, itemId: string) => {
    if (selectedRef.current === agentId) {
      openItem(itemId, 0)
      return
    }
    linkedRef.current = { agentId, itemId }
  }, [openItem])

  const toggle = useCallback(() => {
    setState((current) => {
      if (current.open) return { ...current, open: false }
      const selected = selectedRef.current
      const waiting = selected ? current.unseen[selected] ?? [] : []
      return {
        open: true,
        focus: waiting.length > 0 ? { itemId: waiting[waiting.length - 1], revision: 0 } : current.focus,
        unseen: withoutAgent(current.unseen, selected),
      }
    })
  }, [])

  const close = useCallback(() => setState((current) => ({ ...current, open: false })), [])

  const unseen = useMemo(() => {
    const counts: Record<string, number> = {}
    for (const [agentId, items] of Object.entries(state.unseen)) {
      if (items.length > 0) counts[agentId] = items.length
    }
    return counts
  }, [state.unseen])

  return { open: state.open, focus: state.focus, unseen, openItem, showFor, toggle, close }
}
