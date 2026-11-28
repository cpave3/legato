import { describe, it, expect, vi, afterEach, beforeEach } from "vitest"
import { render, screen, cleanup, fireEvent, waitFor } from "@testing-library/react"

const mockSetMode = vi.fn()
const mockCheck = vi.fn()
const mockApply = vi.fn()
const mockApplyTool = vi.fn()
const mockRollbackTool = vi.fn()
vi.mock("../../lib/updates", async (importActual) => ({
  ...(await importActual<typeof import("../../lib/updates")>()),
  setUpdateMode: (...a: unknown[]) => mockSetMode(...a),
  checkForUpdates: (...a: unknown[]) => mockCheck(...a),
  applyUpdate: (...a: unknown[]) => mockApply(...a),
  applyToolUpdate: (...a: unknown[]) => mockApplyTool(...a),
  rollbackTool: (...a: unknown[]) => mockRollbackTool(...a),
}))

import { UpdatesSettings } from "../UpdatesSettings"
import type { UpdateStatus } from "../../lib/updates"

const upToDate: UpdateStatus = {
  version: "v1.3.0", release: true, mode: "ask", state: "up_to_date", available: false,
  latest: "v1.3.0", checked_at: "2026-11-03T01:00:00Z",
}

describe("UpdatesSettings", () => {
  beforeEach(() => {
    for (const mock of [mockSetMode, mockCheck, mockApply, mockApplyTool, mockRollbackTool]) {
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

  it("locks the mode when config.yaml pins it", () => {
    render(<UpdatesSettings status={upToDate} baseUrl="" modeSetting={{
      key: "updates.mode", title: "Updates", kind: "enum", class: "risky", mode: "live", value: "ask", source: "config.yaml", pinned: true,
    }} />)
    expect((screen.getByRole("radio", { name: /Automatic/ }) as HTMLInputElement).disabled).toBe(true)
    expect(screen.getByTestId("source-updates.mode").textContent).toContain("config.yaml")
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

  it.each(["st", "chimera"])("shows %s beside Legato and updates it when asked", async (name) => {
    const changed = vi.fn()
    const withTool: UpdateStatus = { ...upToDate, tools: [{ name, path: `/home/me/.local/bin/${name}`, managed: true, version: "v0.1.0", latest: "v0.2.0", available: true, state: "available" }] }
    render(<UpdatesSettings status={withTool} baseUrl="http://legato" onChanged={changed} />)

    expect(screen.getByText(name)).toBeTruthy()
    expect(screen.getByText("v0.1.0")).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: `Update ${name} to v0.2.0` }))
    await waitFor(() => expect(mockApplyTool).toHaveBeenCalledWith("http://legato", name))
    await waitFor(() => expect(changed).toHaveBeenCalled())
  })

  it.each(["st", "chimera"])("says when %s is not a release it looks after, and offers no update", (name) => {
    const fromSource: UpdateStatus = { ...upToDate, tools: [{ name, managed: false, version: "dev", available: false, state: "unmanaged" }] }
    render(<UpdatesSettings status={fromSource} baseUrl="http://legato" />)
    expect(screen.getByText(/not a release/i)).toBeTruthy()
    expect(screen.queryByRole("button", { name: new RegExp(`Update ${name}`) })).toBeNull()
  })

  it("keeps st current on a Legato built from source", () => {
    const dev: UpdateStatus = { ...upToDate, version: "dev", release: false, tools: [{ name: "st", managed: true, version: "v0.1.0", latest: "v0.2.0", available: true, state: "available" }] }
    render(<UpdatesSettings status={dev} baseUrl="http://legato" />)
    expect(screen.getByText(/built from source/)).toBeTruthy()
    expect(screen.getByRole("button", { name: "Update st to v0.2.0" })).toBeTruthy()
  })

  it("shows missing Chimera beside st without offering to install it", () => {
    render(<UpdatesSettings status={{ ...upToDate, tools: [
      { name: "st", managed: true, version: "v0.2.0", available: false, state: "up_to_date" },
      { name: "chimera", managed: false, available: false, state: "missing" },
    ] }} baseUrl="" />)
    expect(screen.getByText("st")).toBeTruthy()
    expect(screen.getByText("chimera")).toBeTruthy()
    expect(screen.getByText("Not installed on this server")).toBeTruthy()
    expect(screen.queryByRole("button", { name: /Update chimera/ })).toBeNull()
  })

  it("rolls Legato's Claude Code back to its last known good version", async () => {
    const changed = vi.fn()
    render(<UpdatesSettings status={{ ...upToDate, tools: [
      { name: "claude", managed: true, version: "2.1.330", available: false, state: "up_to_date", channel: "stable", last_good: "2.1.326", can_rollback: true },
    ] }} baseUrl="http://legato" onChanged={changed} />)
    expect(screen.getByText(/last known good 2\.1\.326/)).toBeTruthy()
    expect(screen.getByText(/stable channel/)).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Roll claude back to 2.1.326" }))
    await waitFor(() => expect(mockRollbackTool).toHaveBeenCalledWith("http://legato", "claude"))
    await waitFor(() => expect(changed).toHaveBeenCalled())
  })

  it("says which Claude Code version is held back, and offers no rollback without a last known good", () => {
    render(<UpdatesSettings status={{ ...upToDate, tools: [
      { name: "claude", managed: true, version: "2.1.326", available: false, state: "up_to_date", channel: "stable", last_good: "2.1.326", held: "2.1.330" },
    ] }} baseUrl="" />)
    expect(screen.getByText(/2\.1\.330 held back/)).toBeTruthy()
    expect(screen.queryByRole("button", { name: /Roll claude back/ })).toBeNull()
  })

  it("fetches a Claude Code version that went missing again", async () => {
    render(<UpdatesSettings status={{ ...upToDate, tools: [
      { name: "claude", managed: true, version: "2.1.326", available: false, state: "failed", channel: "stable", error: "Claude Code 2.1.326 is no longer in /data/tools/claude/versions" },
    ] }} baseUrl="http://legato" />)
    expect(screen.getByText(/no longer in/)).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Fetch claude 2.1.326 again" }))
    await waitFor(() => expect(mockApplyTool).toHaveBeenCalledWith("http://legato", "claude"))
  })

  it("shows your own claude as unmanaged in its own words", () => {
    render(<UpdatesSettings status={{ ...upToDate, tools: [
      { name: "claude", path: "/opt/claude", managed: false, version: "2.1.400", available: false, state: "unmanaged", detail: "Your own claude, from updates.claude.command. Legato does not update or roll it back." },
    ] }} baseUrl="" />)
    expect(screen.getByText(/Your own claude/)).toBeTruthy()
    expect(screen.queryByText(/Not a release build/)).toBeNull()
  })
})
