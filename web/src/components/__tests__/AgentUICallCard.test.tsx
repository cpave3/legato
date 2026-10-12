import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { AgentUICallCard } from "../AgentUICallCard"
import { onComposerProposal, type AgentUIItem, type LegatoUICall } from "../../lib/agentUi"

vi.mock("../../hooks/useServer", () => ({
  useServer: () => ({ baseUrl: "" }),
}))

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

function wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } })
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>
}

function item(overrides: Partial<AgentUIItem>): AgentUIItem {
  return {
    id: "ui-0123456789", task_id: "task-1", placement: "inline", format: "html", title: "Weather in Brisbane",
    revision: 1, oldest_revision: 1, content: "<b>27 C</b>", fallback_text: "27 C and sunny", resource_domains: [],
    created_at: "2026-10-11 01:00:00", updated_at: "2026-10-11 01:00:00", ...overrides,
  }
}

// Answers the item API from a table of "<id>?revision=<n>" to an item or a status.
function mockItems(items: Record<string, AgentUIItem | number>) {
  return vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const match = /\/api\/ui-items\/([^?]+)(?:\?revision=(\d+))?/.exec(String(input))
    const found = match ? items[`${match[1]}?revision=${match[2] ?? 0}`] : undefined
    if (found === undefined || typeof found === "number") {
      return new Response(JSON.stringify({ error: "item not found" }), { status: typeof found === "number" ? found : 404 })
    }
    return new Response(JSON.stringify(found), { status: 200 })
  })
}

const shownCard: LegatoUICall = { tool: "show_card", state: "shown", itemId: "ui-0123456789", revision: 1, title: "Weather in Brisbane", fallbackText: "27 C and sunny" }

describe("AgentUICallCard", () => {
  it("draws an HTML card from the item the call made, at the revision it made", async () => {
    const fetchMock = mockItems({ "ui-0123456789?revision=1": item({}) })
    render(<AgentUICallCard call={shownCard} agentId="task-1" />, { wrapper })

    const frame = await screen.findByTitle("Weather in Brisbane") as HTMLIFrameElement
    expect(frame.getAttribute("sandbox")).toBe("allow-scripts")
    expect(frame.getAttribute("srcdoc")).toContain("<b>27 C</b>")
    expect(fetchMock.mock.calls.some(([url]) => String(url).endsWith("/api/ui-items/ui-0123456789?revision=1"))).toBe(true)
  })

  it("draws a component card with Legato's own elements", async () => {
    mockItems({ "ui-0123456789?revision=1": item({ format: "components", content: JSON.stringify([{ type: "stat", label: "Now", value: "27 C" }]) }) })
    render(<AgentUICallCard call={{ ...shownCard, tool: "show_components" }} agentId="task-1" />, { wrapper })

    expect(await screen.findByText("27 C")).toBeTruthy()
    expect(screen.queryByTitle("Weather in Brisbane")).toBeNull()
    expect(screen.getByRole("group", { name: "Card from the agent: Weather in Brisbane" })).toBeTruthy()
  })

  it("says a card is on its way while the call is still running", () => {
    mockItems({})
    render(<AgentUICallCard call={{ tool: "show_card", state: "pending", title: "", fallbackText: "" }} agentId="task-1" />, { wrapper })

    expect(screen.getByRole("status").textContent).toContain("Preparing a card")
  })

  it("falls back to the plain text when the item is no longer stored", async () => {
    mockItems({ "ui-0123456789?revision=1": 404 })
    render(<AgentUICallCard call={shownCard} agentId="task-1" />, { wrapper })

    expect(await screen.findByText("27 C and sunny")).toBeTruthy()
    expect(screen.getByText(/no longer stored/i)).toBeTruthy()
  })

  it("puts a message a card proposes in that agent's composer channel", async () => {
    mockItems({ "ui-0123456789?revision=1": item({ format: "components", content: JSON.stringify([{ type: "actions", items: [{ label: "Forecast", message: "Show the 7 day forecast" }] }]) }) })
    const proposed: string[] = []
    const stop = onComposerProposal("task-1", (text) => proposed.push(text))
    const other = onComposerProposal("task-2", (text) => proposed.push(`wrong agent: ${text}`))
    render(<AgentUICallCard call={{ ...shownCard, tool: "show_components" }} agentId="task-1" />, { wrapper })

    fireEvent.click(await screen.findByRole("button", { name: "Forecast" }))

    expect(proposed).toEqual(["Show the 7 day forecast"])
    stop()
    other()
  })

  it("draws an artifact call as a line that opens the artifact in the side panel", async () => {
    mockItems({})
    const onOpen = vi.fn()
    render(<AgentUICallCard call={{ tool: "show_artifact", state: "shown", itemId: "ui-aaaaaaaaaa", revision: 3, title: "Sync design", fallbackText: "" }} agentId="task-1" onOpen={onOpen} />, { wrapper })

    fireEvent.click(screen.getByRole("button", { name: /Sync design/ }))

    expect(onOpen).toHaveBeenCalledWith("ui-aaaaaaaaaa", 3)
    expect(screen.getByText(/revision 3/i)).toBeTruthy()
  })

  it("draws a video call as a line that opens the video in the side panel", async () => {
    mockItems({})
    const onOpen = vi.fn()
    render(<AgentUICallCard call={{ tool: "show_video", state: "shown", itemId: "ui-bbbbbbbbbb", revision: 2, title: "Tour of the change", fallbackText: "" }} agentId="task-1" onOpen={onOpen} />, { wrapper })

    fireEvent.click(screen.getByRole("button", { name: /Tour of the change/ }))

    expect(onOpen).toHaveBeenCalledWith("ui-bbbbbbbbbb", 2)
    expect(screen.getByText(/Video · revision 2/)).toBeTruthy()
  })

  it("offers to open a card in the side panel", async () => {
    mockItems({ "ui-0123456789?revision=1": item({}) })
    const onOpen = vi.fn()
    render(<AgentUICallCard call={shownCard} agentId="task-1" onOpen={onOpen} />, { wrapper })
    await screen.findByTitle("Weather in Brisbane")

    fireEvent.click(screen.getByRole("button", { name: "Open in side panel" }))

    await waitFor(() => expect(onOpen).toHaveBeenCalledWith("ui-0123456789", 1))
  })
})
