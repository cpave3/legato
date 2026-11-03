import { describe, it, expect, vi, afterEach, beforeEach } from "vitest"
import { render, screen, cleanup, fireEvent, waitFor } from "@testing-library/react"

const mockSetMode = vi.fn()
const mockCheck = vi.fn()
const mockApply = vi.fn()
vi.mock("../../lib/updates", async (importActual) => ({
  ...(await importActual<typeof import("../../lib/updates")>()),
  setUpdateMode: (...a: unknown[]) => mockSetMode(...a),
  checkForUpdates: (...a: unknown[]) => mockCheck(...a),
  applyUpdate: (...a: unknown[]) => mockApply(...a),
}))

import { UpdatesSettings } from "../UpdatesSettings"
import type { UpdateStatus } from "../../lib/updates"

const upToDate: UpdateStatus = {
  version: "v1.3.0", release: true, mode: "ask", state: "up_to_date", available: false,
  latest: "v1.3.0", checked_at: "2026-11-03T01:00:00Z",
}

describe("UpdatesSettings", () => {
  beforeEach(() => {
    for (const mock of [mockSetMode, mockCheck, mockApply]) {
      mock.mockReset()
      mock.mockResolvedValue(undefined)
    }
  })
  afterEach(() => cleanup())

  it("shows the version and how updates are taken, and changes it", async () => {
    const changed = vi.fn()
    render(<UpdatesSettings status={upToDate} baseUrl="http://legato" onChanged={changed} />)

    expect(screen.getByText("v1.3.0", { selector: "[data-testid=current-version]" })).toBeTruthy()
    expect(screen.getByText(/up to date/i)).toBeTruthy()
    expect((screen.getByRole("radio", { name: /Ask/ }) as HTMLInputElement).checked).toBe(true)

    fireEvent.click(screen.getByRole("radio", { name: /Automatic/ }))

    await waitFor(() => expect(mockSetMode).toHaveBeenCalledWith("http://legato", "auto"))
    await waitFor(() => expect(changed).toHaveBeenCalled())
  })

  it("checks now", async () => {
    render(<UpdatesSettings status={upToDate} baseUrl="" />)

    fireEvent.click(screen.getByRole("button", { name: "Check now" }))

    await waitFor(() => expect(mockCheck).toHaveBeenCalledWith(""))
  })

  it("offers a newer release", async () => {
    render(<UpdatesSettings status={{ ...upToDate, version: "v1.2.0", available: true, state: "available" }} baseUrl="" />)

    fireEvent.click(screen.getByRole("button", { name: "Update to v1.3.0" }))

    await waitFor(() => expect(mockApply).toHaveBeenCalledWith(""))
  })

  it("says a build from source does not update itself", () => {
    render(<UpdatesSettings status={{ version: "dev", release: false, mode: "ask", state: "unchecked", available: false }} baseUrl="" />)

    expect(screen.getByText(/built from source/)).toBeTruthy()
    expect(screen.queryByRole("radio")).toBeNull()
    expect(screen.queryByRole("button", { name: "Check now" })).toBeNull()
  })

  it("says when the server cannot update itself", () => {
    render(<UpdatesSettings status={null} baseUrl="" />)

    expect(screen.getByText(/does not update itself/)).toBeTruthy()
  })
})
