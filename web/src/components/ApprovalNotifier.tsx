import { useEffect, useRef, useState } from "react"
import { useLocation, useNavigate } from "react-router-dom"
import { useApprovals } from "../hooks/useApprovals"
import { APPROVAL_NOTIFICATIONS_KEY, approvalNotice, approvalNotificationsEnabled, freshApprovals, notificationsSupported } from "../lib/approvalNotifications"

// ApprovalNotifier shows a browser notification for each approval that
// arrives while the Legato tab is not in front; in front, the approval tray
// shows it. A click brings the tab forward with the tray open on it, and a
// notification goes once its approval is decided. It draws nothing.
export function ApprovalNotifier() {
  const { data: approvals } = useApprovals()
  const navigate = useNavigate()
  const location = useLocation()
  // Where the person is, for a click to open the tray there.
  const where = useRef(location.pathname)
  useEffect(() => { where.current = location.pathname }, [location.pathname])
  const seen = useRef<Set<string> | null>(null)
  const shown = useRef(new Map<string, Notification>())
  const [enabled, setEnabled] = useState(approvalNotificationsEnabled)

  useEffect(() => {
    const update = () => setEnabled(approvalNotificationsEnabled())
    const fromStorage = (event: StorageEvent) => { if (event.key === APPROVAL_NOTIFICATIONS_KEY) update() }
    window.addEventListener("legato:approval-notifications", update)
    window.addEventListener("storage", fromStorage)
    return () => {
      window.removeEventListener("legato:approval-notifications", update)
      window.removeEventListener("storage", fromStorage)
    }
  }, [])

  useEffect(() => {
    const state = freshApprovals(seen.current, approvals)
    seen.current = state.seen
    const pending = new Set(approvals.map((approval) => approval.id))
    for (const [id, notification] of shown.current) {
      if (!pending.has(id)) {
        notification.close()
        shown.current.delete(id)
      }
    }
    if (!enabled || state.fresh.length === 0 || !notificationsSupported() || Notification.permission !== "granted") return
    if (!document.hidden && document.hasFocus()) return
    for (const approval of state.fresh) {
      const { title, body } = approvalNotice(approval)
      const notification = new Notification(title, { body, tag: approval.id })
      notification.onclick = () => {
        window.focus()
        navigate({ pathname: where.current, search: `?approval=${encodeURIComponent(approval.id)}` })
        notification.close()
      }
      shown.current.set(approval.id, notification)
    }
  }, [approvals, enabled, navigate])

  useEffect(() => () => {
    for (const notification of shown.current.values()) notification.close()
  }, [])

  return null
}
