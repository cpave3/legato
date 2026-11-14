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
    let sent: Record<string, unknown>
    try {
      sent = JSON.parse(rawBody ?? "")
    } catch {
      return json(400, { error: "the body is not JSON" })
    }
    // Only the field each call needs goes on, rebuilt here: Go's JSON decoder
    // matches keys without regard to case and keeps the last, so a body passed
    // on as sent could turn transcription_only off and have Legato type the
    // text into an agent's pane.
    const field = found.route === "transcribe" ? "pcm" : "content"
    if (typeof sent?.[field] !== "string") return json(400, { error: `${field} must be a string` })
    body = found.route === "transcribe" ? { transcription_only: true, pcm: sent.pcm } : { content: sent.content }
  }
  if (settings.kind === "fixture") return fromFixtures(settings, found.route, found.taskId, body)

  if (found.route === "send" && !settings.allowSend) {
    return json(403, { error: "sending is off: set LEGATO_ALLOW_SEND=1 for a scratch Legato" })
  }

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

// Why a request is refused before it is read, or undefined. The app calls
// this path from its own page, with JSON. A page on another site can make a
// browser send a POST of plain text without asking first, so a POST must be
// JSON (which makes the browser ask, and Vite answers only its own origins),
// and a request the browser marks as from another site is refused.
export function crossSiteRefusal(request: { method: string; contentType?: string; fetchSite?: string }): string | undefined {
  if (request.fetchSite === "cross-site") return "refused: the request came from another site"
  if (request.method === "POST" && request.contentType?.split(";")[0].trim().toLowerCase() !== "application/json") {
    return "refused: the body must be JSON (Content-Type: application/json)"
  }
  return undefined
}

// A recording of a minute is about 2.6 MB as base64.
const MAX_BODY = 8 << 20

class TooLarge extends Error {}

// The body of a POST; past MAX_BODY it is read to its end and dropped, and
// the request is answered 413.
function readBody(req: IncomingMessage): Promise<string | undefined> {
  if (req.method !== "POST") return Promise.resolve(undefined)
  return new Promise((done, fail) => {
    const chunks: Buffer[] = []
    let size = 0
    req.on("data", (chunk: Buffer) => {
      size += chunk.length
      if (size <= MAX_BODY) chunks.push(chunk)
    })
    req.on("end", () => (size > MAX_BODY ? fail(new TooLarge()) : done(Buffer.concat(chunks).toString("utf8"))))
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
        const refused = crossSiteRefusal({
          method: req.method ?? "GET",
          contentType: req.headers["content-type"],
          fetchSite: req.headers["sec-fetch-site"] as string | undefined,
        })
        if (refused) {
          res.statusCode = 403
          res.setHeader("Content-Type", "application/json")
          res.end(JSON.stringify({ error: refused }))
          return
        }
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
              res.statusCode = err instanceof TooLarge ? 413 : 500
              res.setHeader("Content-Type", "application/json")
              res.end(JSON.stringify({ error: err instanceof TooLarge ? "the body is too large" : String(err) }))
            },
          )
      })
    },
  }
}
