import { mkdtempSync, writeFileSync } from "node:fs"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { describe, expect, it } from "vitest"
import { answerPhone, fakePhone, type FakePhone, type PhoneRequest } from "./legato-dev"

const fixtures = (() => {
  const dir = mkdtempSync(join(tmpdir(), "even-hub-fixture-"))
  writeFileSync(
    join(dir, "agents.json"),
    JSON.stringify([
      { task_id: "FIX-1", status: "running", activity: "waiting" },
      { task_id: "rv1", status: "running", activity: "", reeve: true },
    ]),
  )
  writeFileSync(join(dir, "approvals.json"), JSON.stringify({ approvals: [{ id: "ap-1", title: "Push" }] }))
  writeFileSync(join(dir, "reeve.json"), JSON.stringify({ task_id: "", entries: [{ kind: "assistant", text: "Ready." }] }))
  writeFileSync(join(dir, "empty.json"), "[]")
  writeFileSync(join(dir, "conversation.json"), JSON.stringify({ task_id: "", entries: [{ kind: "user", text: "Hi" }] }))
  return dir
})()

function phone(): FakePhone & { sent: string[] } {
  const sent: string[] = []
  return {
    instances: [
      { id: "desk", name: "Desk", agents: join(fixtures, "agents.json") },
      { id: "laptop", name: "Laptop", agents: join(fixtures, "empty.json") },
    ],
    conversation: join(fixtures, "conversation.json"),
    reeveConversation: join(fixtures, "reeve.json"),
    approvals: join(fixtures, "approvals.json"),
    log: (line) => sent.push(line),
    sent,
  }
}

const get = (path: string): PhoneRequest => ({ method: "GET", path })
const post = (path: string, body: string, contentType = "application/json"): PhoneRequest => ({
  method: "POST",
  path,
  body,
  contentType,
})

describe("fakePhone", () => {
  it("has two instances, the first reading the chosen fixture, so the picker can be seen", () => {
    expect(fakePhone({}, "/app")).toMatchObject({
      instances: [
        { id: "desk", name: "Desk", agents: "/app/fixtures/agents.json" },
        { id: "laptop", name: "Laptop", agents: "/app/fixtures/empty.json" },
      ],
      conversation: "/app/fixtures/conversation.json",
      reeveConversation: "/app/fixtures/reeve.json",
      approvals: "/app/fixtures/approvals.json",
    })
    expect(fakePhone({ LEGATO_FIXTURE: "fixtures/other.json" }, "/app").instances[0].agents).toBe("/app/fixtures/other.json")
  })
})

describe("answerPhone and approvals", () => {
  it("lists the recorded approvals for the first instance, and none for the second", async () => {
    const desk = await answerPhone(get("/v1/instances/desk/api/approvals?status=pending"), phone())
    const laptop = await answerPhone(get("/v1/instances/laptop/api/approvals?status=pending"), phone())

    expect(JSON.parse(desk.body)).toEqual({ approvals: [{ id: "ap-1", title: "Push" }] })
    expect(JSON.parse(laptop.body)).toEqual({ approvals: [] })
  })

  it("logs a decision of allow or deny instead of making it", async () => {
    const p = phone()
    const answer = await answerPhone(post("/v1/instances/desk/api/approvals/ap-1/decision", JSON.stringify({ decision: "allow", option: "once" })), p)

    expect(answer.status).toBe(200)
    expect(p.sent).toEqual(['would allow ap-1 on Desk with "once", from the glasses'])
    expect((await answerPhone(post("/v1/instances/desk/api/approvals/ap-1/decision", JSON.stringify({ decision: "maybe" })), p)).status).toBe(400)
  })

  it("logs answers to a question instead of sending them, and refuses answers that are not lists of strings", async () => {
    const p = phone()
    const answers = { q1: ["Beside the plan (Recommended)"], q2: ["A", "B"] }
    const answer = await answerPhone(post("/v1/instances/desk/api/approvals/ap-q/decision", JSON.stringify({ decision: "answer", answers })), p)

    expect(answer.status).toBe(200)
    expect(p.sent).toEqual([`would answer ap-q on Desk with ${JSON.stringify(answers)}, from the glasses`])
    for (const answers of [undefined, ["A"], { q1: "A" }, { q1: [1] }, {}]) {
      const refused = await answerPhone(post("/v1/instances/desk/api/approvals/ap-q/decision", JSON.stringify({ decision: "answer", answers })), p)
      expect(refused.status).toBe(400)
    }
  })
})

describe("answerPhone and the Reeve", () => {
  it("answers the Reeve's conversation from its own fixture", async () => {
    const answer = await answerPhone(get("/v1/instances/desk/api/agents/rv1/conversation?limit=50"), phone())

    expect(JSON.parse(answer.body)).toEqual({ task_id: "rv1", entries: [{ kind: "assistant", text: "Ready." }] })
  })

  it("logs a message to the Reeve, from its text alone, instead of sending it", async () => {
    const p = phone()
    const answer = await answerPhone(post("/v1/instances/desk/api/reeve/message", JSON.stringify({ text: "Status?", from: "web" })), p)

    expect(answer.status).toBe(200)
    expect(JSON.parse(answer.body)).toEqual({ status: "sent" })
    expect(p.sent).toEqual(['would send the Reeve on Desk, from the glasses: "Status?"'])
  })

  it("refuses a message to the Reeve without its text", async () => {
    expect((await answerPhone(post("/v1/instances/desk/api/reeve/message", JSON.stringify({ content: "Hi" })), phone())).status).toBe(400)
  })
})

