import { useEffect, useRef, useState } from "react"
import { Link, useLocation } from "react-router-dom"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { Check, ChevronLeft, ChevronRight, ShieldCheck, SlidersHorizontal } from "lucide-react"
import { dismissAttentionItem, executeAttentionAction, type AttentionItem } from "../../lib/attention"
import { fetchRailAttention, itemAgent, needsYouKind, needsYouKinds } from "../../lib/needsYou"
import { RAIL_FOLDED, RAIL_OPEN, setRailFolded, setRailPresent, useRailState } from "../../lib/railState"
import { useApprovals } from "../../hooks/useApprovals"
import { useNavigationSummary } from "../../hooks/useNavigationSummary"
import { usePeerApprovals } from "../../hooks/usePeerApprovals"
import { approvalQuestions } from "../../lib/questionDrafts"
import type { Approval } from "../../lib/approvals"
import { ApprovalCard, FocusButton, PeerApprovalCard } from "../ApprovalTray"
import { queryKeys } from "../../lib/query"
import { useServer } from "../../hooks/useServer"
import { useAgentSelection } from "../../hooks/useAgentSelection"
import { NeedsYouRow, kindIcons, toneClass } from "./NeedsYouRow"

const iconButton = "inline-flex h-7 w-7 items-center justify-center rounded-md text-lw-text-2 hover:bg-lw-raised hover:text-lw-text"

// useRailItems is the rail's list with what acting on an item needs: the
// items, and the action and dismiss handlers that refetch after.
export function useRailItems() {
  const { baseUrl } = useServer()
  const queryClient = useQueryClient()
  const items = useQuery({ queryKey: queryKeys.railAttention(baseUrl), queryFn: () => fetchRailAttention(baseUrl) })
  const [pending, setPending] = useState<string | null>(null)
  const [error, setError] = useState("")
  const refresh = async () => {
    await queryClient.invalidateQueries({ queryKey: queryKeys.attention(baseUrl) })
    await queryClient.invalidateQueries({ queryKey: queryKeys.navigationSummary(baseUrl) })
  }
  const run = async (item: AttentionItem, act: () => Promise<void>) => {
    setPending(item.id)
    setError("")
    try {
      await act()
      await refresh()
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason))
    } finally {
      setPending(null)
    }
  }
  return {
    // The approvals producer's items are drawn from the approvals API
    // instead, with every control the tray had.
    items: (items.data?.items ?? []).filter((item) => item.producer !== "approvals"),
    hidden: items.data?.hidden ?? 0,
    approvalsShown: !(items.data?.hiddenNames ?? []).includes("approvals"),
    loaded: items.isSuccess,
    error,
    pending,
    onAction: (item: AttentionItem, actionId: string, text?: string) => {
      const action = item.actions?.find((each) => each.id === actionId)
      void run(item, () => executeAttentionAction(baseUrl, item.id, actionId, item.source_revision,
        text === undefined ? { confirmed: action?.requires_confirmation ?? false } : { confirmed: action?.requires_confirmation ?? false, text }))
    },
    onDismiss: (item: AttentionItem) => { void run(item, () => dismissAttentionItem(baseUrl, item.id, item.source_revision)) },
  }
}

// useAgentInView is the agent whose page is open, whose items the rail
// marks "this agent".
export function useAgentInView(): string | null {
  const location = useLocation()
  const { selectedAgentId } = useAgentSelection()
  return location.pathname.startsWith("/agents") ? selectedAgentId : null
}

// RailFooter says how many kinds the rail's settings hide.
function RailFooter({ hidden }: { hidden: number }) {
  if (hidden === 0) return null
  return (
    <div className="flex gap-1.5 border-t border-lw-line px-4 py-2.5 text-[11.5px] text-lw-text-2">
      <span>{hidden} {hidden === 1 ? "kind" : "kinds"} hidden here</span><span>·</span><Link to="/attention" className="text-lw-link hover:underline">All in Attention</Link>
    </div>
  )
}

// useRailApprovals is the approvals the rail shows first: every pending
// one and the paired instances' change sets, or, with approvals switched
// off, only the one the page was asked to show. A new one unfolds the rail.
function useRailApprovals(shown: boolean) {
  const { wanted } = useRailState()
  // Asked only while the navigation counts say some wait, or one is wanted.
  const { data: summary } = useNavigationSummary()
  const { data: approvals, refresh } = useApprovals((summary?.approvals ?? 0) > 0 || wanted !== null)
  const peers = usePeerApprovals((summary?.peer_approvals ?? 0) > 0)
  const seen = useRef<Set<string> | null>(null)
  useEffect(() => {
    // What was pending when the page opened is not new.
    if (seen.current === null) {
      seen.current = new Set(approvals.map((approval) => approval.id))
      return
    }
    const fresh = approvals.some((approval) => !seen.current!.has(approval.id))
    approvals.forEach((approval) => seen.current!.add(approval.id))
    if (fresh && shown) setRailFolded(false)
  }, [approvals, shown])
  const listed: Approval[] = shown ? approvals : approvals.filter((approval) => approval.id === wanted)
  return {
    approvals: listed,
    peers: shown ? peers : [],
    asking: approvals.some((approval) => approvalQuestions(approval).length > 0),
    wanted,
    refresh,
  }
}

