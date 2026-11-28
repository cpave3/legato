import { describe, it, expect, vi, afterEach } from "vitest"
import { act, render, screen, waitFor, fireEvent, cleanup } from "@testing-library/react"

const mockList = vi.fn()
const mockRemove = vi.fn()
vi.mock("../../lib/devices", () => ({
  listDevices: (...a: unknown[]) => mockList(...a),
  removeDevice: (...a: unknown[]) => mockRemove(...a),
}))

const mockEnrollPasskey = vi.fn()
let mockSupported = false
vi.mock("../../lib/passkeys", () => ({
  passkeysSupported: () => mockSupported,
  enrollPasskey: (...a: unknown[]) => mockEnrollPasskey(...a),
}))

import { QueryClientProvider } from "@tanstack/react-query"
import { createQueryClient } from "../../lib/query"
import { DevicesSettings as Devices } from "../DevicesSettings"

// Each test gets a cache of its own, as a page load does.
function DevicesSettings(props: { baseUrl: string }) {
  return <QueryClientProvider client={createQueryClient()}><Devices {...props} /></QueryClientProvider>
}

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
    expect(screen.queryByRole("button", { name: /Sign approvals with this device/ })).toBeNull()
  })

  it("comes into view when the address names it, as Attention's link to it does", async () => {
    mockList.mockResolvedValue({ devices: [], enrolled: false, server_id: "srv-1" })
    const scroll = vi.fn()
    Object.defineProperty(HTMLElement.prototype, "scrollIntoView", { configurable: true, value: scroll })
    window.history.replaceState(null, "", "/settings#devices")
    render(<DevicesSettings baseUrl="http://legato" />)
    await waitFor(() => expect(scroll).toHaveBeenCalledWith({ block: "start" }))
    window.history.replaceState(null, "", "/")
  })

  it("adds a passkey on this computer where the page can make one", async () => {
    mockSupported = true
    mockList.mockResolvedValue({ devices: [{ id: "dev-1", kind: "android", name: "Pixel 9 Pro", security_level: "strongbox", status: "active", created_at: "2026-10-29T10:00:00Z" }], enrolled: true, server_id: "srv-1" })
    mockEnrollPasskey.mockResolvedValue({ id: "dev-2", kind: "passkey", name: "Desktop", status: "pending", approval_id: "ap-5", origin: "https://erebus.example", created_at: "" })
    render(<DevicesSettings baseUrl="http://legato" />)
    fireEvent.click(await screen.findByRole("button", { name: /Add a passkey/ }))
    await waitFor(() => expect(mockEnrollPasskey).toHaveBeenCalledWith("http://legato", expect.any(String)))
    expect(await screen.findByText(/Approve adding it on your phone/)).toBeTruthy()
    mockSupported = false
  })

  it("shows a passkey with the page it belongs to", async () => {
    mockList.mockResolvedValue({ devices: [{ id: "dev-2", kind: "passkey", name: "Desktop", security_level: "synced passkey", status: "active", origin: "https://erebus.example:3080", created_at: "" }], enrolled: true, server_id: "srv-1" })
    render(<DevicesSettings baseUrl="http://legato" />)
    expect(await screen.findByText("Desktop")).toBeTruthy()
    expect(screen.getByText(/erebus.example:3080/)).toBeTruthy()
    expect(screen.getByText("Synced passkey")).toBeTruthy()
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
    fireEvent.click(await screen.findByRole("button", { name: "Sign approvals with this device" }))
    expect(JSON.parse(posted[0])).toEqual({ legatoShell: "enroll", key: "k1" })
  })

  it("says nothing of enrolling once a phone signs, and shows waiting and use", async () => {
    mockList.mockResolvedValue({ devices: [
      { id: "dev-1", kind: "android", name: "Pixel", security_level: "titan", status: "pending", created_at: "" },
      { id: "dev-2", kind: "android", name: "Old", status: "active", last_used_at: "2026-10-29T10:00:00Z", created_at: "" },
    ], enrolled: true, server_id: "srv-1" })
    render(<DevicesSettings baseUrl="http://legato" />)
    expect(await screen.findByText("Pixel")).toBeTruthy()
    expect(screen.queryByText(/No phone signs approvals yet/)).toBeNull()
    expect(screen.getByText("titan")).toBeTruthy()
    expect(screen.getByText("waiting for approval")).toBeTruthy()
    expect(screen.getByText("Not used yet")).toBeTruthy()
    expect(screen.getByText(`Last signed ${new Date("2026-10-29T10:00:00Z").toLocaleString()}`)).toBeTruthy()
    expect(screen.queryByRole("list")).toBeTruthy()
    expect(screen.queryByRole("button", { name: /Add a passkey/ })).toBeNull()
  })

  it("lists nothing without devices", async () => {
    mockList.mockResolvedValue({ devices: [], enrolled: true, server_id: "srv-1" })
    render(<DevicesSettings baseUrl="http://legato" />)
    await waitFor(() => expect(mockList).toHaveBeenCalled())
    expect(screen.queryByRole("list")).toBeNull()
  })

  it("says a device was removed, or why it could not be", async () => {
    mockList.mockResolvedValue({ devices: [{ id: "dev-1", kind: "android", name: "Pixel", status: "active", created_at: "" }], enrolled: true, server_id: "srv-1" })
    mockRemove.mockResolvedValueOnce({})
    render(<DevicesSettings baseUrl="http://legato" />)
    fireEvent.click(await screen.findByRole("button", { name: "Remove" }))
    expect((await screen.findByRole("status")).textContent).toBe("Pixel was removed.")
    mockRemove.mockRejectedValueOnce(new Error("refused"))
    fireEvent.click(screen.getByRole("button", { name: "Remove" }))
    expect((await screen.findByRole("alert")).textContent).toBe("refused")
    expect(screen.queryByRole("status")).toBeNull()
  })

  it("reports a passkey made at once, and one that failed", async () => {
    mockSupported = true
    mockList.mockResolvedValue({ devices: [], enrolled: true, server_id: "srv-1" })
    mockEnrollPasskey.mockResolvedValueOnce({ id: "dev-2", kind: "passkey", name: "Desktop", status: "active", created_at: "" })
    render(<DevicesSettings baseUrl="http://legato" />)
    fireEvent.click(await screen.findByRole("button", { name: "Add a passkey for this computer" }))
    expect((await screen.findByRole("status")).textContent).toBe("This computer's passkey now signs approvals.")
    mockEnrollPasskey.mockRejectedValueOnce("cancelled by the operator")
    fireEvent.click(screen.getByRole("button", { name: "Add a passkey for this computer" }))
    expect((await screen.findByRole("alert")).textContent).toBe("cancelled by the operator")
    mockSupported = false
  })

  it("shows the passkey wait while it is asked", async () => {
    mockSupported = true
    mockList.mockResolvedValue({ devices: [], enrolled: true, server_id: "srv-1" })
    mockEnrollPasskey.mockReturnValueOnce(new Promise(() => {}))
    render(<DevicesSettings baseUrl="http://legato" />)
    fireEvent.click(await screen.findByRole("button", { name: "Add a passkey for this computer" }))
    expect((await screen.findByRole("button", { name: "Waiting for your passkey…" })).hasAttribute("disabled")).toBe(true)
    mockSupported = false
  })

  it("reports what the app says of enrolling", async () => {
    ;(window as { legatoShell?: unknown }).legatoShell = { approval: 1, key: "k1" }
    ;(window as { ReactNativeWebView?: unknown }).ReactNativeWebView = { postMessage: () => {} }
    mockList.mockResolvedValue({ devices: [], enrolled: false, server_id: "srv-1" })
    render(<DevicesSettings baseUrl="http://legato" />)
    await screen.findByRole("button", { name: "Sign approvals with this device" })
    expect(screen.queryByText(/open this server in the Legato app/)).toBeNull()
    const send = (detail: unknown) => act(() => { window.dispatchEvent(new CustomEvent("legato:shell-enroll", { detail })) })
    send({ status: "enrolled" })
    expect(screen.getByRole("status").textContent).toBe("This phone now signs approvals.")
    send({ status: "pending" })
    expect(screen.getByRole("status").textContent).toBe("Approve this phone on a phone that is already enrolled.")
    send({ status: "error" })
    expect(screen.getByRole("alert").textContent).toBe("The phone could not enroll.")
    send({ status: "error", error: "no key" })
    expect(screen.getByRole("alert").textContent).toBe("no key")
    fireEvent.click(screen.getByRole("button", { name: "Sign approvals with this device" }))
    expect(screen.queryByRole("alert")).toBeNull()
    expect(screen.queryByRole("status")).toBeNull()
  })

  it("shows a failed read", async () => {
    mockList.mockRejectedValue(new Error("offline"))
    render(<DevicesSettings baseUrl="http://legato" />)
    expect((await screen.findByRole("alert", {}, { timeout: 5000 })).textContent).toBe("offline")
  })
})
