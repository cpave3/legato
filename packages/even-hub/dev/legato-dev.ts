// The dev server's /legato path: what the app reads in the simulator and on
// sideloaded glasses. It serves recorded fixtures, or passes the app's calls
// on to the Legato at LEGATO_URL with LEGATO_TOKEN, so the token stays in this
// process and never reaches the browser bundle. It answers four calls and
// nothing else: the agent list, an agent's conversation, a transcription, and
// a message to an agent, which it passes on only with LEGATO_ALLOW_SEND=1.
import { readFile } from "node:fs/promises"
import { resolve } from "node:path"
import type { IncomingMessage } from "node:http"
import type { Plugin } from "vite"

export type DevSettings =
  | { kind: "fixture"; file: string; conversation: string }
  | { kind: "proxy"; url: string; token: string; allowSend: boolean }

export type Answer = { status: number; body: string }

export const DEV_PATH = "/legato"
const DEFAULT_FIXTURE = "fixtures/agents.json"
const CONVERSATION_FIXTURE = "fixtures/conversation.json"
// What the fixtures "hear" in any recording.
const FIXTURE_TRANSCRIPT = "Run the tests again, and push if they pass."

// Where the dev server reads from, by the environment (or .env.local).
export function devSettings(env: Record<string, string | undefined>, root: string): DevSettings {
  const url = env.LEGATO_URL?.trim()
  if (!url) {
    return {
      kind: "fixture",
      file: resolve(root, env.LEGATO_FIXTURE?.trim() || DEFAULT_FIXTURE),
      conversation: resolve(root, CONVERSATION_FIXTURE),
    }
  }
  const token = env.LEGATO_TOKEN?.trim()
  if (!token) throw new Error("LEGATO_URL is set without LEGATO_TOKEN: set both, or neither for the fixtures")
  return { kind: "proxy", url: url.replace(/\/+$/, ""), token, allowSend: env.LEGATO_ALLOW_SEND?.trim() === "1" }
}

type Route = "agents" | "conversation" | "transcribe" | "send"

// The four calls the glasses make, by method and path. A task ID is one
// path segment.
function route(method: string, pathname: string): { route: Route; taskId?: string } | undefined {
  if (method === "GET" && pathname === "/api/agents") return { route: "agents" }
  if (method === "POST" && pathname === "/api/voice/transcribe") return { route: "transcribe" }
  const agent = pathname.match(/^\/api\/agents\/([^/]+)\/(conversation|chimera\/send)$/)
  if (!agent) return undefined
  const taskId = decodeURIComponent(agent[1])
  if (method === "GET" && agent[2] === "conversation") return { route: "conversation", taskId }
  if (method === "POST" && agent[2] === "chimera/send") return { route: "send", taskId }
  return undefined
}

const json = (status: number, value: unknown): Answer => ({ status, body: JSON.stringify(value) })

async function fromFixtures(settings: Extract<DevSettings, { kind: "fixture" }>, call: Route, taskId?: string, body?: unknown): Promise<Answer> {
  switch (call) {
    case "agents":
      return { status: 200, body: await readFile(settings.file, "utf8") }
    case "conversation": {
      const conversation = JSON.parse(await readFile(settings.conversation, "utf8"))
      return json(200, { ...conversation, task_id: taskId })
    }
    case "transcribe":
      return json(200, { text: FIXTURE_TRANSCRIPT })
    case "send":
      console.info(`[legato-even-hub] fixture: would send to ${taskId}: ${JSON.stringify((body as { content?: string })?.content)}`)
      return { status: 202, body: "" }
  }
}

export async function answerLegato(
  method: string,
  path: string,
  rawBody: string | undefined,
  settings: DevSettings,
  fetchFn: typeof fetch = fetch,
): Promise<Answer> {
  const { pathname, search } = new URL(path, "http://dev")
  const found = route(method, pathname)
  if (!found) return json(404, { error: "not served to the glasses" })

  let body: Record<string, unknown> | undefined
  if (method === "POST") {
    try {
      body = JSON.parse(rawBody ?? "")
    } catch {
      return json(400, { error: "the body is not JSON" })
    }
  }
  if (settings.kind === "fixture") return fromFixtures(settings, found.route, found.taskId, body)

  if (found.route === "send" && !settings.allowSend) {
    return json(403, { error: "sending is off: set LEGATO_ALLOW_SEND=1 for a scratch Legato" })
  }
  // Transcription only: never let Legato type the text into a pane.
  if (found.route === "transcribe") body = { ...body, transcription_only: true }

  try {
    const response = await fetchFn(`${settings.url}${pathname}${search}`, {
      method,
      headers: {
        Accept: "application/json",
        Authorization: `Bearer ${settings.token}`,
        ...(body ? { "Content-Type": "application/json" } : {}),
      },
      body: body ? JSON.stringify(body) : undefined,
    })
    return { status: response.status, body: await response.text() }
  } catch (err) {
    const cause = err instanceof Error && err.cause instanceof Error ? `: ${err.cause.message}` : ""
    console.error(`[legato-even-hub] can't reach ${settings.url}: ${String(err)}${cause}`)
    return json(502, { error: "can't reach Legato" })
  }
}

// A recording of a minute is about 2.6 MB as base64.
const MAX_BODY = 8 << 20

function readBody(req: IncomingMessage): Promise<string | undefined> {
  if (req.method !== "POST") return Promise.resolve(undefined)
  return new Promise((done, fail) => {
    const chunks: Buffer[] = []
    let size = 0
    req.on("data", (chunk: Buffer) => {
      size += chunk.length
      if (size > MAX_BODY) {
        fail(new Error("body too large"))
        req.destroy()
        return
      }
      chunks.push(chunk)
    })
    req.on("end", () => done(Buffer.concat(chunks).toString("utf8")))
    req.on("error", fail)
  })
}

// The Vite plugin that serves DEV_PATH.
export function legatoDev(settings: DevSettings): Plugin {
  return {
    name: "legato-dev",
    configureServer(server) {
      const from = settings.kind === "fixture" ? settings.file : settings.url
      const sending = settings.kind === "proxy" ? (settings.allowSend ? ", messages sent" : ", messages refused") : ""
      server.config.logger.info(`  Legato: ${settings.kind} ${from}${sending}`)
      server.middlewares.use(DEV_PATH, (req, res) => {
        readBody(req)
          .then((body) => answerLegato(req.method ?? "GET", req.url ?? "/", body, settings))
          .then(
            (answer) => {
              res.statusCode = answer.status
              res.setHeader("Content-Type", "application/json")
              res.setHeader("Cache-Control", "no-store")
              res.end(answer.body)
            },
            (err: unknown) => {
              res.statusCode = 500
              res.end(JSON.stringify({ error: String(err) }))
            },
          )
      })
    },
  }
}
