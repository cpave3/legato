import { apiFetch } from "./api"

/** ChatBotState is how a chat bot's connection stands. elsewhere: another
 * Legato process on this machine holds the connection. */
export type ChatBotState = "disabled" | "connecting" | "connected" | "backing_off" | "auth_failed" | "elsewhere"

/** A chat bot's status (GET /api/settings/integrations/chat-bots); it
 * never carries a token. */
export interface ChatBotStatus {
  name: string
  platform: string
  enabled: boolean
  configured: boolean
  default_channel?: string
  state: ChatBotState | string
  last_error?: string
  since: string
  next_attempt?: string
}

async function ok(resp: Response): Promise<Response> {
  if (resp.ok) return resp
  let message = `${resp.status} ${resp.statusText}`.trim()
  try {
    const body = await resp.json()
    if (body?.error) message = body.error
  } catch { /* not JSON */ }
  throw new Error(message)
}

/** Every chat bot's status. */
export async function fetchChatBots(baseUrl: string): Promise<ChatBotStatus[]> {
  const resp = await ok(await apiFetch(baseUrl, "/api/settings/integrations/chat-bots"))
  const body = await resp.json() as { bots?: ChatBotStatus[] }
  return body.bots ?? []
}

/** Connects a bot again now; returns every bot's status. */
export async function reconnectChatBot(baseUrl: string, name: string): Promise<ChatBotStatus[]> {
  const resp = await ok(await apiFetch(baseUrl, `/api/settings/integrations/chat-bots/${encodeURIComponent(name)}/reconnect`, { method: "POST" }))
  const body = await resp.json() as { bots?: ChatBotStatus[] }
  return body.bots ?? []
}

const stateLabels: Record<string, string> = {
  connected: "Connected",
  connecting: "Connecting",
  backing_off: "Backing off",
  auth_failed: "Auth failed",
  disabled: "Disabled",
  elsewhere: "Connected in another Legato process",
}

/** A state in plain words. */
export function chatBotStateLabel(state: string): string {
  return stateLabels[state] ?? state
}

/** Whether Reconnect is worth offering in a state. */
export function canReconnect(state: string): boolean {
  return state === "auth_failed" || state === "backing_off"
}

/** The bots the workflow builder offers: those set up, else slack and
 * discord, so a workflow can still be written before either is. */
export function builderBots(bots: ChatBotStatus[] | undefined): string[] {
  const set = (bots ?? []).filter((bot) => bot.configured).map((bot) => bot.name)
  return set.length > 0 ? set : ["slack", "discord"]
}
