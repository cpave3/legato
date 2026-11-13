import { mkdtempSync, writeFileSync } from "node:fs"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { describe, expect, it } from "vitest"
import { answerLegato, crossSiteRefusal, devSettings, type DevSettings } from "./legato-dev"

const fixtures = (() => {
  const dir = mkdtempSync(join(tmpdir(), "even-hub-fixture-"))
  writeFileSync(join(dir, "agents.json"), JSON.stringify([{ task_id: "FIX-1", status: "running", activity: "waiting" }]))
  writeFileSync(join(dir, "conversation.json"), JSON.stringify({ task_id: "", entries: [{ kind: "user", text: "Hi" }] }))
  return dir
})()

const fixture: DevSettings = {
  kind: "fixture",
  file: join(fixtures, "agents.json"),
  conversation: join(fixtures, "conversation.json"),
}
const proxy = (allowSend = false): DevSettings => ({ kind: "proxy", url: "https://l", token: "t0k", allowSend })

type Asked = { url: string; auth: string | null; method?: string; body?: unknown }

function fakeLegato(respond: () => Response = () => Response.json([])) {
  const asked: Asked[] = []
  const fn = (async (url: string, init?: RequestInit) => {
    asked.push({
      url,
      auth: new Headers(init?.headers).get("authorization"),
      method: init?.method,
      body: init?.body ? JSON.parse(String(init.body)) : undefined,
    })
    return respond()
  }) as unknown as typeof fetch
  return { fn, asked }
}

describe("devSettings", () => {
  it("reads Legato's address and token from the environment, the fixtures when there is no address", () => {
    expect(devSettings({ LEGATO_URL: "https://127.0.0.1:3999/", LEGATO_TOKEN: "t0k" }, "/app")).toEqual({
      kind: "proxy",
      url: "https://127.0.0.1:3999",
      token: "t0k",
      allowSend: false,
    })
    expect(devSettings({}, "/app")).toEqual({
      kind: "fixture",
      file: "/app/fixtures/agents.json",
      conversation: "/app/fixtures/conversation.json",
    })
    expect(devSettings({ LEGATO_FIXTURE: "fixtures/empty.json" }, "/app")).toMatchObject({ file: "/app/fixtures/empty.json" })
  })

  it("passes messages on only when LEGATO_ALLOW_SEND is 1", () => {
    const env = { LEGATO_URL: "https://l", LEGATO_TOKEN: "t" }
    expect(devSettings({ ...env, LEGATO_ALLOW_SEND: "1" }, "/app")).toMatchObject({ allowSend: true })
    expect(devSettings({ ...env, LEGATO_ALLOW_SEND: "yes" }, "/app")).toMatchObject({ allowSend: false })
  })

  it("refuses an address without a token rather than ask Legato unauthenticated", () => {
    expect(() => devSettings({ LEGATO_URL: "https://127.0.0.1:3999" }, "/app")).toThrow(/LEGATO_TOKEN/)
  })
})

describe("answerLegato with the fixtures", () => {
  it("serves the recorded list of agents", async () => {
    const answer = await answerLegato("GET", "/api/agents?view=compact&status=running", undefined, fixture)

    expect(answer.status).toBe(200)
    expect(JSON.parse(answer.body)).toEqual([{ task_id: "FIX-1", status: "running", activity: "waiting" }])
  })

  it("serves the recorded conversation as any agent's", async () => {
    const answer = await answerLegato("GET", "/api/agents/LEG-7/conversation?limit=20", undefined, fixture)

    expect(JSON.parse(answer.body)).toEqual({ task_id: "LEG-7", entries: [{ kind: "user", text: "Hi" }] })
  })

  it("answers a recording with a set transcript, and takes a message without sending it anywhere", async () => {
    const heard = await answerLegato("POST", "/api/voice/transcribe", '{"transcription_only":true,"pcm":"AQ=="}', fixture)
    const sent = await answerLegato("POST", "/api/agents/LEG-7/chimera/send", '{"content":"Hi"}', fixture)

    expect(JSON.parse(heard.body)).toEqual({ text: "Run the tests again, and push if they pass." })
    expect(sent.status).toBe(202)
  })
})

