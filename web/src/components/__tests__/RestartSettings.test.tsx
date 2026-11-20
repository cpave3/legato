import "@testing-library/jest-dom/vitest"
import { describe, it, expect, vi, afterEach, beforeEach } from "vitest"
import { render, screen, cleanup, fireEvent, waitFor, within } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { MemoryRouter } from "react-router-dom"
import type { ReactNode } from "react"

const mockRestart = vi.fn()
const mockSet = vi.fn()
const mockReset = vi.fn()
vi.mock("../../lib/restart", () => ({ restartLegato: (...a: unknown[]) => mockRestart(...a) }))
vi.mock("../../lib/configSettings", async (importActual) => ({
  ...(await importActual<typeof import("../../lib/configSettings")>()),
  setConfigSetting: (...a: unknown[]) => mockSet(...a),
  resetConfigSetting: (...a: unknown[]) => mockReset(...a),
}))

import { RestartBar, RestartCard } from "../settings/RestartSettings"
import { waitingChanges } from "../../lib/pendingRestart"
import type { ConfigList, ConfigSetting } from "../../lib/configSettings"

const setting = (over: Partial<ConfigSetting>): ConfigSetting => ({
  key: "swarm.max_concurrent_agents", title: "Workers at once", section: "swarms", group: "Limits", kind: "int", class: "change", mode: "restart",
  value: 6, default: 0, default_label: "4", started_value: 0, source: "database", pinned: false, pending_restart: true, ...over,
})

function wrap(children: ReactNode) {
  return <QueryClientProvider client={new QueryClient()}><MemoryRouter>{children}</MemoryRouter></QueryClientProvider>
}

describe("Waiting for a restart", () => {
  beforeEach(() => { mockRestart.mockReset(); mockSet.mockReset(); mockReset.mockReset(); mockSet.mockResolvedValue({}); mockReset.mockResolvedValue({}) })
  afterEach(() => cleanup())

  it("names settings and list entries that wait, by section", () => {
    const lists: ConfigList[] = [{ key: "workspaces", title: "Workspaces", class: "change", mode: "restart", entries: [], pending_entries: ["infra"] }]
    expect(waitingChanges([setting({}), setting({ key: "agents.reviews", title: "Review tour at the end", section: "agents", pending_restart: false })], lists).map((c) => c.name))
      .toEqual(["Swarms › Workers at once", "Board and tickets › Workspaces › infra"])
  })

  it("shows no bar while nothing waits", () => {
    render(wrap(<RestartBar changes={[]} baseUrl="" />))
    expect(screen.queryByRole("region", { name: "Changes waiting for a restart" })).toBeNull()
  })

  it("names up to three changes, counts the rest, and restarts after the person confirms", async () => {
    mockRestart.mockResolvedValue(undefined)
    const settings = ["a", "b", "c", "d", "e"].map((k) => setting({ key: `swarm.${k}`, title: k.toUpperCase() }))
    render(wrap(<RestartBar changes={waitingChanges(settings, [])} baseUrl="http://legato" />))
    const bar = within(screen.getByRole("region", { name: "Changes waiting for a restart" }))
    expect(bar.getByText(/5 changes apply when Legato restarts/)).toBeTruthy()
    expect(bar.getByText(/Swarms › A, Swarms › B, Swarms › C and 2 more/)).toBeTruthy()
    expect(bar.getByRole("link", { name: "Show" })).toHaveAttribute("href", "/#system/restart")
    fireEvent.click(bar.getByRole("button", { name: "Restart Legato" }))
    expect(mockRestart).not.toHaveBeenCalled()
    fireEvent.click(bar.getByRole("button", { name: "Restart" }))
    await waitFor(() => expect(mockRestart).toHaveBeenCalledWith("http://legato"))
    expect(bar.getByRole("status").textContent).toMatch(/Restarting/)
  })

  it("says why a restart was refused", async () => {
    mockRestart.mockRejectedValue(new Error("this Legato cannot restart itself"))
    render(wrap(<RestartCard changes={[]} baseUrl="" />))
    expect(screen.getByText("Nothing waits for a restart.")).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Restart Legato" }))
    fireEvent.click(screen.getByRole("button", { name: "Restart" }))
    expect((await screen.findByRole("alert")).textContent).toMatch(/cannot restart/)
  })

  it("lists each waiting change with its value then and now, and Undo resets one that started at its default", async () => {
    render(wrap(<RestartCard changes={waitingChanges([setting({})], [])} baseUrl="http://legato" />))
    const list = within(screen.getByRole("list", { name: "Changes waiting for a restart" }))
    expect(list.getByText("4 → 6")).toBeTruthy()
    fireEvent.click(list.getByRole("button", { name: "Undo Swarms › Workers at once" }))
    await waitFor(() => expect(mockReset).toHaveBeenCalledWith("http://legato", "swarm.max_concurrent_agents"))
    expect(mockSet).not.toHaveBeenCalled()
  })

  it("Undo sets back a value that started set here", async () => {
    render(wrap(<RestartCard changes={waitingChanges([setting({ key: "jira.sync_interval_seconds", title: "Sync every", section: "board", unit: "seconds", value: 120, default: 60, default_label: undefined, started_value: 90 })], [])} baseUrl="http://legato" />))
    expect(screen.getByText("90 seconds → 120 seconds")).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: /^Undo/ }))
    await waitFor(() => expect(mockSet).toHaveBeenCalledWith("http://legato", "jira.sync_interval_seconds", 90))
  })

  it("lists a waiting list entry by name, without Undo", () => {
    const lists: ConfigList[] = [{ key: "workspaces", title: "Workspaces", class: "change", mode: "restart", entries: [], pending_entries: ["infra"] }]
    render(wrap(<RestartCard changes={waitingChanges([], lists)} baseUrl="" />))
    expect(screen.getByText("Board and tickets › Workspaces › infra")).toBeTruthy()
    expect(screen.queryByRole("button", { name: /^Undo/ })).toBeNull()
  })
})