// NeedsYouRail is the column on the right of every page, from md up: what
// needs the operator, by the rail settings. Folded, it is a strip with the
// count and one icon per item.
export function NeedsYouRail() {
  const { folded } = useRailState()
  const [tip, setTip] = useState<string | null>(null)
  const { items, hidden, approvalsShown, loaded, error, pending, onAction, onDismiss } = useRailItems()
  const approvals = useRailApprovals(approvalsShown)
  const inView = useAgentInView()
  const fold = setRailFolded
  useEffect(() => {
    setRailPresent(true)
    return () => setRailPresent(false)
  }, [])
  const count = approvals.approvals.length + approvals.peers.length + items.length

  if (folded) {
    return (
      <aside aria-label="Needs you, folded" style={{ width: RAIL_FOLDED }} className="hidden shrink-0 flex-col items-center gap-2 border-l border-lw-line bg-lw-rail py-3 md:flex">
        <button type="button" className={iconButton} aria-label="Open the rail" onClick={() => fold(false)}><ChevronLeft size={16} aria-hidden="true" /></button>
        {count > 0 && <span aria-label={`${count} need you`} className="mb-1.5 rounded-full bg-lw-needs-soft px-2 py-0.5 text-xs font-semibold text-lw-needs">{count}</span>}
        {approvals.approvals.map((approval) => (
          <button key={approval.id} type="button" aria-label={`Approval: ${approval.title}`} title={approval.title} onClick={() => fold(false)}
            className={`flex h-8 w-8 items-center justify-center rounded-lg ${toneClass.needs}`}>
            <ShieldCheck size={16} aria-hidden="true" />
          </button>
        ))}
        {items.map((item) => {
          const kind = needsYouKind(item)
          const Icon = kindIcons[kind]
          const here = inView !== null && itemAgent(item) === inView
          return (
            <div key={item.id} className="relative">
              <button type="button" aria-label={`${needsYouKinds[kind].word}: ${item.title}`} aria-describedby={tip === item.id ? `rail-tip-${item.id}` : undefined}
                onMouseEnter={() => setTip(item.id)} onMouseLeave={() => setTip(null)} onFocus={() => setTip(item.id)} onBlur={() => setTip(null)}
                onClick={() => fold(false)}
                className={`flex h-8 w-8 items-center justify-center rounded-lg ${toneClass[needsYouKinds[kind].tone]} ${here ? "outline outline-2 outline-offset-2 outline-lw-accent" : ""}`}>
                <Icon size={16} aria-hidden="true" />
              </button>
              {tip === item.id && (
                <div id={`rail-tip-${item.id}`} role="tooltip" className="absolute right-11 top-[-4px] z-30 w-60 rounded-lg border border-lw-line-strong/40 bg-lw-raised p-2.5 text-left text-xs leading-snug text-lw-text shadow-xl">
                  {item.task_id && <div className="mb-1 text-[11px] text-lw-text-2">{item.task_id}{here ? " · this agent" : ""}</div>}
                  <b className="font-semibold">{item.title}</b>
                  {item.reason && <div className="mt-1 text-lw-text-2">{item.reason}</div>}
                </div>
              )}
            </div>
          )
        })}
      </aside>
    )
  }

  return (
    <aside aria-label="Needs you" style={{ width: RAIL_OPEN }} className="hidden shrink-0 flex-col border-l border-lw-line bg-lw-rail md:flex">
      <div className="flex h-14 shrink-0 items-center gap-2 border-b border-lw-line pl-4 pr-2">
        <span className="text-sm font-semibold text-lw-text">Needs you</span>
        {count > 0 && <span className="rounded-full bg-lw-needs-soft px-2 py-0.5 text-[11px] font-semibold text-lw-needs">{count}</span>}
        <span className="flex-1" />
        {approvals.asking && <FocusButton />}
        <Link to="/settings#needs-you-rail" className={iconButton} aria-label="What the rail shows"><SlidersHorizontal size={16} aria-hidden="true" /></Link>
        <button type="button" className={iconButton} aria-label="Fold the rail" onClick={() => fold(true)}><ChevronRight size={16} aria-hidden="true" /></button>
      </div>
      {error && <p role="alert" className="mx-3 mt-2 text-xs text-lw-error">{error}</p>}
      {loaded && count === 0 ? (
        <div className="flex flex-1 flex-col items-center justify-center gap-3 px-8 text-center">
          <div className="flex h-11 w-11 items-center justify-center rounded-xl bg-lw-live-soft text-lw-live"><Check size={22} aria-hidden="true" /></div>
          <p className="m-0 text-sm font-semibold text-lw-text">Nothing needs you</p>
          <p className="m-0 text-[12.5px] leading-normal text-lw-text-2">Agents' asks, approvals and failed workflow steps appear here when they come.</p>
        </div>
      ) : (
        <div className="flex flex-1 flex-col gap-2.5 overflow-y-auto p-3">
          {approvals.approvals.map((approval) => (
            <ApprovalCard key={approval.id} approval={approval} focused={approval.id === approvals.wanted} onDone={approvals.refresh} />
          ))}
          {approvals.peers.map((approval) => (
            <PeerApprovalCard key={`${approval.instance_id}:${approval.id}`} approval={approval} />
          ))}
          {items.map((item) => (
            <NeedsYouRow key={item.id} item={item} onAction={onAction} onDismiss={onDismiss} pending={pending === item.id}
              here={inView !== null && itemAgent(item) === inView} />
          ))}
        </div>
      )}
      <RailFooter hidden={hidden} />
    </aside>
  )
}
