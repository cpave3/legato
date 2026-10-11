import { apiFetch } from "./api"
import type { NativeToolItem } from "./nativeAgentSync"

// What an agent asked Legato to show, through its `legato mcp serve` tools:
// an artifact for the side panel, or a card drawn in the conversation.
export interface AgentUIItem {
  id: string
  task_id: string
  placement: "panel" | "inline"
  format: "html" | "markdown" | "components"
  title: string
  key?: string
  revision: number
  oldest_revision: number
  content?: string
  fallback_text?: string
  // Hosts HTML content may load scripts, styles, fonts and images from.
  resource_domains?: string[]
  created_at: string
  updated_at: string
}

export type UITone = "neutral" | "positive" | "warning" | "negative" | "info"

export interface UIComponentItem {
  label?: string
  value?: string
  detail?: string
  tone?: UITone
  text?: string
  number?: number
  message?: string
}

// One entry of a component card: `type` says which other fields apply. The
// server has checked it against the same catalog before storing it.
export interface UIComponent {
  type: string
  text?: string
  label?: string
  value?: string
  detail?: string
  tone?: UITone
  level?: number
  ordered?: boolean
  language?: string
  fraction?: number
  columns?: string[]
  rows?: string[][]
  items?: UIComponentItem[]
}

async function readJSON<T>(response: Response): Promise<T> {
  if (!response.ok) {
    const body = await response.json().catch(() => null) as { error?: string } | null
    throw new AgentUIError(body?.error ?? `request failed (${response.status})`, response.status)
  }
  return response.json() as Promise<T>
}

export class AgentUIError extends Error {
  status: number
  constructor(message: string, status: number) {
    super(message)
    this.status = status
  }
}

export async function fetchAgentUIItems(baseUrl: string, taskId: string): Promise<AgentUIItem[]> {
  const data = await readJSON<{ items: AgentUIItem[] }>(await apiFetch(baseUrl, `/api/agents/${encodeURIComponent(taskId)}/ui-items`))
  return data.items ?? []
}

// revision 0 reads the latest.
export async function fetchAgentUIItem(baseUrl: string, id: string, revision = 0): Promise<AgentUIItem> {
  const query = revision > 0 ? `?revision=${revision}` : ""
  return readJSON<AgentUIItem>(await apiFetch(baseUrl, `/api/ui-items/${encodeURIComponent(id)}${query}`))
}

export async function deleteAgentUIItem(baseUrl: string, id: string): Promise<void> {
  await readJSON<unknown>(await apiFetch(baseUrl, `/api/ui-items/${encodeURIComponent(id)}`, { method: "DELETE" }))
}

export function parseUIComponents(content: string | undefined): UIComponent[] {
  if (!content) return []
  try {
    const parsed: unknown = JSON.parse(content)
    return Array.isArray(parsed) ? parsed as UIComponent[] : []
  } catch {
    return []
  }
}

export type LegatoUITool = "show_artifact" | "show_card" | "show_components"

export interface LegatoUICall {
  tool: LegatoUITool
  // pending: the call is still running. shown: its result names the item it
  // made. failed: it returned without one, so there is an error to show.
  state: "pending" | "shown" | "failed"
  itemId?: string
  revision?: number
  title: string
  fallbackText: string
}

// An MCP server's tools are named by the agent tool that hosts it:
// `mcp__plugin_legato-tools_legato__show_card` in Claude Code,
// `legato.show_card` from the Codex bridge, and in Chimera
// `mcp_legato_show_x00005f_card`, where each character outside [a-zA-Z0-9]
// is written as `_x<six hex digits>_`.
const LEGATO_UI_TOOL = /(?:^|[^a-zA-Z0-9])legato(?:__|[._])(show_artifact|show_card|show_components)$/
const ITEM_MARKER = /\[legato-ui:(ui-[0-9a-f]{10}):(\d+)\]/

function decodedToolName(name: string): string {
  return name.replace(/_x([0-9a-f]{6})_/g, (_match, hex: string) => String.fromCodePoint(parseInt(hex, 16)))
}

