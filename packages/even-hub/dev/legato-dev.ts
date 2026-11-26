// The dev server's fake phone: what the app reads in the simulator. It
// answers the Legato Android app's glasses link protocol (milestone LEG-474;
// mobile/README.md § The glasses link) at /phone, from recorded fixtures,
// for two made-up Legato instances. It asks for no pairing code, and logs a
// message instead of sending it: nothing here reaches a real Legato.
//
// It also serves POST /__log, which prints what a dev build of the app logs
// (src/dev-log.ts): the Even app's WebView on Android has no DevTools.
import { readFile } from "node:fs/promises"
import { resolve } from "node:path"
import type { IncomingMessage } from "node:http"
import type { Plugin } from "vite"

export type FakeInstance = { id: string; name: string; agents: string }
export type FakePhone = {
  instances: FakeInstance[]
  conversation: string
  // The Reeve's conversation: any agent the instance's list marks `reeve`.
  reeveConversation: string
  // The approvals waiting on the first instance; the others have none.
  approvals: string
  log: (line: string) => void
}

export type PhoneRequest = { method: string; path: string; contentType?: string; body?: string }
export type Answer = { status: number; headers: Record<string, string>; body: string }

export const DEV_PATH = "/phone"
export const LOG_PATH = "/__log"
const DEFAULT_FIXTURE = "fixtures/agents.json"
const EMPTY_FIXTURE = "fixtures/empty.json"
const CONVERSATION_FIXTURE = "fixtures/conversation.json"
const REEVE_FIXTURE = "fixtures/reeve.json"
const APPROVALS_FIXTURE = "fixtures/approvals.json"
// What the fixtures "hear" in any recording.
const FIXTURE_TRANSCRIPT = "Run the tests again, and push if they pass."

// The fake phone, by the environment (or .env.local): LEGATO_FIXTURE picks
// the first instance's list of agents. The second has none.
export function fakePhone(env: Record<string, string | undefined>, root: string): FakePhone {
  return {
    instances: [
      { id: "desk", name: "Desk", agents: resolve(root, env.LEGATO_FIXTURE?.trim() || DEFAULT_FIXTURE) },
      { id: "laptop", name: "Laptop", agents: resolve(root, EMPTY_FIXTURE) },
    ],
    conversation: resolve(root, CONVERSATION_FIXTURE),
    reeveConversation: resolve(root, REEVE_FIXTURE),
    approvals: resolve(root, APPROVALS_FIXTURE),
    log: (line) => console.info(`[legato-even-hub] fake phone: ${line}`),
  }
}

// What the Legato app puts on every answer.
const COMMON = { "Access-Control-Allow-Origin": "*", "Cache-Control": "no-store", Connection: "close" }

const json = (status: number, value: unknown): Answer => ({
  status,
  headers: { ...COMMON, "Content-Type": "application/json" },
  body: JSON.stringify(value),
})

type Call =
  | { route: "agents" }
  | { route: "conversation"; taskId: string }
  | { route: "transcribe" }
  | { route: "send"; taskId: string }
  | { route: "reeve" }
  | { route: "approvals" }
  | { route: "decide"; id: string }

// The Legato calls the glasses make through an instance, by method, path
// and query. A task ID is one path segment.
function legatoCall(method: string, rest: string, query: URLSearchParams): Call | undefined {
  const exactly = (want: Record<string, string>) =>
    [...query.keys()].length === Object.keys(want).length && Object.entries(want).every(([k, v]) => query.getAll(k).join() === v)
  if (method === "GET" && rest === "/api/agents") return exactly({ view: "compact", status: "running" }) ? { route: "agents" } : undefined
  if (method === "POST" && rest === "/api/voice/transcribe" && [...query.keys()].length === 0) return { route: "transcribe" }
  if (method === "POST" && rest === "/api/reeve/message" && [...query.keys()].length === 0) return { route: "reeve" }
  if (method === "GET" && rest === "/api/approvals") return exactly({ status: "pending" }) ? { route: "approvals" } : undefined
  const decision = rest.match(/^\/api\/approvals\/([^/]+)\/decision$/)
  if (method === "POST" && decision && [...query.keys()].length === 0) return { route: "decide", id: decodeURIComponent(decision[1]) }
  const agent = rest.match(/^\/api\/agents\/([^/]+)\/(conversation|chimera\/send)$/)
  if (!agent) return undefined
  const taskId = decodeURIComponent(agent[1])
  if (method === "GET" && agent[2] === "conversation") {
    const limit = query.get("limit") ?? ""
    const n = /^[0-9]{1,2}$/.test(limit) ? Number(limit) : 0
    return [...query.keys()].length === 1 && n >= 1 && n <= 50 ? { route: "conversation", taskId } : undefined
  }
  if (method === "POST" && agent[2] === "chimera/send" && [...query.keys()].length === 0) return { route: "send", taskId }
  return undefined
}

