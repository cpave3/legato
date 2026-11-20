import { useState } from "react"
import { Bell } from "lucide-react"
import { approvalNotificationsEnabled, notificationsSupported, setApprovalNotificationsEnabled } from "../lib/approvalNotifications"
import { SettingsRow, SettingsRows, Toggle, quietButton } from "./settings/SettingsUI"

// ApprovalNotificationSettings turns this browser's approval notifications
// on and off, and asks the browser for permission to show them.
export function ApprovalNotificationSettings() {
  const [on, setOn] = useState(approvalNotificationsEnabled)
  const [permission, setPermission] = useState<NotificationPermission | null>(() =>
    notificationsSupported() ? Notification.permission : null,
  )
  if (permission === null) return null

  const ask = async () => {
    try {
      setPermission(await Notification.requestPermission())
    } catch {
      // An old browser: requestPermission takes a callback instead.
    }
  }
  const toggle = () => {
    const next = !on
    setOn(next)
    setApprovalNotificationsEnabled(next)
    if (next && permission === "default") void ask()
  }
  const description =
    permission === "denied"
      ? "Your browser blocks notifications for this site. Allow them in its site settings, then reload."
      : permission === "default"
        ? "Approvals, when this tab is not in front. Your browser asks for permission first."
        : "Approvals, when this tab is not in front. A click opens it."

  return (
    <SettingsRows>
      <SettingsRow
        icon={Bell}
        title="In this browser"
        description={description}
        browser
        control={
          <div className="flex items-center gap-2">
            {on && permission === "default" && (
              <button type="button" onClick={() => void ask()} className={quietButton}>Allow notifications</button>
            )}
            <Toggle label="Notify me in this browser" on={on} onChange={toggle} />
          </div>
        }
      />
    </SettingsRows>
  )
}
