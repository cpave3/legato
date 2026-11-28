import { useCallback, useEffect, useRef, useState, type RefObject } from "react"
import { useAgentReselected } from "../hooks/useAgentSelection"
import { apiFetch } from "../lib/api"
import {
  createNativeSyncState, reconcileNativeSnapshot, reduceNativeEvent,
  type NativeEvent, type NativeSnapshot, type NativeSyncState,
} from "../lib/nativeAgentSync"
import { reasonText } from "../lib/nativeAgentView"
import { replayEvents, snapshotTurnRuns } from "../lib/nativeFeed"
import { scrollAnchorOf, type ScrollAnchor } from "./useConversationScroll"

// useNativeSyncState is a native session as the panel knows it: the state
// its events and snapshots build, and what changes it.
export function useNativeSyncState() {
  const [syncState, setSyncState] = useState<NativeSyncState>(() => createNativeSyncState())
  const syncStateRef = useRef(syncState)
  const modelRequestRef = useRef(0)
  const invalidateModelRequests = useCallback(() => { modelRequestRef.current++ }, [])
  // Events that arrive while a snapshot loads, applied once it has.
  const queuedEventsRef = useRef<NativeEvent[] | null>(null)
  // True while the panel shows Working only because the person sent a message
  // to an idle session, before the session says a turn started. A message
  // that was not sent then takes it back: nothing is running (LEG-151).
  const workingUnconfirmedRef = useRef(false)
  const [loading, setLoading] = useState(true)
  const [syncing, setSyncing] = useState(false)
  const [mode, setMode] = useState<string>("")

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

  const applyEvent = useCallback((event: NativeEvent) => {
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

  return {
    syncState, syncStateRef, replaceSyncState, applyEvent, stopUnconfirmedWorking, failWith,
    modelRequestRef, invalidateModelRequests, queuedEventsRef, workingUnconfirmedRef,
    loading, setLoading, syncing, setSyncing, mode, setMode,
  }
}

export type NativeSync = ReturnType<typeof useNativeSyncState>

async function fetchSnapshot(baseUrl: string, agentId: string, initialHydration: boolean): Promise<NativeSnapshot> {
  const response = await apiFetch(baseUrl, `/api/agents/${agentId}/chimera/snapshot${initialHydration ? "?initial=true" : ""}`)
  if (!response.ok) throw new Error(`snapshot: ${response.status}`)
  return response.json()
}

// A snapshot replaces what the panel knew, with the events that arrived
// while it loaded on top.
function applySnapshot(snapshot: NativeSnapshot, eventQueue: NativeEvent[], sync: Pick<NativeSync, "setMode" | "workingUnconfirmedRef" | "syncStateRef" | "replaceSyncState">) {
  sync.setMode(snapshot.mode || "")
  if (snapshotTurnRuns(snapshot.status)) sync.workingUnconfirmedRef.current = false
  sync.replaceSyncState(reconcileNativeSnapshot(snapshot, sync.syncStateRef.current, eventQueue))
}

export interface ResyncScroll {
  scrollRef: RefObject<HTMLDivElement | null>
  restoreAfterSync: (anchor: ScrollAnchor | null, toEnd: boolean) => void
}

// useSnapshotResync refreshes the panel from the session's snapshot, the
// newest refresh winning, and offers it to a menu outside the panel and to
// picking the open agent again.
export function useSnapshotResync(agentId: string, baseUrl: string, sync: NativeSync, scroll: ResyncScroll, resyncRef?: { current: (() => void) | null }) {
  const syncGenerationRef = useRef(0)
  const { queuedEventsRef, syncStateRef, replaceSyncState, setLoading, setSyncing, setMode, workingUnconfirmedRef } = sync
  const { scrollRef, restoreAfterSync } = scroll

  const resync = useCallback(async (scrollAfterSync = false, initialHydration = false) => {
    const generation = ++syncGenerationRef.current
    const latest = () => generation === syncGenerationRef.current
    const eventQueue = queuedEventsRef.current ?? []
    const viewport = scrollRef.current
    const scrollAnchor = scrollAnchorOf(viewport)
    queuedEventsRef.current = eventQueue
    if (viewport) setSyncing(true)
    else setLoading(true)
    try {
      const snapshot = await fetchSnapshot(baseUrl, agentId, initialHydration)
      if (!latest()) return
      applySnapshot(snapshot, eventQueue, { setMode, workingUnconfirmedRef, syncStateRef, replaceSyncState })
      restoreAfterSync(scrollAnchor, scrollAfterSync)
    } catch (reason) {
      if (latest()) replaceSyncState({ ...replayEvents(syncStateRef.current, eventQueue), error: reasonText(reason) })
    } finally {
      if (latest()) {
        queuedEventsRef.current = null
        setLoading(false)
        setSyncing(false)
      }
    }
  }, [agentId, baseUrl, replaceSyncState, scrollRef, restoreAfterSync, queuedEventsRef, syncStateRef, setLoading, setSyncing, setMode, workingUnconfirmedRef])

  useEffect(() => {
    if (!resyncRef) return
    resyncRef.current = () => { void resync() }
    return () => { resyncRef.current = null }
  }, [resyncRef, resync])
  // Picking the agent that is already open asks for a fresh view of it.
  useAgentReselected(agentId, resync)

  return resync
}
