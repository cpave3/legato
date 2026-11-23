import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
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

// A native agent's panel, with the answer the compact route gives.
function panelWith(compact: Response) {
  const compactions: { url: string; body: unknown }[] = []
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    const url = String(input)
    if (url.includes("/snapshot")) {
      return new Response(JSON.stringify({ id: "session-1", status: "idle", messages: [], toolCalls: [] }), { status: 200 })
    }
    if (url.endsWith("/compact") && init?.method === "POST") {
      compactions.push({ url, body: JSON.parse(String(init.body)) })
      return compact
    }
    return new Response("[]", { status: 200 })
  })
  render(<NativeAgentPanel agentId="task-1" agentKind="claude-code" />)
  return compactions
}

function type(text: string) {
  const composer = screen.getByRole("textbox", { name: "Message" })
  fireEvent.change(composer, { target: { value: text } })
  fireEvent.keyDown(composer, { key: "Enter" })
}

function event(content: object) {
  act(() => websocketHandler?.({ type: "chimera_event", agent_id: "task-1", content: JSON.stringify(content) }))
}

describe("compacting from the panel", () => {
  beforeEach(() => send.mockReset())
  afterEach(() => {
    cleanup()
    vi.restoreAllMocks()
  })

  it("runs /compact with its focus as the agent's own compaction, never as a message", async () => {
    const compactions = panelWith(new Response(null, { status: 202 }))
    await screen.findByText("Session ready — send a message to start")

    type("/compact keep the plan")

    await waitFor(() => expect(compactions).toEqual([{ url: "/api/agents/task-1/compact", body: { instructions: "keep the plan" } }]))
    expect(send).not.toHaveBeenCalledWith(expect.objectContaining({ type: "agent_message" }))
  })

  it("shows the compaction running, then what it saved", async () => {
    panelWith(new Response(null, { status: 202 }))
    await screen.findByText("Session ready — send a message to start")

    event({ type: "compaction_started", reason: "manual" })
    expect(await screen.findByText("Compacting the conversation…")).toBeTruthy()

    event({ type: "compaction_finished", tokensBefore: 49805, tokensAfter: 6100 })
    expect(await screen.findByText("Compacted: 49.8k → 6.1k tokens")).toBeTruthy()
  })

  it("says why Legato could not compact the agent", async () => {
    panelWith(new Response(JSON.stringify({ error: "Codex compacts with its own prompt only and takes no focus instructions. Compact without them." }), { status: 422 }))
    await screen.findByText("Session ready — send a message to start")

    type("/compact keep the plan")

    expect(await screen.findByText(/Codex compacts with its own prompt only/)).toBeTruthy()
  })
})
