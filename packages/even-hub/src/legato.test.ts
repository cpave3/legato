import { describe, expect, it } from "vitest"
import { fetchAgents, fetchConversation, sendMessage, transcribe, type Agent, type Phone, fetchInstances } from "./legato"

const running: Agent = {
  task_id: "LEG-163",
  title: "POC: an Even Realities G2 glasses app",
  status: "running",
  activity: "working",
  sandboxed: true,
  started_at: "2026-11-13T07:24:57Z",
}

type Call = { url: string; headers: Record<string, string> }
type Sent = Call & { method: string; body: unknown }

function fakeFetch(respond: () => Response | Promise<Response>) {
  const calls: Call[] = []
  const sent: Sent[] = []
  const fn = async (input: RequestInfo | URL, init?: RequestInit) => {
    const call = { url: String(input), headers: Object.fromEntries(new Headers(init?.headers)) }
    calls.push(call)
    sent.push({ ...call, method: init?.method ?? "GET", body: init?.body ? JSON.parse(String(init.body)) : undefined })
    return respond()
  }
  return { fn: fn as typeof fetch, calls, sent }
}

const phone: Phone = { url: "http://127.0.0.1:8792", code: "ABCDEFGHIJKLMNOPQRSTUVWX" }
const at = "http://127.0.0.1:8792/v1/instances/inst-1"

describe("fetchAgents", () => {
  it("asks the Legato app for one instance's running agents in the compact view, with the pairing code", async () => {
    const fake = fakeFetch(() => Response.json([running]))

    const result = await fetchAgents(phone, "inst-1", fake.fn)

    expect(result).toEqual({ ok: true, agents: [running] })
    expect(fake.calls).toEqual([
      {
        url: `${at}/api/agents?view=compact&status=running`,
        headers: { accept: "application/json", authorization: "Bearer ABCDEFGHIJKLMNOPQRSTUVWX" },
      },
    ])
  })

  it("reads each instance at its own path", async () => {
    const fake = fakeFetch(() => Response.json([]))

    await fetchAgents(phone, "inst-1", fake.fn)
    await fetchAgents(phone, "a/b", fake.fn)

    expect(fake.calls.map((c) => c.url)).toEqual([
      `${at}/api/agents?view=compact&status=running`,
      "http://127.0.0.1:8792/v1/instances/a%2Fb/api/agents?view=compact&status=running",
    ])
  })

  it("passes on Legato's own words for a refusal it gives as {error}", async () => {
    const fake = fakeFetch(() => Response.json({ error: "workspace not found" }, { status: 400 }))

    expect(await fetchAgents(phone, "inst-1", fake.fn)).toEqual({ ok: false, problem: "Legato: workspace not found" })
  })

  it("names the status of any other failed answer", async () => {
    const fake = fakeFetch(() => new Response("boom", { status: 500 }))

    expect(await fetchAgents(phone, "inst-1", fake.fn)).toEqual({ ok: false, problem: "Legato answered 500" })
  })

  it("does not take an answer that is not a list of agents", async () => {
    const fake = fakeFetch(() => Response.json({ agents: [] }))

    expect(await fetchAgents(phone, "inst-1", fake.fn)).toEqual({
      ok: false,
      problem: "Legato's answer was not a list of agents",
    })
  })
})

