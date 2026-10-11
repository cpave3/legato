import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { AgentArtifactPanel } from "../AgentArtifactPanel"
import type { AgentUIItem } from "../../lib/agentUi"

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

const base = { task_id: "task-1", created_at: "2026-10-11 01:00:00", updated_at: "2026-10-11 01:00:00", resource_domains: [] }
const design: AgentUIItem = { ...base, id: "ui-aaaaaaaaaa", placement: "panel", format: "markdown", title: "Sync design", revision: 2, oldest_revision: 1 }
const table: AgentUIItem = { ...base, id: "ui-bbbbbbbbbb", placement: "panel", format: "markdown", title: "API table", revision: 1, oldest_revision: 1 }
const weather: AgentUIItem = { ...base, id: "ui-cccccccccc", placement: "inline", format: "components", title: "Weather", revision: 1, oldest_revision: 1 }

const contents: Record<string, string> = {
  "ui-aaaaaaaaaa?revision=0": "# Design, second draft",
  "ui-aaaaaaaaaa?revision=2": "# Design, second draft",
  "ui-aaaaaaaaaa?revision=1": "# Design, first draft",
  "ui-bbbbbbbbbb?revision=0": "# The API table",
  "ui-cccccccccc?revision=0": JSON.stringify([{ type: "stat", label: "Now", value: "27 C" }]),
}

function mockAPI(items: AgentUIItem[]) {
  return vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    const url = String(input)
    if (url.endsWith("/api/agents/task-1/ui-items")) return new Response(JSON.stringify({ items }), { status: 200 })
    const match = /\/api\/ui-items\/([^?]+)(?:\?revision=(\d+))?/.exec(url)
    if (match && init?.method === "DELETE") return new Response(JSON.stringify({ status: "ok" }), { status: 200 })
    const item = match && items.find((candidate) => candidate.id === match[1])
    const revision = Number(match?.[2] ?? 0)
    const content = match && contents[`${match[1]}?revision=${revision}`]
    if (!item || content === undefined) return new Response(JSON.stringify({ error: "item not found" }), { status: 404 })
    return new Response(JSON.stringify({ ...item, revision: revision || item.revision, content }), { status: 200 })
  })
}