const NOT_SERVED = () => json(404, { error: "not served to the glasses" })

// Answers one request as the Legato app would, from the fixtures.
export async function answerPhone(request: PhoneRequest, phone: FakePhone): Promise<Answer> {
  if (request.method === "OPTIONS") {
    return {
      status: 204,
      headers: {
        ...COMMON,
        "Access-Control-Allow-Headers": "Authorization, Content-Type",
        "Access-Control-Allow-Methods": "GET, POST",
        "Access-Control-Allow-Private-Network": "true",
      },
      body: "",
    }
  }
  const { pathname, searchParams } = new URL(request.path, "http://phone")
  if (request.method === "GET" && pathname === "/v1/instances" && [...searchParams.keys()].length === 0) {
    return json(200, phone.instances.map(({ id, name }) => ({ id, name })))
  }
  const at = pathname.match(/^\/v1\/instances\/([^/]+)(\/.*)$/)
  if (!at) return NOT_SERVED()
  const call = legatoCall(request.method, at[2], searchParams)
  if (!call) return NOT_SERVED()
  const instance = phone.instances.find((i) => i.id === decodeURIComponent(at[1]))
  if (!instance) return json(404, { error: "unknown instance" })

  let field = ""
  if (request.method === "POST") {
    if (request.contentType?.split(";")[0].trim().toLowerCase() !== "application/json") {
      return json(415, { error: "the body must be JSON (Content-Type: application/json)" })
    }
    let sent: unknown
    try {
      sent = JSON.parse(request.body ?? "")
    } catch {
      return json(400, { error: "the body is not JSON" })
    }
    const name = call.route === "transcribe" ? "pcm" : call.route === "reeve" ? "text" : call.route === "decide" ? "decision" : "content"
    const value = (sent as Record<string, unknown> | null)?.[name]
    if (typeof value !== "string") return json(400, { error: `${name} must be a string` })
    field = value
    if (call.route === "decide" && value === "answer") {
      // As the Legato app does: question IDs to lists of strings, nothing else.
      const answers = (sent as Record<string, unknown>).answers
      const valid =
        typeof answers === "object" &&
        answers !== null &&
        !Array.isArray(answers) &&
        Object.keys(answers).length > 0 &&
        Object.values(answers).every((a) => Array.isArray(a) && a.every((s) => typeof s === "string"))
      if (!valid) return json(400, { error: "answers must map question IDs to lists of strings" })
      field = `answer ${JSON.stringify(answers)}`
    } else if (call.route === "decide" && value !== "allow" && value !== "deny") return json(400, { error: "decision must be allow, deny or answer" })
    else if (call.route === "decide") {
      const option = (sent as Record<string, unknown>).option
      field = typeof option === "string" ? `${value} ${JSON.stringify(option)}` : value
    }
  }

  switch (call.route) {
    case "agents":
      return { ...json(200, null), body: await readFile(instance.agents, "utf8") }
    case "conversation": {
      const agents = JSON.parse(await readFile(instance.agents, "utf8")) as { task_id: string; reeve?: boolean }[]
      const reeve = agents.some((a) => a.reeve && a.task_id === call.taskId)
      return json(200, { ...JSON.parse(await readFile(reeve ? phone.reeveConversation : phone.conversation, "utf8")), task_id: call.taskId })
    }
    case "transcribe":
      return json(200, { text: FIXTURE_TRANSCRIPT })
    case "send":
      phone.log(`would send to ${call.taskId} on ${instance.name}: ${JSON.stringify(field)}`)
      return { status: 202, headers: COMMON, body: "" }
    case "reeve":
      phone.log(`would send the Reeve on ${instance.name}, from the glasses: ${JSON.stringify(field)}`)
      return json(200, { status: "sent" })
    case "approvals":
      if (instance !== phone.instances[0]) return json(200, { approvals: [] })
      return { ...json(200, null), body: await readFile(phone.approvals, "utf8") }
    case "decide": {
      if (field.startsWith("answer ")) {
        phone.log(`would answer ${call.id} on ${instance.name} with ${field.slice("answer ".length)}, from the glasses`)
        return json(200, { id: call.id })
      }
      const [decision, option] = field.split(" ")
      phone.log(`would ${decision} ${call.id} on ${instance.name}${option ? ` with ${option}` : ""}, from the glasses`)
      return json(200, { id: call.id })
    }
  }
}

