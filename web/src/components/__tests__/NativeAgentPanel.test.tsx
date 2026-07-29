import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { NativeAgentPanel } from "../NativeAgentPanel"

const send = vi.fn()
let websocketHandler: ((message: unknown) => void) | undefined
let websocketConnected = true

vi.mock("../../hooks/useWebSocket", () => ({
  useWebSocket: () => ({
    connected: websocketConnected,
    send,
    subscribe: (handler: (message: unknown) => void) => {
      websocketHandler = handler
      return () => { websocketHandler = undefined }
    },
  }),
}))

vi.mock("../../hooks/useServer", () => ({
  useServer: () => ({ baseUrl: "" }),
}))

describe("NativeAgentPanel", () => {
  beforeEach(() => {
    send.mockClear()
    websocketConnected = true
  })

  it("queues a message through Chimera while the agent is working", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => String(input).endsWith("/snapshot")
      ? new Response(JSON.stringify({ id: "session-1", status: "running", messages: [], toolCalls: [] }), { status: 200 })
      : new Response("[]", { status: 200 }))

    const view = render(<NativeAgentPanel agentId="task-1" />)
    const composer = await within(view.container).findByPlaceholderText("Queue a message...")
    expect((composer as HTMLTextAreaElement).disabled).toBe(false)
    fireEvent.change(composer, { target: { value: "Check the tests next" } })
    fireEvent.click(within(view.container).getByRole("button", { name: "Queue" }))

    expect(send).toHaveBeenCalledWith({
      type: "agent_message",
      agent_id: "task-1",
      content: "Check the tests next",
      queue: true,
    })
  })

  it("shows current context usage instead of cumulative session tokens", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const url = String(input)
      if (url.endsWith("/snapshot")) {
        return new Response(JSON.stringify({
          id: "session-1",
          cwd: "/repo",
          mode: "build",
          status: "idle",
          messages: [],
          toolCalls: [],
          usage: {
            inputTokens: 3_700_000,
            outputTokens: 100_000,
            totalTokens: 3_800_000,
            stepCount: 30,
            lastStep: { inputTokens: 128_000, outputTokens: 2_000, totalTokens: 130_000 },
          },
          contextWindow: 256_000,
          usedContextTokens: 128_000,
        }), { status: 200 })
      }
      return new Response("[]", { status: 200 })
    })

    render(<NativeAgentPanel agentId="task-1" />)

    expect(await screen.findByText("128k / 256k (50%)")).not.toBeNull()
    expect(screen.queryByText(/3,800,000 tokens/)).toBeNull()

    act(() => websocketHandler?.({
      type: "chimera_event",
      agent_id: "task-1",
      content: JSON.stringify({
        eventId: "event-1",
        sessionId: "session-1",
        ts: 1,
        type: "usage_updated",
        contextWindow: 256_000,
        usedContextTokens: 192_000,
      }),
    }))

    expect(screen.getByText("192k / 256k (75%)")).not.toBeNull()
  })

  it("does not lose a live reply when the initial snapshot resolves late", async () => {
    let releaseSnapshot!: (response: Response) => void
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const url = String(input)
      if (url.endsWith("/snapshot")) {
        return new Promise<Response>((resolve) => { releaseSnapshot = resolve })
      }
      return new Response("[]", { status: 200 })
    })

    render(<NativeAgentPanel agentId="task-1" />)
    await waitFor(() => expect(websocketHandler).toBeDefined())
    act(() => websocketHandler?.({
      type: "chimera_event",
      agent_id: "task-1",
      content: JSON.stringify({
        eventId: "event-live",
        sessionId: "session-1",
        ts: 1,
        type: "assistant_text_done",
        id: "reply-1",
        text: "Reply that arrived during hydration",
      }),
    }))
    releaseSnapshot(new Response(JSON.stringify({
      id: "session-1",
      mode: "build",
      status: "running",
      messages: [{ role: "user", content: "system prompt" }],
      toolCalls: [],
    }), { status: 200 }))

    expect(await screen.findByText("Reply that arrived during hydration")).not.toBeNull()
    expect(screen.getAllByText("Reply that arrived during hydration")).toHaveLength(1)
  })

  it("resubscribes with the last accepted cursor after reconnect", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => String(input).endsWith("/snapshot")
      ? new Response(JSON.stringify({ id: "session-1", status: "idle", messages: [], toolCalls: [] }), { status: 200 })
      : new Response("[]", { status: 200 }))

    const view = render(<NativeAgentPanel agentId="task-1" />)
    await waitFor(() => expect(websocketHandler).toBeDefined())
    act(() => websocketHandler?.({
      type: "chimera_event",
      agent_id: "task-1",
      content: JSON.stringify({ eventId: "cursor-1", type: "assistant_text_done", id: "reply", text: "Received" }),
    }))

    websocketConnected = false
    view.rerender(<NativeAgentPanel agentId="task-1" />)
    websocketConnected = true
    view.rerender(<NativeAgentPanel agentId="task-1" />)

    expect(send).toHaveBeenLastCalledWith({ type: "subscribe_agent", agent_id: "task-1", cursor: "cursor-1" })
  })

  it("repairs from snapshot when the server cannot replay its cursor", async () => {
    let snapshotText = "Before repair"
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => String(input).endsWith("/snapshot")
      ? new Response(JSON.stringify({
          id: "session-1",
          status: "idle",
          messages: [
            { role: "user", content: "system" },
            { role: "assistant", content: [{ type: "text", text: snapshotText }] },
          ],
          toolCalls: [],
        }), { status: 200 })
      : new Response("[]", { status: 200 }))

    render(<NativeAgentPanel agentId="task-1" />)
    expect(await screen.findByText("Before repair")).not.toBeNull()
    snapshotText = "After repair"
    act(() => websocketHandler?.({ type: "chimera_sync_required", agent_id: "task-1" }))

    expect(await screen.findByText("After repair")).not.toBeNull()
  })

  it("repairs from the authoritative snapshot after a run finishes", async () => {
    let snapshot: { id: string; status: string; messages: unknown[]; toolCalls: unknown[] } = {
      id: "session-1", status: "running", messages: [], toolCalls: [],
    }
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => String(input).endsWith("/snapshot")
      ? new Response(JSON.stringify(snapshot), { status: 200 })
      : new Response("[]", { status: 200 }))

    render(<NativeAgentPanel agentId="task-1" />)
    await waitFor(() => expect(websocketHandler).toBeDefined())
    snapshot = {
      id: "session-1",
      status: "idle",
      messages: [
        { role: "user", content: "system" },
        { role: "assistant", content: [{ type: "text", text: "Authoritative final reply" }] },
      ],
      toolCalls: [],
    }
    act(() => websocketHandler?.({
      type: "chimera_event",
      agent_id: "task-1",
      content: JSON.stringify({ eventId: "finish-1", type: "run_finished" }),
    }))

    expect(await screen.findByText("Authoritative final reply", {}, { timeout: 1000 })).not.toBeNull()
  })

  it("refreshes from snapshot from the overflow menu and retains content on failure", async () => {
    let refreshFails = false
    let snapshotText = "Initial reply"
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      if (!String(input).endsWith("/snapshot")) return new Response("[]", { status: 200 })
      if (refreshFails) return new Response("failed", { status: 502 })
      return new Response(JSON.stringify({
        id: "session-1",
        status: "idle",
        messages: [
          { role: "user", content: "system" },
          { role: "assistant", content: [{ type: "text", text: snapshotText }] },
        ],
        toolCalls: [],
      }), { status: 200 })
    })

    const view = render(<NativeAgentPanel agentId="task-1" />)
    expect(await within(view.container).findByText("Initial reply")).not.toBeNull()
    snapshotText = "Refreshed reply"
    fireEvent.click(within(view.container).getByTitle("More actions"))
    fireEvent.click(within(view.container).getByRole("button", { name: "Refresh from snapshot" }))
    expect(await within(view.container).findByText("Refreshed reply")).not.toBeNull()

    refreshFails = true
    fireEvent.click(within(view.container).getByTitle("More actions"))
    fireEvent.click(within(view.container).getByRole("button", { name: "Refresh from snapshot" }))
    act(() => websocketHandler?.({
      type: "chimera_event",
      agent_id: "task-1",
      content: JSON.stringify({ eventId: "during-failure", type: "assistant_text_done", id: "live", text: "Live during failed refresh" }),
    }))
    expect(await within(view.container).findByText("snapshot: 502")).not.toBeNull()
    expect(within(view.container).getByText("Refreshed reply")).not.toBeNull()
    expect(within(view.container).getByText("Live during failed refresh")).not.toBeNull()
  })

  it("renders a snapshot tool call only at the assistant message that made it", async () => {
    const snapshot = {
      id: "session-1",
      cwd: "/repo",
      mode: "build",
      status: "idle",
      messages: [
        { role: "user", content: "system prompt" },
        { role: "assistant", content: [{ type: "text", text: "First update" }] },
        { role: "assistant", content: [{ type: "text", text: "Second update" }] },
        {
          role: "assistant",
          content: [
            { type: "text", text: "Writing the file" },
            {
              type: "tool-call",
              toolCallId: "provider-call-1",
              toolName: "write",
              input: { path: "/tmp/result.txt", content: "done" },
            },
          ],
        },
        {
          role: "tool",
          content: [{
            type: "tool-result",
            toolCallId: "provider-call-1",
            toolName: "write",
            output: { type: "json", value: { bytes_written: 4, created: true } },
          }],
        },
        { role: "assistant", content: [{ type: "text", text: "Fourth update" }] },
        { role: "assistant", content: [{ type: "text", text: "Fifth update" }] },
      ],
      toolCalls: [{
        callId: "chimera-call-1",
        name: "write",
        args: { path: "/tmp/result.txt", content: "done" },
        result: { bytes_written: 4, created: true },
      }],
    }

    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const url = String(input)
      if (url.endsWith("/snapshot")) {
        return new Response(JSON.stringify(snapshot), { status: 200 })
      }
      return new Response("[]", { status: 200 })
    })

    render(<NativeAgentPanel agentId="task-1" />)

    await waitFor(() => expect(screen.queryByText("Loading Chimera session...")).toBeNull())

    expect(screen.getAllByText("write", { selector: "span" })).toHaveLength(1)
    expect(screen.getByText("result.txt")).not.toBeNull()
    for (const text of ["First update", "Second update", "Writing the file", "Fourth update", "Fifth update"]) {
      expect(screen.getByText(text)).not.toBeNull()
    }
  })
})
