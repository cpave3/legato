import type { Approval } from "./approvals"
import { agentKindLabel } from "./agentKind"

// Browser notifications for approvals that wait for the person, while the
// Legato tab is not in front. On by default; turned off per browser.
export const APPROVAL_NOTIFICATIONS_KEY = "legato:approval-notifications"

export function approvalNotificationsEnabled(): boolean {
  try {
    return localStorage.getItem(APPROVAL_NOTIFICATIONS_KEY) !== "false"
  } catch {
    return true
  }
}

export function setApprovalNotificationsEnabled(on: boolean) {
  try {
    localStorage.setItem(APPROVAL_NOTIFICATIONS_KEY, on ? "true" : "false")
  } catch {
    // Storage blocked: the setting lasts as long as the page.
  }
  window.dispatchEvent(new Event("legato:approval-notifications"))
}

// notificationsSupported says whether this page can show notifications: the
// browser has them, the page is secure (HTTPS or localhost), and it is not
// the Legato app, whose notifications come from ntfy.
export function notificationsSupported(): boolean {
  return typeof window !== "undefined" && "Notification" in window && window.isSecureContext !== false &&
    !(window as unknown as { legatoShell?: unknown }).legatoShell
}

// freshApprovals finds the approvals not seen before. The first call (no
// seen set yet) only records what is waiting when the page loads.
export function freshApprovals(seen: Set<string> | null, approvals: Approval[]): { seen: Set<string>; fresh: Approval[] } {
  const next = new Set(seen ?? [])
  const fresh: Approval[] = []
  for (const approval of approvals) {
    if (!next.has(approval.id)) {
      next.add(approval.id)
      if (seen) fresh.push(approval)
    }
  }
  return { seen: next, fresh }
}

// approvalNotice is what a notification says: what is asked, then which
// task's agent asks, and what it would do.
export function approvalNotice(approval: Approval): { title: string; body: string } {
  const who = [approval.task_id, approval.details.task_title, approval.details.agent_kind && agentKindLabel(approval.details.agent_kind)].filter(Boolean).join(" · ")
  return {
    title: `Approval needed: ${approval.title}`,
    body: [who, approval.summary].filter(Boolean).join("\n"),
  }
}