// A recording of a minute is about 2.6 MB as base64.
const MAX_BODY = 8 << 20
const MAX_LOG_BODY = 64 << 10

class TooLarge extends Error {}

// The body of a POST; past `max` it is read to its end and dropped, and the
// request is answered 413.
function readBody(req: IncomingMessage, max = MAX_BODY): Promise<string | undefined> {
  if (req.method !== "POST") return Promise.resolve(undefined)
  return new Promise((done, fail) => {
    const chunks: Buffer[] = []
    let size = 0
    req.on("data", (chunk: Buffer) => {
      size += chunk.length
      if (size <= max) chunks.push(chunk)
    })
    req.on("end", () => (size > max ? fail(new TooLarge()) : done(Buffer.concat(chunks).toString("utf8"))))
    req.on("error", fail)
  })
}

// One line a dev build of the app sent to LOG_PATH, as the terminal shows it.
export function logLine(body: string): string {
  let entry: { level?: unknown; text?: unknown }
  try {
    entry = JSON.parse(body)
  } catch {
    return `[glasses] ${body.slice(0, 2000)}`
  }
  const level = typeof entry.level === "string" ? entry.level : "log"
  return `[glasses ${level}] ${String(entry.text ?? "").slice(0, 2000)}`
}

// The Vite plugin that serves DEV_PATH and LOG_PATH. `real` says the app
// reads the real phone (VITE_LEGATO_PHONE=real), not the fake one.
export function legatoDev(phone: FakePhone, real = false): Plugin {
  return {
    name: "legato-dev",
    configureServer(server) {
      server.config.logger.info(
        real
          ? "  The app reads the Legato app on the phone at http://127.0.0.1:8792 (VITE_LEGATO_PHONE=real)"
          : `  Fake phone: ${phone.instances.map((i) => `${i.name} (${i.agents})`).join(", ")}`,
      )
      server.middlewares.use(LOG_PATH, (req, res) => {
        readBody(req, MAX_LOG_BODY).then(
          (body) => {
            if (body !== undefined) server.config.logger.info(logLine(body))
            res.statusCode = 204
            res.end()
          },
          () => {
            res.statusCode = 413
            res.end()
          },
        )
      })
      server.middlewares.use(DEV_PATH, (req, res) => {
        const send = (answer: Answer) => {
          res.statusCode = answer.status
          for (const [name, value] of Object.entries(answer.headers)) res.setHeader(name, value)
          res.end(answer.body)
        }
        readBody(req)
          .then((body) =>
            answerPhone(
              { method: req.method ?? "GET", path: req.url ?? "/", contentType: req.headers["content-type"], body },
              phone,
            ),
          )
          .then(send, (err: unknown) =>
            send(err instanceof TooLarge ? json(413, { error: "the body is too large" }) : json(500, { error: String(err) })),
          )
      })
    },
  }
}