describe("answerLegato passing reads on to Legato", () => {
  it("passes the agent list on with the token, which the app never sees", async () => {
    const legato = fakeLegato()

    const answer = await answerLegato("GET", "/api/agents?view=compact&status=running", undefined, proxy(), legato.fn)

    expect(legato.asked).toEqual([
      { url: "https://l/api/agents?view=compact&status=running", auth: "Bearer t0k", method: "GET", body: undefined },
    ])
    expect(answer).toEqual({ status: 200, body: "[]" })
  })

  it("passes an agent's conversation on", async () => {
    const legato = fakeLegato()

    await answerLegato("GET", "/api/agents/LEG-7/conversation?limit=20", undefined, proxy(), legato.fn)

    expect(legato.asked[0].url).toBe("https://l/api/agents/LEG-7/conversation?limit=20")
  })

  it("passes a recording on for transcription only, whatever the app asked", async () => {
    const legato = fakeLegato(() => Response.json({ text: "Hi" }))

    await answerLegato("POST", "/api/voice/transcribe", '{"pcm":"AQ==","agent_id":"LEG-7"}', proxy(), legato.fn)

    expect(legato.asked[0]).toMatchObject({
      url: "https://l/api/voice/transcribe",
      method: "POST",
      body: { pcm: "AQ==", transcription_only: true },
    })
  })

  it("forwards only the recording, so no other key can turn transcription-only off", async () => {
    const legato = fakeLegato(() => Response.json({ text: "Hi" }))
    // Go's JSON decoder matches keys without case and keeps the last one.
    const body = '{"transcription_only":false,"TRANSCRIPTION_ONLY":false,"agent_id":"LEG-7","pcm":"AQ=="}'

    await answerLegato("POST", "/api/voice/transcribe", body, proxy(), legato.fn)

    expect(legato.asked[0].body).toEqual({ transcription_only: true, pcm: "AQ==" })
  })

  it("forwards only a message's content", async () => {
    const legato = fakeLegato(() => new Response(null, { status: 202 }))

    await answerLegato("POST", "/api/agents/LEG-7/chimera/send", '{"content":"Hi","extra":1}', proxy(true), legato.fn)

    expect(legato.asked[0].body).toEqual({ content: "Hi" })
  })

  it("refuses a recording or a message that is not a string", async () => {
    const legato = fakeLegato()

    expect((await answerLegato("POST", "/api/voice/transcribe", '{"pcm":1}', proxy(), legato.fn)).status).toBe(400)
    expect((await answerLegato("POST", "/api/agents/A/chimera/send", '{"content":[]}', proxy(true), legato.fn)).status).toBe(400)
    expect(legato.asked).toEqual([])
  })

  it("refuses to send a message unless sending is allowed", async () => {
    const legato = fakeLegato()

    const answer = await answerLegato("POST", "/api/agents/LEG-7/chimera/send", '{"content":"Hi"}', proxy(false), legato.fn)

    expect(answer.status).toBe(403)
    expect(legato.asked).toEqual([])
  })

  it("sends a message when sending is allowed", async () => {
    const legato = fakeLegato(() => new Response(null, { status: 202 }))

    const answer = await answerLegato("POST", "/api/agents/LEG-7/chimera/send", '{"content":"Hi"}', proxy(true), legato.fn)

    expect(answer.status).toBe(202)
    expect(legato.asked[0]).toMatchObject({ url: "https://l/api/agents/LEG-7/chimera/send", body: { content: "Hi" } })
  })

  it("passes on Legato's refusal as it is", async () => {
    const legato = fakeLegato(() => new Response('{"error":"unauthorized"}', { status: 401 }))

    expect((await answerLegato("GET", "/api/agents", undefined, proxy(), legato.fn)).status).toBe(401)
  })

  it("answers nothing else, so nothing else can reach Legato through it", async () => {
    const legato = fakeLegato()
    const refused = [
      ["POST", "/api/agents/spawn"],
      ["DELETE", "/api/agents"],
      ["GET", "/api/tasks"],
      ["POST", "/api/agents/LEG-7/chimera/interrupt"],
      ["POST", "/api/agents/LEG-7/conversation"],
      ["GET", "/api/agents/LEG-7/chimera/send"],
      ["GET", "/api/agents/a/b/conversation"],
    ]

    for (const [method, path] of refused) {
      expect((await answerLegato(method, path, "{}", proxy(true), legato.fn)).status, `${method} ${path}`).toBe(404)
    }
    expect(legato.asked).toEqual([])
  })

  it("refuses a body that is not JSON", async () => {
    expect((await answerLegato("POST", "/api/voice/transcribe", "pcm", proxy())).status).toBe(400)
  })

  it("answers 502 when it can't reach Legato", async () => {
    const fn = (async () => {
      throw new TypeError("self-signed certificate")
    }) as unknown as typeof fetch

    expect((await answerLegato("GET", "/api/agents", undefined, proxy(), fn)).status).toBe(502)
  })
})

describe("crossSiteRefusal", () => {
  it("lets the app's own requests through", () => {
    expect(crossSiteRefusal({ method: "GET", fetchSite: "same-origin" })).toBeUndefined()
    expect(crossSiteRefusal({ method: "POST", contentType: "application/json", fetchSite: "same-origin" })).toBeUndefined()
    expect(crossSiteRefusal({ method: "POST", contentType: "application/json; charset=utf-8" })).toBeUndefined()
  })

  it("refuses a POST that is not JSON, which a page elsewhere could send without asking", () => {
    expect(crossSiteRefusal({ method: "POST", contentType: "text/plain" })).toMatch(/JSON/)
    expect(crossSiteRefusal({ method: "POST" })).toMatch(/JSON/)
  })

  it("refuses requests the browser says come from another site", () => {
    expect(crossSiteRefusal({ method: "GET", fetchSite: "cross-site" })).toMatch(/another site/)
    expect(crossSiteRefusal({ method: "POST", contentType: "application/json", fetchSite: "cross-site" })).toMatch(/another site/)
  })
})
