import { useEffect, useRef, useState } from "react"
import { useLocation } from "react-router-dom"
import { useQuery } from "@tanstack/react-query"
import { fetchRailAttention, itemAgent } from "../lib/needsYou"
import { queryKeys } from "../lib/query"
import { useFocusMode } from "../lib/focusMode"
import { clearSnooze, DEFAULT_SNOOZE_MS, holdsWork, readSnooze, snoozeOver, writeSnooze, type SheetCandidate } from "../lib/railSnooze"
import { useServer } from "./useServer"
import { useNavigationSummary } from "./useNavigationSummary"
import { usePeerApprovals } from "./usePeerApprovals"
import { useAgentInView } from "./useNeedsYou"
import { useAutoOpen } from "../lib/railAutoOpen"

// The longest delay setTimeout keeps; a longer one fires at once.
const MAX_TIMER_MS = 2 ** 31 - 1

// isTyping says whether the focused element takes text.
function isTyping(element: Element | null): boolean {
  if (!(element instanceof HTMLElement)) return false
  if (element.isContentEditable || element instanceof HTMLTextAreaElement) return true
  if (!(element instanceof HTMLInputElement)) return false
  return !["button", "checkbox", "radio", "range", "color", "file", "submit", "reset", "image"].includes(element.type)
}

// useTyping is whether the operator has a text box in focus.
function useTyping(): boolean {
  const [typing, setTyping] = useState(() => isTyping(document.activeElement))
  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | undefined
    // On focusout the focus has not moved yet: look once it has.
    const update = () => {
      clearTimeout(timer)
      timer = setTimeout(() => setTyping(isTyping(document.activeElement)), 0)
    }
    update()
    document.addEventListener("focusin", update)
    document.addEventListener("focusout", update)
    return () => {
      clearTimeout(timer)
      document.removeEventListener("focusin", update)
      document.removeEventListener("focusout", update)
    }
  }, [])
  return typing
}

const MODAL = '[aria-modal="true"]'

// useModalOpen is whether a modal dialog is on the page, kept current as
// dialogs come and go.
function useModalOpen(): boolean {
  const [modal, setModal] = useState(() => document.querySelector(MODAL) !== null)
  useEffect(() => {
    const update = () => setModal(document.querySelector(MODAL) !== null)
    update()
    const observer = new MutationObserver(update)
    observer.observe(document.body, { childList: true, subtree: true, attributes: true, attributeFilter: ["aria-modal"] })
    return () => observer.disconnect()
  }, [])
  return modal
}

// useSheetAutoOpen opens the phone's needs-you sheet by itself when an item
// waits, and keeps it away for the snooze once the operator closes it: any
// close, from the sheet, the tab or a link in it. A new approval, question,
// plan or gate ends the snooze early. It does not open while the operator
// types, in focus mode, on the Attention page, or over another sheet or
// dialog (blocked, or any aria-modal one); it opens once that ends. For the
// agent whose page is open, only an item that holds work opens it, since
// the page shows the rest; the page's own approval card stays, and
// answering either settles both. This device's choices (useAutoOpen) can
// leave out the agent in view, or every item that does not hold work. Only
// a phone runs it (see NeedsYouAutoOpen): from md up the rail is on the
// page.
export function useSheetAutoOpen({ open, blocked, onOpen }: {
  open: boolean
  blocked: boolean
  onOpen: () => void
}) {
  const { baseUrl } = useServer()
  const view = useQuery({ queryKey: queryKeys.railAttention(baseUrl), queryFn: () => fetchRailAttention(baseUrl) })
  const { data: summary } = useNavigationSummary()
  const peers = usePeerApprovals((summary?.peer_approvals ?? 0) > 0)
  const location = useLocation()
  const inView = useAgentInView()
  const choice = useAutoOpen(baseUrl)
  const focusMode = useFocusMode()
  const typing = useTyping()
  const modal = useModalOpen()
  // Bumped when a snooze ends, to look again.
  const [tick, setTick] = useState(0)

  const items = view.data?.items ?? []
  // The sheet lists paired instances' approvals only while approvals show,
  // which the rail view says once it has loaded.
  const shownPeers = view.data && !view.data.hiddenNames.includes("approvals") ? peers : []
  const snoozeMs = (view.data?.snoozeSeconds ?? 0) > 0 ? view.data!.snoozeSeconds! * 1000 : DEFAULT_SNOOZE_MS
  const everything = [...items.map((each) => each.id), ...peers.map((peer) => `peer:${peer.instance_id}:${peer.id}`)]
  const candidates: SheetCandidate[] = [
    ...items.filter((each) => {
      const held = holdsWork(each)
      if (!held && !choice.everything) return false
      return inView === null || itemAgent(each) !== inView || (held && choice.agentInView)
    }).map((each) => ({ id: each.id, holdsWork: holdsWork(each) })),
    ...shownPeers.map((peer) => ({ id: `peer:${peer.instance_id}:${peer.id}`, holdsWork: true })),
  ]

  // Every close starts the snooze, with what the rail held then.
  const latest = useRef({ everything, snoozeMs })
  latest.current = { everything, snoozeMs }
  const wasOpen = useRef(open)
  useEffect(() => {
    if (wasOpen.current && !open) {
      writeSnooze(baseUrl, { until: Date.now() + latest.current.snoozeMs, seen: latest.current.everything })
    }
    wasOpen.current = open
  }, [open, baseUrl])

  const key = candidates.map((each) => `${each.id}:${each.holdsWork ? 1 : 0}`).join(",")
  const onOpenRef = useRef(onOpen)
  onOpenRef.current = onOpen
  useEffect(() => {
    if (open || blocked || key === "") return
    if (focusMode || typing || modal || location.pathname.startsWith("/attention")) return
    const now = Date.now()
    const snooze = readSnooze(baseUrl, now)
    if (snooze && !snoozeOver(snooze, candidates, now)) {
      // setTimeout takes at most 2^31-1 ms; a longer snooze looks again then.
      const timer = setTimeout(() => setTick((n) => n + 1), Math.min(snooze.until - now + 1, MAX_TIMER_MS))
      return () => clearTimeout(timer)
    }
    clearSnooze(baseUrl)
    onOpenRef.current()
    // candidates is read through key.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, blocked, key, focusMode, typing, modal, location.pathname, baseUrl, tick])
}

// NeedsYouAutoOpen runs useSheetAutoOpen; the layout mounts it on a phone
// only.
export function NeedsYouAutoOpen(props: Parameters<typeof useSheetAutoOpen>[0]) {
  useSheetAutoOpen(props)
  return null
}