describe("the Legato app's own answers", () => {
  it("sends nothing when no pairing code is saved", async () => {
    const fake = fakeFetch(() => Response.json([]))

    expect(await fetchAgents({ ...phone, code: "" }, "inst-1", fake.fn)).toEqual({
      ok: false,
      problem: "Not paired with the Legato app",
    })
    expect(await fetchInstances({ ...phone, code: "" }, fake.fn)).toEqual({ ok: false, problem: "Not paired with the Legato app" })
    expect(fake.calls).toEqual([])
  })

  it("sends no Authorization header to a phone that asks for no code, as the dev server's fake phone", async () => {
    const fake = fakeFetch(() => Response.json([]))

    await fetchAgents({ url: "/phone", code: false }, "inst-1", fake.fn)

    expect(fake.calls[0].url).toBe("/phone/v1/instances/inst-1/api/agents?view=compact&status=running")
    expect(fake.calls[0].headers).not.toHaveProperty("authorization")
  })

  it("says the code was refused on a 401", async () => {
    const fake = fakeFetch(() => new Response(null, { status: 401 }))

    expect(await fetchAgents(phone, "inst-1", fake.fn)).toEqual({
      ok: false,
      problem: "The Legato app refused the pairing code",
    })
  })

  it("says the instance is gone when the phone no longer has it", async () => {
    const fake = fakeFetch(() => Response.json({ error: "unknown instance" }, { status: 404 }))

    expect(await fetchConversation(phone, "inst-1", "LEG-1", fake.fn)).toEqual({
      ok: false,
      problem: "That Legato is no longer in the app",
      gone: true,
    })
  })

  it("gives the phone's words when it couldn't reach Legato", async () => {
    const said = fakeFetch(() => Response.json({ error: "Legato refused the Legato app's token" }, { status: 502 }))
    const silent = fakeFetch(() => new Response("", { status: 502 }))

    expect(await fetchAgents(phone, "inst-1", said.fn)).toEqual({ ok: false, problem: "Legato refused the Legato app's token" })
    expect(await fetchAgents(phone, "inst-1", silent.fn)).toEqual({ ok: false, problem: "Can't reach Legato" })
  })

  it("says it can't reach the Legato app when the request fails", async () => {
    const fake = fakeFetch(() => Promise.reject(new TypeError("Failed to fetch")))

    expect(await fetchAgents(phone, "inst-1", fake.fn)).toEqual({ ok: false, problem: "Can't reach the Legato app on this phone" })
  })
})

describe("fetchInstances", () => {
  it("lists the instances saved in the Legato app, in its order", async () => {
    const fake = fakeFetch(() => Response.json([{ id: "b", name: "Laptop" }, { id: "a", name: "Desk" }]))

    expect(await fetchInstances(phone, fake.fn)).toEqual({
      ok: true,
      instances: [{ id: "b", name: "Laptop" }, { id: "a", name: "Desk" }],
    })
    expect(fake.calls).toEqual([
      { url: "http://127.0.0.1:8792/v1/instances", headers: { accept: "application/json", authorization: "Bearer ABCDEFGHIJKLMNOPQRSTUVWX" } },
    ])
  })

  it("does not take an answer that is not a list of instances", async () => {
    const fake = fakeFetch(() => Response.json([{ id: 1 }]))

    expect(await fetchInstances(phone, fake.fn)).toEqual({ ok: false, problem: "The Legato app's answer was not a list of instances" })
  })
})

describe("fetchConversation", () => {
  const conversation = {
    task_id: "LEG-163",
    status: "waiting_for_permission",
    entries: [{ kind: "assistant", text: "Running the tests." }],
    permission: { tool: "Bash", command: "pnpm test" },
    question: null,
    context_used: 1000,
    context_window: 200000,
  }

  it("reads the end of an agent's conversation", async () => {
    const fake = fakeFetch(() => Response.json(conversation))

    expect(await fetchConversation(phone, "inst-1", "LEG-163", fake.fn)).toEqual({ ok: true, conversation })
    expect(fake.sent[0]).toMatchObject({
      method: "GET",
      url: `${at}/api/agents/LEG-163/conversation?limit=20`,
      headers: { authorization: "Bearer ABCDEFGHIJKLMNOPQRSTUVWX" },
    })
  })

  it("says an agent driven through its terminal has no conversation to read", async () => {
    const fake = fakeFetch(() => new Response("agent has no native endpoint", { status: 409 }))

    expect(await fetchConversation(phone, "inst-1", "LEG-1", fake.fn)).toEqual({
      ok: false,
      problem: "This agent runs in a terminal, so Legato can't read its conversation.",
    })
  })

  it("says the agent has gone on a 404", async () => {
    const fake = fakeFetch(() => new Response("running agent not found", { status: 404 }))

    expect(await fetchConversation(phone, "inst-1", "LEG-1", fake.fn)).toEqual({ ok: false, problem: "The agent is no longer running." })
  })

  it("puts the task ID in the path safely", async () => {
    const fake = fakeFetch(() => Response.json(conversation))

    await fetchConversation(phone, "inst-1", "a/b?c", fake.fn)

    expect(fake.sent[0].url).toBe(`${at}/api/agents/a%2Fb%3Fc/conversation?limit=20`)
  })
})

