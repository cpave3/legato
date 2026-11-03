import { describe, it, expect, vi, afterEach, beforeEach } from "vitest"
import { render, screen, cleanup, fireEvent, waitFor } from "@testing-library/react"

const mockApply = vi.fn()
vi.mock("../../lib/updates", async (importActual) => ({
  ...(await importActual<typeof import("../../lib/updates")>()),
  applyUpdate: (...a: unknown[]) => mockApply(...a),
}))

import { UpdateNotice } from "../UpdateNotice"
import type { UpdateStatus } from "../../lib/updates"

const available: UpdateStatus = {
  version: "v1.2.0", release: true, mode: "ask", state: "available", available: true,
  latest: "v1.3.0", latest_url: "https://github.com/cpave3/legato/releases/tag/v1.3.0",
}

describe("UpdateNotice", () => {
  beforeEach(() => {
    localStorage.clear()
    mockApply.mockReset()
    mockApply.mockResolvedValue(undefined)
  })
  afterEach(() => cleanup())

  it("asks the person, and updates when they say so", async () => {
    render(<UpdateNotice status={available} baseUrl="http://legato" />)

    expect(screen.getByRole("status").textContent).toMatch(/Legato v1\.3\.0 is available/)
    expect(screen.getByRole("link", { name: "What's new" }).getAttribute("href")).toBe(available.latest_url)
    fireEvent.click(screen.getByRole("button", { name: "Update and restart" }))

    await waitFor(() => expect(mockApply).toHaveBeenCalledWith("http://legato"))
  })

  it("stays away for that version once put off, and comes back for the next", () => {
    const { rerender } = render(<UpdateNotice status={available} baseUrl="" />)

    fireEvent.click(screen.getByRole("button", { name: "Later" }))
    expect(screen.queryByRole("status")).toBeNull()

    rerender(<UpdateNotice status={{ ...available }} baseUrl="" />)
    expect(screen.queryByRole("status")).toBeNull()

    rerender(<UpdateNotice status={{ ...available, latest: "v1.4.0" }} baseUrl="" />)
    expect(screen.getByRole("status").textContent).toMatch(/v1\.4\.0/)
  })

  it("says an update is under way, whatever the mode", () => {
    render(<UpdateNotice status={{ ...available, mode: "auto", state: "installing" }} baseUrl="" />)

    expect(screen.getByRole("status").textContent).toMatch(/Updating Legato to v1\.3\.0/)
    expect(screen.queryByRole("button", { name: "Update and restart" })).toBeNull()
  })

  it("says to restart Legato when it cannot restart itself", () => {
    render(<UpdateNotice status={{ ...available, available: false, state: "installed" }} baseUrl="" />)

    expect(screen.getByRole("status").textContent).toMatch(/v1\.3\.0 is installed.*Restart Legato/)
  })

  it("says why an update failed, and offers to try again", async () => {
    render(<UpdateNotice status={{ ...available, state: "failed", error: "checksum does not match" }} baseUrl="" />)

    expect(screen.getByRole("status").textContent).toMatch(/checksum does not match/)
    fireEvent.click(screen.getByRole("button", { name: "Try again" }))
    await waitFor(() => expect(mockApply).toHaveBeenCalled())
  })

  it("shows nothing when there is nothing to say", () => {
    for (const status of [
      { ...available, available: false, state: "up_to_date" },
      { ...available, mode: "auto", state: "waiting" },
      { version: "dev", release: false, mode: "ask", state: "unchecked", available: false },
    ] as UpdateStatus[]) {
      const { unmount } = render(<UpdateNotice status={status} baseUrl="" />)
      expect(screen.queryByRole("status")).toBeNull()
      unmount()
    }
    render(<UpdateNotice status={undefined} baseUrl="" />)
    expect(screen.queryByRole("status")).toBeNull()
  })
})
