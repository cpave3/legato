import { describe, expect, it, vi } from "vitest"
import { createDiscussionSession, type DiscussionSocket } from "./discussion-session"

class FakeSocket implements DiscussionSocket {
  sent: Array<Record<string, unknown>> = []
  onopen: (() => void) | null = null
  onmessage: ((event: { data: string }) => void) | null = null
  onclose: (() => void) | null = null
  closed = false
  constructor(public url: string) {}
  send(data: string): void { this.sent.push(JSON.parse(data) as Record<string, unknown>) }
  close(): void { this.closed = true; this.onclose?.() }
  open(): void { this.onopen?.() }
  receive(message: Record<string, unknown>): void { this.onmessage?.({ data: JSON.stringify(message) }) }
  event(agentId: string, event: Record<string, unknown>): void {
    this.receive({ type: "chimera_event", agent_id: agentId, content: JSON.stringify(event) })
  }
}

const snapshot = {
  status: "idle",
  messages: [
    { role: "user", content: "You are here to talk through PR #7." },
    { role: "assistant", content: "This PR adds a discussion workflow." },
  ],
  toolCalls: [],
}

function fixture(snapshotBody: unknown = snapshot) {
  const sockets: FakeSocket[] = []
  const fetcher = vi.fn().mockResolvedValue(new Response(JSON.stringify(snapshotBody), { status: 200 }))
  const changes = vi.fn()
  const session = createDiscussionSession(
    { baseUrl: "https://localhost:3080", token: "secret", taskId: "task-9" },
    {
      fetch: fetcher as unknown as typeof fetch,
      connect: (url) => { const socket = new FakeSocket(url); sockets.push(socket); return socket },
      setTimeout: (callback, ms) => setTimeout(callback, ms) as unknown as number,
      clearTimeout: (id) => clearTimeout(id),
    },
    changes,
  )
  return { session, sockets, fetcher, changes, last: () => changes.mock.calls.at(-1)![0] }
}

describe("createDiscussionSession", () => {
  it("connects to Legato's socket over wss with the token and subscribes to the agent", async () => {
    const f = fixture()
    f.session.start()
    expect(f.sockets[0].url).toBe("wss://localhost:3080/ws?token=secret")

    f.sockets[0].open()

    expect(f.sockets[0].sent[0]).toEqual({ type: "subscribe_agent", agent_id: "task-9", cursor: "" })
    await vi.waitFor(() => expect(f.last().items.map((item: { text: string }) => item.text)).toEqual(["This PR adds a discussion workflow."]))
    expect(f.fetcher).toHaveBeenCalledWith("https://localhost:3080/api/agents/task-9/chimera/snapshot?initial=true", { headers: { Authorization: "Bearer secret" } })
  })

  it("follows the agent's live events and ignores other agents'", async () => {
    const f = fixture()
    f.session.start()
    f.sockets[0].open()
    await vi.waitFor(() => expect(f.last().items).toHaveLength(1))

    f.sockets[0].event("someone-else", { eventId: "e0", type: "user_message", id: "m0", content: "not this one" })
    f.sockets[0].event("task-9", { eventId: "e1", type: "user_message", id: "m1", content: "Why a new trigger?" })

    expect(f.last().items.map((item: { text: string }) => item.text)).toEqual(["This PR adds a discussion workflow.", "Why a new trigger?"])
  })

  it("sends a message, queued while the agent is working", async () => {
    const f = fixture()
    f.session.start()
    f.sockets[0].open()
    await vi.waitFor(() => expect(f.last().items).toHaveLength(1))

    f.session.send("What does agent_ended do?")
    f.sockets[0].event("task-9", { eventId: "e1", type: "session_started" })
    f.session.send("And the badge?")

    expect(f.sockets[0].sent.slice(1)).toEqual([
      { type: "agent_message", agent_id: "task-9", content: "What does agent_ended do?", queue: false },
      { type: "agent_message", agent_id: "task-9", content: "And the badge?", queue: true },
    ])
  })

  it("answers a permission request over the socket", async () => {
    const f = fixture()
    f.session.start()
    f.sockets[0].open()

    f.session.resolvePermission("req-1", "allow")

    expect(f.sockets[0].sent.at(-1)).toEqual({ type: "agent_permission", agent_id: "task-9", request_id: "req-1", decision: "allow" })
  })

  it("reads the snapshot again when the server says the stream lost its place", async () => {
    const f = fixture()
    f.session.start()
    f.sockets[0].open()
    await vi.waitFor(() => expect(f.fetcher).toHaveBeenCalledTimes(1))

    f.sockets[0].receive({ type: "chimera_sync_required", agent_id: "task-9" })

    await vi.waitFor(() => expect(f.fetcher).toHaveBeenCalledTimes(2))
  })

  it("reconnects after the socket drops, from where it was", async () => {
    vi.useFakeTimers()
    try {
      const f = fixture()
      f.session.start()
      f.sockets[0].open()
      f.sockets[0].event("task-9", { eventId: "e1", type: "user_message", id: "m1", content: "hello" })
      f.sockets[0].onclose?.()

      expect(f.last().connected).toBe(false)
      await vi.advanceTimersByTimeAsync(1_000)
      f.sockets[1].open()

      expect(f.sockets[1].sent[0]).toMatchObject({ type: "subscribe_agent", agent_id: "task-9" })
      expect(f.last().connected).toBe(true)
    } finally {
      vi.useRealTimers()
    }
  })

  it("stops for good, without reconnecting", () => {
    vi.useFakeTimers()
    try {
      const f = fixture()
      f.session.start()
      f.sockets[0].open()

      f.session.stop()
      vi.advanceTimersByTime(60_000)

      expect(f.sockets).toHaveLength(1)
      expect(f.sockets[0].sent.at(-1)).toEqual({ type: "unsubscribe_agent", agent_id: "task-9" })
      expect(f.sockets[0].closed).toBe(true)
    } finally {
      vi.useRealTimers()
    }
  })

  it("says why when the snapshot cannot be read", async () => {
    const f = fixture()
    f.fetcher.mockResolvedValue(new Response("running agent not found", { status: 404 }))
    f.session.start()
    f.sockets[0].open()

    await vi.waitFor(() => expect(f.last().error).toBe("running agent not found"))
  })

  it("explains how to trust Legato's certificate when Legato cannot be reached", async () => {
    const f = fixture()
    f.fetcher.mockRejectedValue(new TypeError("Failed to fetch"))
    f.session.start()
    f.sockets[0].open()

    await vi.waitFor(() => expect(f.last().error).toContain("Could not reach Legato at https://localhost:3080"))
    expect(f.last().error).toContain("chrome://certificate-manager")
  })
})