describe("transcribe", () => {
  it("sends the recording for transcription only, as base64 PCM", async () => {
    const fake = fakeFetch(() => Response.json({ text: " Run the tests again. " }))

    const result = await transcribe(phone, "inst-1", new Uint8Array([1, 2, 3, 250]), fake.fn)

    expect(result).toEqual({ ok: true, text: "Run the tests again." })
    expect(fake.sent[0]).toMatchObject({
      method: "POST",
      url: `${at}/api/voice/transcribe`,
      body: { transcription_only: true, pcm: "AQID+g==" },
      headers: { authorization: "Bearer ABCDEFGHIJKLMNOPQRSTUVWX", "content-type": "application/json" },
    })
  })

  it("passes on what went wrong with the transcription", async () => {
    const fake = fakeFetch(() => Response.json({ text: "", error: "whisper: connection refused" }))

    expect(await transcribe(phone, "inst-1", new Uint8Array([1]), fake.fn)).toEqual({
      ok: false,
      problem: "Transcription failed: whisper: connection refused",
    })
  })

  it("says when it heard nothing", async () => {
    const fake = fakeFetch(() => Response.json({ text: "  " }))

    expect(await transcribe(phone, "inst-1", new Uint8Array([1]), fake.fn)).toEqual({ ok: false, problem: "Heard nothing." })
  })

  it("says when Legato has no transcription", async () => {
    const fake = fakeFetch(() => new Response("voice not enabled", { status: 404 }))

    expect(await transcribe(phone, "inst-1", new Uint8Array([1]), fake.fn)).toEqual({
      ok: false,
      problem: "Transcription is off in Legato.",
    })
  })
})

describe("sendMessage", () => {
  it("sends the operator's words to the agent's native session", async () => {
    const fake = fakeFetch(() => new Response(null, { status: 202 }))

    expect(await sendMessage(phone, "inst-1", "LEG-163", "Run the tests again.", fake.fn)).toEqual({ ok: true })
    expect(fake.sent[0]).toMatchObject({
      method: "POST",
      url: `${at}/api/agents/LEG-163/chimera/send`,
      body: { content: "Run the tests again." },
    })
  })

  it("says the agent is busy on a 409", async () => {
    const fake = fakeFetch(() => new Response('{"error":"busy"}', { status: 409 }))

    expect(await sendMessage(phone, "inst-1", "LEG-163", "Hi", fake.fn)).toEqual({ ok: false, problem: "The agent is busy. Try again when it stops." })
  })

  it("says an agent without a native session can't take messages from here", async () => {
    const fake = fakeFetch(() => new Response("no endpoint", { status: 503 }))

    expect(await sendMessage(phone, "inst-1", "LEG-163", "Hi", fake.fn)).toEqual({
      ok: false,
      problem: "This agent can't take messages from the glasses.",
    })
  })
})

describe("a call that never answers", () => {
  it("gives up after a while and says the Legato app took too long", async () => {
    const hang = ((_: RequestInfo | URL, init?: RequestInit) =>
      new Promise<Response>((_, fail) => init?.signal?.addEventListener("abort", () => fail(init.signal!.reason)))) as typeof fetch

    expect(await fetchAgents(phone, "inst-1", hang, 20)).toEqual({ ok: false, problem: "The Legato app took too long to answer" })
  })
})
