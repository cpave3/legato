import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { ApprovalNotificationSettings } from "../ApprovalNotificationSettings"
import { approvalNotificationsEnabled } from "../../lib/approvalNotifications"

const fake = { permission: "default" as NotificationPermission, requestPermission: vi.fn(async () => "granted" as NotificationPermission) }

describe("ApprovalNotificationSettings", () => {
  beforeEach(() => {
    fake.permission = "default"
    fake.requestPermission.mockClear()
    vi.stubGlobal("Notification", fake)
  })
  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    localStorage.clear()
  })

  it("is on by default and turns off for this browser", () => {
    fake.permission = "granted"
    render(<ApprovalNotificationSettings />)
    const toggle = screen.getByRole("button", { name: /Notify me of approvals/ })
    expect(toggle.getAttribute("aria-pressed")).toBe("true")
    fireEvent.click(toggle)
    expect(approvalNotificationsEnabled()).toBe(false)
    expect(toggle.getAttribute("aria-pressed")).toBe("false")
  })

  it("asks the browser for permission when it has not been given", async () => {
    render(<ApprovalNotificationSettings />)
    fireEvent.click(screen.getByRole("button", { name: "Allow notifications" }))
    await vi.waitFor(() => expect(fake.requestPermission).toHaveBeenCalled())
  })

  it("says how to unblock notifications the browser refuses", () => {
    fake.permission = "denied"
    render(<ApprovalNotificationSettings />)
    expect(screen.getByText(/blocks notifications for this site/)).toBeTruthy()
  })
})