// The marker is in the text the model read, which each bridge wraps in its
// own shape, so every string in the result is looked at.
function findMarker(value: unknown, depth = 0): RegExpExecArray | null {
  if (typeof value === "string") return ITEM_MARKER.exec(value)
  if (depth > 4 || value === null || typeof value !== "object") return null
  for (const child of Array.isArray(value) ? value : Object.values(value)) {
    const found = findMarker(child, depth + 1)
    if (found) return found
  }
  return null
}

function stringArg(args: Record<string, unknown>, name: string): string {
  const value = args[name]
  return typeof value === "string" ? value : ""
}

// legatoUICall recognises a tool call that asked Legato to show something,
// whichever agent made it, and returns null for every other call.
export function legatoUICall(tool: NativeToolItem): LegatoUICall | null {
  const name = LEGATO_UI_TOOL.exec(tool.toolName) ?? LEGATO_UI_TOOL.exec(decodedToolName(tool.toolName))
  if (!name) return null
  const args = tool.args ?? {}
  const call: LegatoUICall = {
    tool: name[1] as LegatoUITool,
    state: "pending",
    title: stringArg(args, "title"),
    fallbackText: stringArg(args, "fallback_text"),
  }
  if (tool.result === undefined) return call
  const marker = findMarker(tool.result)
  if (!marker) return { ...call, state: "failed" }
  return { ...call, state: "shown", itemId: marker[1], revision: Number(marker[2]) }
}

// The values below use the variable names of MCP Apps' host styling, so
// content written for one MCP host's theme reads the same here.
const THEME_VARIABLES: Record<string, string> = {
  "--color-background-primary": "#18181b",
  "--color-background-secondary": "#27272a",
  "--color-background-tertiary": "#3f3f46",
  "--color-background-inverse": "#f4f4f5",
  "--color-background-info": "#0c4a6e",
  "--color-background-success": "#064e3b",
  "--color-background-warning": "#78350f",
  "--color-background-danger": "#7f1d1d",
  "--color-text-primary": "#e4e4e7",
  "--color-text-secondary": "#a1a1aa",
  "--color-text-tertiary": "#71717a",
  "--color-text-inverse": "#18181b",
  "--color-text-info": "#7dd3fc",
  "--color-text-success": "#6ee7b7",
  "--color-text-warning": "#fcd34d",
  "--color-text-danger": "#fca5a5",
  "--color-border-primary": "#3f3f46",
  "--color-border-secondary": "#27272a",
  "--color-ring-primary": "#6366f1",
  "--font-sans": "ui-sans-serif, system-ui, -apple-system, 'Segoe UI', Roboto, sans-serif",
  "--font-mono": "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace",
  "--font-text-sm-size": "0.875rem",
  "--font-text-md-size": "1rem",
  "--border-radius-sm": "4px",
  "--border-radius-md": "8px",
  "--border-radius-lg": "12px",
  "--border-width-regular": "1px",
  "--shadow-sm": "0 1px 2px rgba(0, 0, 0, 0.4)",
}

const HOST_NAME = /^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$/

// The script every frame starts with. It runs inside the sandbox, with no
// access to Legato: all it can do is post requests the host may refuse. The
// method names are MCP Apps' own.
const BOOTSTRAP = `(function () {
  function post(method, params) { parent.postMessage({ jsonrpc: "2.0", method: method, params: params || {} }, "*") }
  window.legato = {
    proposeMessage: function (text) { post("ui/message", { role: "user", content: { type: "text", text: String(text) } }) },
    openLink: function (url) { post("ui/open-link", { url: String(url) }) }
  }
  document.addEventListener("click", function (event) {
    var link = event.target && event.target.closest ? event.target.closest("a[href]") : null
    if (!link) return
    var href = link.getAttribute("href") || ""
    if (href.charAt(0) === "#") return
    event.preventDefault()
    post("ui/open-link", { url: link.href })
  }, true)
  function report() {
    if (!document.body) return
    post("ui/notifications/size-changed", { width: Math.ceil(document.body.scrollWidth), height: Math.ceil(document.body.getBoundingClientRect().height) })
  }
  window.addEventListener("load", report)
  document.addEventListener("DOMContentLoaded", function () {
    report()
    if (window.ResizeObserver) new ResizeObserver(report).observe(document.body)
  })
})()`