describe("AgentArtifactPanel", () => {
  it("lists what the session showed and draws the one in focus", async () => {
    mockAPI([design, table, weather])
    render(<AgentArtifactPanel agentId="task-1" focus={{ itemId: "ui-bbbbbbbbbb", revision: 0 }} onFocus={() => {}} onClose={() => {}} mobile={false} />, { wrapper })

    const tabs = await screen.findAllByRole("tab")
    expect(tabs.map((tab) => tab.textContent)).toEqual(["Sync designr2", "API table", "Weather"])
    expect(screen.getByRole("tab", { selected: true }).textContent).toBe("API table")
    expect(await screen.findByRole("heading", { name: "The API table" })).toBeTruthy()
  })

  it("draws the newest artifact when nothing is in focus", async () => {
    mockAPI([design, table, weather])
    render(<AgentArtifactPanel agentId="task-1" focus={null} onFocus={() => {}} onClose={() => {}} mobile={false} />, { wrapper })

    // Artifacts come before cards; the newest artifact is the last of them.
    expect(await screen.findByRole("heading", { name: "The API table" })).toBeTruthy()
  })

  it("asks to change focus when another item is picked", async () => {
    mockAPI([design, table, weather])
    const onFocus = vi.fn()
    render(<AgentArtifactPanel agentId="task-1" focus={{ itemId: "ui-bbbbbbbbbb", revision: 0 }} onFocus={onFocus} onClose={() => {}} mobile={false} />, { wrapper })

    fireEvent.click(await screen.findByRole("tab", { name: /Weather/ }))

    expect(onFocus).toHaveBeenCalledWith("ui-cccccccccc", 0)
  })

  it("steps back through an artifact's revisions", async () => {
    mockAPI([design])
    const onFocus = vi.fn()
    const { rerender } = render(<AgentArtifactPanel agentId="task-1" focus={{ itemId: "ui-aaaaaaaaaa", revision: 0 }} onFocus={onFocus} onClose={() => {}} mobile={false} />, { wrapper })
    expect(await screen.findByRole("heading", { name: "Design, second draft" })).toBeTruthy()
    expect(screen.getByText("Revision 2 of 2")).toBeTruthy()
    expect((screen.getByRole("button", { name: "Next revision" }) as HTMLButtonElement).disabled).toBe(true)

    fireEvent.click(screen.getByRole("button", { name: "Previous revision" }))
    expect(onFocus).toHaveBeenCalledWith("ui-aaaaaaaaaa", 1)

    rerender(<AgentArtifactPanel agentId="task-1" focus={{ itemId: "ui-aaaaaaaaaa", revision: 1 }} onFocus={onFocus} onClose={() => {}} mobile={false} />)
    expect(await screen.findByRole("heading", { name: "Design, first draft" })).toBeTruthy()
    expect(screen.getByText("Revision 1 of 2")).toBeTruthy()
    expect((screen.getByRole("button", { name: "Previous revision" }) as HTMLButtonElement).disabled).toBe(true)
  })

  it("copies an artifact's source", async () => {
    mockAPI([design])
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true })
    render(<AgentArtifactPanel agentId="task-1" focus={{ itemId: "ui-aaaaaaaaaa", revision: 0 }} onFocus={() => {}} onClose={() => {}} mobile={false} />, { wrapper })
    await screen.findByRole("heading", { name: "Design, second draft" })

    fireEvent.click(screen.getByRole("button", { name: "Copy source" }))

    await waitFor(() => expect(writeText).toHaveBeenCalledWith("# Design, second draft"))
  })

  it("removes an artifact only after the person confirms", async () => {
    const fetchMock = mockAPI([design])
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(false)
    render(<AgentArtifactPanel agentId="task-1" focus={{ itemId: "ui-aaaaaaaaaa", revision: 0 }} onFocus={() => {}} onClose={() => {}} mobile={false} />, { wrapper })
    await screen.findByRole("heading", { name: "Design, second draft" })
    const deletes = () => fetchMock.mock.calls.filter(([, init]) => init?.method === "DELETE")

    fireEvent.click(screen.getByRole("button", { name: "Remove" }))
    expect(confirm).toHaveBeenCalledTimes(1)
    expect(deletes()).toHaveLength(0)

    confirm.mockReturnValue(true)
    fireEvent.click(screen.getByRole("button", { name: "Remove" }))
    await waitFor(() => expect(deletes()).toHaveLength(1))
    expect(String(deletes()[0][0])).toContain("/api/ui-items/ui-aaaaaaaaaa")
  })

  it("says so when the session has shown nothing", async () => {
    mockAPI([])
    render(<AgentArtifactPanel agentId="task-1" focus={null} onFocus={() => {}} onClose={() => {}} mobile={false} />, { wrapper })

    expect(await screen.findByText(/has not shown anything yet/i)).toBeTruthy()
  })

  it("is a full-screen dialog on a phone, with a close control large enough to tap", async () => {
    mockAPI([design])
    const onClose = vi.fn()
    render(<AgentArtifactPanel agentId="task-1" focus={null} onFocus={() => {}} onClose={onClose} mobile />, { wrapper })

    const dialog = await screen.findByRole("dialog", { name: "Artifacts" })
    expect(dialog.className).toContain("fixed")
    expect(dialog.className).toContain("inset-0")
    // The app's bottom navigation is fixed at z-40; a sheet that covers the
    // screen covers it too.
    expect(dialog.className).toContain("z-50")
    // A global rule keeps dialogs a margin away from a phone's screen edges.
    // This sheet is the whole screen, so it sets that rule aside.
    expect(dialog.style.maxWidth).toBe("none")
    expect(dialog.style.maxHeight).toBe("none")
    const close = within(dialog).getByRole("button", { name: "Close artifacts" })
    expect(close.className).toContain("h-11")
    fireEvent.click(close)
    expect(onClose).toHaveBeenCalledTimes(1)

    fireEvent.keyDown(dialog, { key: "Escape" })
    expect(onClose).toHaveBeenCalledTimes(2)
  })

  it("is a resizable column beside the conversation on a wide screen", async () => {
    mockAPI([design])
    render(<AgentArtifactPanel agentId="task-1" focus={null} onFocus={() => {}} onClose={() => {}} mobile={false} />, { wrapper })

    const panel = await screen.findByRole("complementary", { name: "Artifacts" })
    expect(panel.className).not.toContain("fixed")
    const separator = within(panel).getByRole("separator", { name: "Resize artifacts" })
    const before = panel.style.width
    fireEvent.keyDown(separator, { key: "ArrowLeft" })
    expect(panel.style.width).not.toBe(before)
  })
})
