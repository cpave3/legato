import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { NativeAgentPanel } from "../NativeAgentPanel"
import { proposeComposerText } from "../../lib/agentUi"

vi.mock("../../hooks/useWebSocket", () => ({
  useWebSocket: () => ({ connected: true, send: vi.fn(), subscribe: () => () => {} }),
}))

vi.mock("../../hooks/useServer", () => ({
  useServer: () => ({ baseUrl: "" }),
}))

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  // The panel keeps each agent's unsent draft.
  localStorage.clear()
})

function wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } })
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>
}

const shown = (id: string) => `Shown as a card in the conversation [legato-ui:${id}:1]. The person can see it now; do not repeat its content in your reply.`

function mockSession(toolCalls: unknown[]) {
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const url = String(input)
    if (url.includes("/snapshot")) {
      return new Response(JSON.stringify({
        id: "task-1", status: "idle", pendingPermissions: [],
        // A snapshot's first user message is the session's own prompt, and
        // its tool calls sit where an assistant message made them.
        messages: [
          { role: "user", content: "You are an agent." },
          { role: "user", content: "What is the weather in Brisbane?" },
          { role: "assistant", content: toolCalls.map(() => ({ type: "tool-call" })) },
        ],
        toolCalls,
      }), { status: 200 })
    }
    if (url.includes("/api/ui-items/ui-0123456789")) {
      return new Response(JSON.stringify({
        id: "ui-0123456789", task_id: "task-1", placement: "inline", format: "components", title: "Weather in Brisbane",
        revision: 1, oldest_revision: 1, fallback_text: "27 C and sunny",
        content: JSON.stringify([{ type: "stat", label: "Now", value: "27 C" }, { type: "actions", items: [{ label: "Forecast", message: "Show the 7 day forecast" }] }]),
        created_at: "", updated_at: "",
      }), { status: 200 })
    }
    return new Response("[]", { status: 200 })
  })
}

describe("NativeAgentPanel and what an agent shows", () => {
  it("draws a card in the conversation where the agent's display call was made", async () => {
    mockSession([
      { callId: "c1", name: "Read", args: { path: "/tmp/a" }, result: { ok: true } },
      { callId: "c2", name: "mcp__plugin_legato-tools_legato__show_components", args: { fallback_text: "27 C and sunny", title: "Weather in Brisbane" }, result: { output: shown("ui-0123456789") } },
    ])
    render(<NativeAgentPanel agentId="task-1" />, { wrapper })

    expect(await screen.findByRole("group", { name: "Card from the agent: Weather in Brisbane" })).toBeTruthy()
    expect(screen.getByText("27 C")).toBeTruthy()
    // The call is the card: its long tool name is not shown as a row too.
    expect(screen.queryByText(/show_components/)).toBeNull()
  })

  it("keeps the ordinary tool row for a display call that failed, so its error can be read", async () => {
    mockSession([
      { callId: "c2", name: "legato.show_card", args: { html: "<b>x</b>" }, result: { error: "invalid agent UI item: a card needs fallback_text" } },
    ])
    render(<NativeAgentPanel agentId="task-1" />, { wrapper })

    expect(await screen.findByText(/legato\.show_card/)).toBeTruthy()
    expect(screen.queryByRole("group", { name: /Card from the agent/ })).toBeNull()
  })

  it("opens an artifact in the side panel from its line in the conversation", async () => {
    mockSession([
      { callId: "c3", name: "legato.show_artifact", args: { title: "Sync design" }, result: { output: "Shown in Legato's side panel as \"Sync design\" [legato-ui:ui-aaaaaaaaaa:2]." } },
    ])
    const onOpenUIItem = vi.fn()
    render(<NativeAgentPanel agentId="task-1" onOpenUIItem={onOpenUIItem} />, { wrapper })

    fireEvent.click(await screen.findByRole("button", { name: /Sync design/ }))

    expect(onOpenUIItem).toHaveBeenCalledWith("ui-aaaaaaaaaa", 2)
  })

  it("puts a message proposed by a card in the message box and does not send it", async () => {
    mockSession([
      { callId: "c2", name: "legato.show_components", args: { fallback_text: "27 C and sunny" }, result: { output: shown("ui-0123456789") } },
    ])
    render(<NativeAgentPanel agentId="task-1" />, { wrapper })
    const composer = await screen.findByPlaceholderText("Message task-1") as HTMLTextAreaElement
    fireEvent.change(composer, { target: { value: "Thanks." } })

    fireEvent.click(await screen.findByRole("button", { name: "Forecast" }))

    await waitFor(() => expect(composer.value).toBe("Thanks.\nShow the 7 day forecast"))
    expect(screen.queryByText("Show the 7 day forecast", { selector: "div" })).toBeNull()
  })

  it("ignores a message proposed for another agent", async () => {
    mockSession([])
    render(<NativeAgentPanel agentId="task-1" />, { wrapper })
    const composer = await screen.findByPlaceholderText("Message task-1") as HTMLTextAreaElement

    act(() => proposeComposerText("task-2", "not for this agent"))

    expect(composer.value).toBe("")
  })
})

describe("NativeAgentPanel artifacts button", () => {
  it("offers the session's artifacts from the toolbar, marking new ones", async () => {
    mockSession([])
    const onToggle = vi.fn()
    render(<NativeAgentPanel agentId="task-1" artifacts={{ count: 2, fresh: 1, onToggle }} />, { wrapper })
    await screen.findByPlaceholderText("Message task-1")

    const button = screen.getByRole("button", { name: "Artifacts (2), 1 new" })
    expect(button.textContent).toContain("2")
    fireEvent.click(button)

    expect(onToggle).toHaveBeenCalledTimes(1)
  })

  it("shows no artifacts button for a session that has shown none", async () => {
    mockSession([])
    render(<NativeAgentPanel agentId="task-1" artifacts={{ count: 0, fresh: 0, onToggle: vi.fn() }} />, { wrapper })
    await screen.findByPlaceholderText("Message task-1")

    expect(screen.queryByRole("button", { name: /Artifacts/ })).toBeNull()
  })
})
