import { act, render, screen, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { NativeAgentPanel } from "../NativeAgentPanel"

const send = vi.fn()
let websocketHandler: ((message: unknown) => void) | undefined

vi.mock("../../hooks/useWebSocket", () => ({
  useWebSocket: () => ({
    connected: true,
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
