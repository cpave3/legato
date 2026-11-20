import { describe, it, expect, vi, afterEach, beforeEach } from "vitest"
import { render, screen, cleanup, fireEvent, waitFor, within } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import type { ConfigSetting } from "../../lib/configSettings"

const mockSet = vi.fn()
vi.mock("../../lib/configSettings", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../../lib/configSettings")>()),
  setConfigSetting: (...a: unknown[]) => mockSet(...a),
}))

import { RailSettings } from "../settings/RailSettings"

const groups = ["actions", "questions", "done", "other", "approvals", "agents", "workflows", "plans", "review", "merge", "legato"]
function settings(overrides: Record<string, Partial<ConfigSetting>> = {}): ConfigSetting[] {
  return groups.map((group) => ({
    key: `web.rail.${group}`, title: `Rail: ${group}`, kind: "bool", class: "change", mode: "live",
    value: group !== "review" && group !== "merge", source: "default", pinned: false,
    ...overrides[group],
  }))
}

function wrap(node: React.ReactNode) {
  return render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>{node}</QueryClientProvider>)
}

describe("RailSettings", () => {
  afterEach(() => cleanup())
  beforeEach(() => {
    mockSet.mockReset()
    mockSet.mockResolvedValue({})
  })

  it("shows a switch for each kind of item, with its default, under who it comes from", () => {
    wrap(<RailSettings baseUrl="http://legato" settings={settings()} />)

    const agents = screen.getByRole("group", { name: "From agents" })
    const legato = screen.getByRole("group", { name: "From Legato" })
    for (const name of ["Actions", "Questions", "Finished work", "Anything else"]) {
      expect(within(agents).getByRole("button", { name }).getAttribute("aria-pressed")).toBe("true")
    }
    expect(within(legato).getByRole("button", { name: "Approvals" }).getAttribute("aria-pressed")).toBe("true")
    expect(within(legato).getByRole("button", { name: "Ready for review" }).getAttribute("aria-pressed")).toBe("false")
    expect(within(legato).getByRole("button", { name: "Merge blockers" }).getAttribute("aria-pressed")).toBe("false")
    expect(screen.getByText(/The Attention page always lists everything/)).toBeTruthy()
  })

  it("saves a change", async () => {
    wrap(<RailSettings baseUrl="http://legato" settings={settings()} />)

    fireEvent.click(screen.getByRole("button", { name: "Merge blockers" }))
    await waitFor(() => expect(mockSet).toHaveBeenCalledWith("http://legato", "web.rail.merge", true))
    fireEvent.click(screen.getByRole("button", { name: "Questions" }))
    await waitFor(() => expect(mockSet).toHaveBeenCalledWith("http://legato", "web.rail.questions", false))
  })

  it("leaves a switch config.yaml sets alone", () => {
    wrap(<RailSettings baseUrl="http://legato" settings={settings({ plans: { pinned: true, value: false } })} />)

    expect((screen.getByRole("button", { name: "Plans" }) as HTMLButtonElement).disabled).toBe(true)
    expect(screen.getByText(/config.yaml sets web.rail.plans/)).toBeTruthy()
  })
})
