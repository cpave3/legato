import { describe, it, expect, vi, afterEach } from "vitest"
import { render, screen, waitFor, fireEvent, cleanup } from "@testing-library/react"

const mockList = vi.fn()
const mockRemove = vi.fn()
vi.mock("../../lib/devices", () => ({
  listDevices: (...a: unknown[]) => mockList(...a),
  removeDevice: (...a: unknown[]) => mockRemove(...a),
}))

import { DevicesSettings } from "../DevicesSettings"

describe("DevicesSettings", () => {
  afterEach(() => {
    cleanup()
    delete (window as { legatoShell?: unknown }).legatoShell
    delete (window as { ReactNativeWebView?: unknown }).ReactNativeWebView
  })

  it("says plainly when no phone signs approvals", async () => {
    mockList.mockResolvedValue({ devices: [], enrolled: false, server_id: "srv-1" })
    render(<DevicesSettings baseUrl="http://legato" />)
    await waitFor(() => expect(screen.getByText(/No phone signs approvals yet/)).toBeTruthy())
    expect(screen.getByText(/open this server in the Legato app/)).toBeTruthy()
    expect(screen.queryByRole("button", { name: /Sign approvals with this phone/ })).toBeNull()
  })

  it("lists devices, and removing one asks the phone", async () => {
    mockList.mockResolvedValue({ devices: [{ id: "dev-1", kind: "android", name: "Pixel 9 Pro", security_level: "strongbox", status: "active", created_at: "2026-10-29T10:00:00Z" }], enrolled: true, server_id: "srv-1" })
    mockRemove.mockResolvedValue({ approval_id: "ap-9" })
    render(<DevicesSettings baseUrl="http://legato" />)
    await waitFor(() => expect(screen.getByText("Pixel 9 Pro")).toBeTruthy())
    expect(screen.getByText("StrongBox")).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Remove" }))
    await waitFor(() => expect(screen.getByText(/Approve removing Pixel 9 Pro on your phone/)).toBeTruthy())
  })

  it("offers to enroll this phone inside the app", async () => {
    const posted: string[] = []
    ;(window as { legatoShell?: unknown }).legatoShell = { approval: 1, key: "k1" }
    ;(window as { ReactNativeWebView?: unknown }).ReactNativeWebView = { postMessage: (m: string) => posted.push(m) }
    mockList.mockResolvedValue({ devices: [], enrolled: false, server_id: "srv-1" })
    render(<DevicesSettings baseUrl="http://legato" />)
    fireEvent.click(await screen.findByRole("button", { name: "Sign approvals with this phone" }))
    expect(JSON.parse(posted[0])).toEqual({ legatoShell: "enroll", key: "k1" })
  })
})
