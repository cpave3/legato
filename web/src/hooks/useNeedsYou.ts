import { useEffect, useRef, useState } from "react"
import { useLocation } from "react-router-dom"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { dismissAttentionItem, executeAttentionAction, type AttentionItem } from "../lib/attention"
import { fetchRailAttention } from "../lib/needsYou"
import { queryKeys } from "../lib/query"
import { setRailFolded, useRailState } from "../lib/railState"
import { approvalQuestions } from "../lib/questionDrafts"
import type { Approval } from "../lib/approvals"
import { useServer } from "./useServer"
import { useAgentSelection } from "./useAgentSelection"
import { useApprovals } from "./useApprovals"
import { usePeerApprovals } from "./usePeerApprovals"
import { useNavigationSummary } from "./useNavigationSummary"

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

// useRailApprovals is the approvals the rail shows first: every pending
// one and the paired instances' change sets, or, with approvals switched
// off, only the one the page was asked to show. A new one unfolds the rail.
export function useRailApprovals(shown: boolean) {
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