describe("answerPhone", () => {
  it("answers a preflight without a code, allowing the glasses' headers from any origin", async () => {
    const answer = await answerPhone({ method: "OPTIONS", path: "/v1/instances" }, phone())

    expect(answer.status).toBe(204)
    expect(answer.headers).toMatchObject({
      "Access-Control-Allow-Origin": "*",
      "Access-Control-Allow-Headers": "Authorization, Content-Type",
      "Access-Control-Allow-Methods": "GET, POST",
      "Access-Control-Allow-Private-Network": "true",
    })
  })

  it("lists its instances by ID and name, in order", async () => {
    const answer = await answerPhone(get("/v1/instances"), phone())

    expect(answer.status).toBe(200)
    expect(JSON.parse(answer.body)).toEqual([
      { id: "desk", name: "Desk" },
      { id: "laptop", name: "Laptop" },
    ])
  })

  it("serves each instance's recorded agents", async () => {
    const desk = await answerPhone(get("/v1/instances/desk/api/agents?view=compact&status=running"), phone())
    const laptop = await answerPhone(get("/v1/instances/laptop/api/agents?view=compact&status=running"), phone())

    expect(JSON.parse(desk.body)).toEqual([
      { task_id: "FIX-1", status: "running", activity: "waiting" },
      { task_id: "rv1", status: "running", activity: "", reeve: true },
    ])
    expect(JSON.parse(laptop.body)).toEqual([])
  })

  it("serves the recorded conversation as any agent's", async () => {
    const answer = await answerPhone(get("/v1/instances/desk/api/agents/LEG-7/conversation?limit=20"), phone())

    expect(JSON.parse(answer.body)).toEqual({ task_id: "LEG-7", entries: [{ kind: "user", text: "Hi" }] })
  })

  it("hears one sentence in any recording, and logs a message instead of sending it", async () => {
    const p = phone()

    const heard = await answerPhone(post("/v1/instances/desk/api/voice/transcribe", '{"transcription_only":true,"pcm":"AQ=="}'), p)
    const sent = await answerPhone(post("/v1/instances/desk/api/agents/LEG-7/chimera/send", '{"content":"Hi"}'), p)

    expect(JSON.parse(heard.body)).toEqual({ text: "Run the tests again, and push if they pass." })
    expect(sent.status).toBe(202)
    expect(p.sent).toEqual(['would send to LEG-7 on Desk: "Hi"'])
  })

  it("answers an instance it doesn't have as the Legato app does", async () => {
    const answer = await answerPhone(get("/v1/instances/gone/api/agents?view=compact&status=running"), phone())

    expect(answer.status).toBe(404)
    expect(JSON.parse(answer.body)).toEqual({ error: "unknown instance" })
  })

  it("answers 404 to anything outside the glasses' calls", async () => {
    const refused: PhoneRequest[] = [
      get("/v1/instances/desk/api/agents"),
      get("/v1/instances/desk/api/agents?view=compact"),
      get("/v1/instances/desk/api/tasks"),
      get("/v1/instances/desk/api/agents/LEG-7/conversation"),
      get("/v1/instances/desk/api/agents/LEG-7/conversation?limit=0"),
      get("/v1/instances/desk/api/agents/LEG-7/conversation?limit=51"),
      get("/v1/instances/desk/api/agents/a/b/conversation?limit=20"),
      get("/v1/instances/desk/api/agents/LEG-7/chimera/send"),
      post("/v1/instances/desk/api/agents/spawn", "{}"),
      post("/v1/instances/desk/api/agents/LEG-7/chimera/interrupt", "{}"),
      post("/v1/instances", "{}"),
      get("/api/agents?view=compact&status=running"),
    ]

    for (const request of refused) {
      expect((await answerPhone(request, phone())).status, `${request.method} ${request.path}`).toBe(404)
    }
  })

  it("refuses a POST that is not JSON", async () => {
    const answer = await answerPhone(post("/v1/instances/desk/api/voice/transcribe", '{"pcm":"AQ=="}', "text/plain"), phone())

    expect(answer.status).toBe(415)
  })

  it("refuses a body without the field the call needs", async () => {
    const p = phone()

    expect((await answerPhone(post("/v1/instances/desk/api/voice/transcribe", '{"pcm":1}'), p)).status).toBe(400)
    expect((await answerPhone(post("/v1/instances/desk/api/agents/A/chimera/send", '{"content":[]}'), p)).status).toBe(400)
    expect((await answerPhone(post("/v1/instances/desk/api/agents/A/chimera/send", "Hi"), p)).status).toBe(400)
    expect(p.sent).toEqual([])
  })

  it("marks every answer as the Legato app does", async () => {
    for (const request of [get("/v1/instances"), get("/nowhere"), { method: "OPTIONS", path: "/v1/instances" }]) {
      expect((await answerPhone(request, phone())).headers).toMatchObject({
        "Access-Control-Allow-Origin": "*",
        "Cache-Control": "no-store",
        Connection: "close",
      })
    }
  })
})
