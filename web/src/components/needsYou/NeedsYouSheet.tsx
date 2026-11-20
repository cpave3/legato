import { useRef } from "react"
import { Link } from "react-router-dom"
import { Check, X } from "lucide-react"
import { useModalLayer } from "../../hooks/useModalLayer"
import { setFocusMode, useFocusMode } from "../../lib/focusMode"
import { itemAgent } from "../../lib/needsYou"
import { ApprovalCard, FocusButton, PeerApprovalCard } from "../ApprovalTray"
import { NeedsYouRow } from "./NeedsYouRow"
import { useAgentInView, useRailApprovals, useRailItems } from "../../hooks/useNeedsYou"

// NeedsYouSheet is the needs-you rail on a phone: the same list, approvals
// first, in a sheet over the page that the tab bar's Needs you tab opens.
export function NeedsYouSheet({ onClose }: { onClose: () => void }) {
  const { items, hidden, approvalsShown, loaded, error, pending, onAction, onDismiss } = useRailItems()
  const approvals = useRailApprovals(approvalsShown)
  const inView = useAgentInView()
  const focusMode = useFocusMode()
  const count = approvals.approvals.length + approvals.peers.length + items.length
  const containerRef = useRef<HTMLDivElement>(null)
  useModalLayer({ open: true, onClose, containerRef })

  return (
    // It opens above the tab bar, as More does.
    <div className="fixed inset-x-0 top-0 bottom-[calc(3.5rem+env(safe-area-inset-bottom))] z-30 md:hidden">
      <div aria-hidden="true" className="absolute inset-0 bg-black/45" onClick={onClose} />
      <div ref={containerRef} role="dialog" aria-modal="true" tabIndex={-1} aria-label="Needs you" style={{ maxWidth: "none" }}
        className="absolute inset-x-0 bottom-0 flex max-h-[85%] flex-col rounded-t-2xl border-t border-lw-line-strong/40 bg-lw-rail font-lw text-lw-text shadow-2xl">
        <div className="flex justify-center pt-2"><span aria-hidden="true" className="h-1 w-10 rounded-full bg-lw-line" /></div>
        <div className="flex items-center gap-2 px-4 pb-3 pt-2.5">
          <h2 className="m-0 text-base font-semibold">Needs you</h2>
          {count > 0 && <span aria-label={`${count} ${count === 1 ? "needs" : "need"} you`} className="rounded-full bg-lw-needs-soft px-2 py-0.5 text-xs font-semibold text-lw-needs">{count}</span>}
          <span className="flex-1" />
          {focusMode && !approvals.asking && (
            <>
              <span className="rounded-full border border-lw-accent-ring bg-lw-accent-soft px-2 py-0.5 text-xs text-lw-accent-text">Focus is on</span>
              <button type="button" onClick={() => setFocusMode(false)} className="min-h-11 rounded-lg px-2 text-sm text-lw-text-2 hover:bg-lw-raised">Leave focus</button>
            </>
          )}
          {approvals.asking && <FocusButton />}
          <button type="button" onClick={onClose} aria-label="Close" className="flex size-11 items-center justify-center rounded-full text-lw-text-2 hover:text-lw-text"><X size={20} /></button>
        </div>
        {error && <p role="alert" className="mx-4 mb-2 text-xs text-lw-error">{error}</p>}
        <div className="flex min-h-0 flex-1 flex-col gap-2.5 overflow-y-auto px-3 pb-3">
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
          {loaded && count === 0 && (
            <div className="flex flex-col items-center gap-2 py-10 text-center">
              <div className="flex h-11 w-11 items-center justify-center rounded-xl bg-lw-live-soft text-lw-live"><Check size={22} aria-hidden="true" /></div>
              <p className="m-0 text-sm font-semibold">Nothing needs you</p>
            </div>
          )}
        </div>
        <div className="flex gap-1.5 border-t border-lw-line px-4 py-3 text-xs text-lw-text-2">
          {hidden > 0 && <><span>{hidden} {hidden === 1 ? "kind" : "kinds"} hidden here</span><span>·</span></>}
          <Link to="/attention" onClick={onClose} className="text-lw-link hover:underline">All in Attention</Link>
        </div>
      </div>
    </div>
  )
}