export interface SandboxDocumentInput {
  html: string
  resourceDomains: string[]
  // fill: the frame is as tall as its container and the content scrolls
  // inside it (the side panel). Otherwise the frame grows to its content.
  fill: boolean
}

// buildSandboxDocument wraps an agent's HTML in the document a sandboxed
// frame draws: a content security policy ahead of everything the agent wrote,
// Legato's theme, and the bootstrap script. The policy allows no network
// except loading scripts, styles, fonts and images from the allowed hosts.
export function buildSandboxDocument({ html, resourceDomains, fill }: SandboxDocumentInput): string {
  const hosts = resourceDomains.filter((domain) => HOST_NAME.test(domain)).map((domain) => `https://${domain}`).join(" ")
  const withHosts = (base: string) => (hosts ? `${base} ${hosts}` : base)
  const policy = [
    "default-src 'none'",
    `script-src ${withHosts("'unsafe-inline'")}`,
    `style-src ${withHosts("'unsafe-inline'")}`,
    `img-src ${withHosts("data: blob:")}`,
    `font-src ${withHosts("data:")}`,
    "media-src data: blob:",
    "connect-src 'none'",
    "frame-src 'none'",
    "child-src 'none'",
    "object-src 'none'",
    "form-action 'none'",
    "base-uri 'none'",
  ].join("; ")
  const variables = Object.entries(THEME_VARIABLES).map(([name, value]) => `${name}: ${value};`).join(" ")
  const baseStyle = `:root { color-scheme: dark; ${variables} }
html { background: var(--color-background-primary); }
body { margin: 0; padding: ${fill ? "20px" : "12px"}; color: var(--color-text-primary); font-family: var(--font-sans); font-size: 14px; line-height: 1.5; overflow-wrap: anywhere; ${fill ? "" : "overflow-x: auto;"} }
a { color: #a5b4fc; }
img, svg, video, canvas { max-width: 100%; }
table { border-collapse: collapse; }
pre, code { font-family: var(--font-mono); }`
  return `<!doctype html><html><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="${policy}"><meta name="referrer" content="no-referrer"><meta name="viewport" content="width=device-width, initial-scale=1"><style>${baseStyle}</style><script>${BOOTSTRAP}</script></head><body>${html}</body></html>`
}

const MAX_PROPOSED_MESSAGE = 4000

// proposedMessageText reads the params of a view's `ui/message` request: the
// text it asks to put in the person's message box. A view never sends a
// message; the person does, after reading what was put there.
export function proposedMessageText(params: unknown): string | null {
  if (!params || typeof params !== "object") return null
  const content = (params as { content?: unknown }).content
  if (!content || typeof content !== "object") return null
  const { type, text } = content as { type?: unknown; text?: unknown }
  if (type !== "text" || typeof text !== "string") return null
  const trimmed = text.trim().slice(0, MAX_PROPOSED_MESSAGE)
  return trimmed === "" ? null : trimmed
}

// A view's request to put text in an agent's message box travels as a window
// event, so the conversation's composer and the side panel need not know of
// each other.
const PROPOSAL_EVENT = "legato:propose-message"

export function proposeComposerText(agentId: string, text: string) {
  window.dispatchEvent(new CustomEvent(PROPOSAL_EVENT, { detail: { agentId, text } }))
}

export function onComposerProposal(agentId: string, handler: (text: string) => void): () => void {
  const listener = (event: Event) => {
    const detail = (event as CustomEvent<{ agentId?: string; text?: string }>).detail
    if (detail?.agentId === agentId && typeof detail.text === "string") handler(detail.text)
  }
  window.addEventListener(PROPOSAL_EVENT, listener)
  return () => window.removeEventListener(PROPOSAL_EVENT, listener)
}

// appendProposal adds proposed text to what the person has already typed,
// never replacing it.
export function appendProposal(draft: string, text: string): string {
  return draft.trim() === "" ? text : `${draft.replace(/\s+$/, "")}\n${text}`
}
