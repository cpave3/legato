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
  // The image files this revision uses: the name its content refers to each
  // by, and where the server gives out Legato's copy.
  images?: AgentUIImage[]
  created_at: string
  updated_at: string
}

export interface AgentUIImage {
  name: string
  // An address on the server, without its origin.
  url: string
}

// An image of an item as a page draws it: its name and its whole address.
export interface ItemImage {
  name: string
  address: string
}

// Where the server gives out the image files agents showed
// (service.AgentUIImageRoute). It asks for no token: the address of an image
// is the hash of its content.
const IMAGE_ROUTE = "/agent-ui-images/"

// itemImages says where each of an item's images is, at the server the page
// talks to (the page's own when baseUrl is empty). An address that is not
// one of Legato's stored images is left out: these addresses go into a
// frame's content security policy and into pages a person opens.
export function itemImages(baseUrl: string, item: Pick<AgentUIItem, "images">): ItemImage[] {
  const origin = baseUrl || window.location.origin
  return (item.images ?? []).flatMap(({ name, url }) => {
    if (typeof url !== "string" || !url.startsWith(IMAGE_ROUTE)) return []
    try {
      const address = new URL(url, origin)
      return address.origin === new URL(origin).origin && address.pathname.startsWith(IMAGE_ROUTE) ? [{ name, address: address.href }] : []
    } catch {
      return []
    }
  })
}

// The kinds of image the server gives out (agentUIImageExtensions in
// internal/service/agent_ui_images.go).
const IMAGE_TYPE = /^image\/(png|jpeg|gif|webp|bmp)$/

function base64(bytes: Uint8Array): string {
  let binary = ""
  // In pieces: a whole image is more arguments than a call may take.
  for (let at = 0; at < bytes.length; at += 0x8000) binary += String.fromCharCode(...bytes.subarray(at, at + 0x8000))
  return btoa(binary)
}

// inlineImages reads each image and returns it with a data: address, for a
// page that must carry its images with it. An image that cannot be read
// keeps the address it had.
export function inlineImages(images: ItemImage[]): Promise<ItemImage[]> {
  return Promise.all(images.map(async (image) => {
    try {
      const response = await fetch(image.address)
      const type = (response.headers.get("Content-Type") ?? "").split(";")[0].trim()
      if (!response.ok || !IMAGE_TYPE.test(type)) return image
      return { name: image.name, address: `data:${type};base64,${base64(new Uint8Array(await response.arrayBuffer()))}` }
    } catch {
      return image
    }
  }))
}

// How content refers to one of its images: legato-image:<name>.
const IMAGE_REFERENCE = /legato-image:([A-Za-z0-9][A-Za-z0-9_-]*)/g

// resolveImageReferences puts each image's address where the content names
// the image. A name the item does not have is left as it was written.
export function resolveImageReferences(content: string, images: ItemImage[]): string {
  if (images.length === 0) return content
  const addresses = new Map(images.map((image) => [image.name, image.address]))
  return content.replace(IMAGE_REFERENCE, (reference, name: string) => addresses.get(name) ?? reference)
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
  path?: string
  alt?: string
  caption?: string
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
  // image: the file the agent named. The item's images say where Legato's
  // copy of it is.
  path?: string
  alt?: string
  caption?: string
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

// What stands in for the host in a page saved from Legato. There is no
// message box out there, so proposing a message does nothing, and a link
// just opens. A script that calls either must not fail for want of them.
const STANDALONE_STUB = `window.legato = {
  proposeMessage: function () {},
  openLink: function (url) { window.open(String(url), "_blank", "noopener") }
}`

// The Mermaid release a frame loads: the one the web UI itself bundles, and
// the one every statement about diagrams in a frame was checked against.
const MERMAID_VERSION = "11.16.0"

const MERMAID_SOURCES: [host: string, url: string][] = [
  ["cdn.jsdelivr.net", `https://cdn.jsdelivr.net/npm/mermaid@${MERMAID_VERSION}/dist/mermaid.min.js`],
  ["unpkg.com", `https://unpkg.com/mermaid@${MERMAID_VERSION}/dist/mermaid.min.js`],
]

// mermaidScriptURL is where a frame may load Mermaid from, or "" when none of
// the hosts it may load from serves it.
function mermaidScriptURL(resourceDomains: string[]): string {
  return MERMAID_SOURCES.find(([host]) => resourceDomains.includes(host))?.[1] ?? ""
}

// The colors a node can be given by name in a diagram's source ("A:::blue"),
// so an agent can tell kinds of thing apart without choosing colors that
// suit a dark page.
const DIAGRAM_HUES: Record<string, string> = {
  blue: "#60a5fa",
  cyan: "#22d3ee",
  green: "#34d399",
  amber: "#fbbf24",
  red: "#f87171",
  pink: "#f472b6",
  purple: "#a78bfa",
  gray: "#a1a1aa",
}

// tint is a hue laid thinly over the page's background: a node's fill.
function tint(hue: string, amount: number): string {
  const channels = (color: string) => [1, 3, 5].map((at) => parseInt(color.slice(at, at + 2), 16))
  const [from, to] = [channels(THEME_VARIABLES["--color-background-primary"]), channels(hue)]
  return `#${from.map((channel, index) => Math.round(channel + (to[index] - channel) * amount).toString(16).padStart(2, "0")).join("")}`
}

// diagramStyle is what Legato adds to Mermaid's own style sheet for a
// diagram: softer shapes, the named colors, and how a node that is a button
// shows it. Mermaid scopes it to the diagram, ahead of its own rules.
function diagramStyle(): string {
  const shape = (node: string) => `${node} .label-container, ${node} .label-container path, ${node} > rect, ${node} > circle, ${node} > ellipse, ${node} > polygon, ${node} > path`
  const hues = Object.entries(DIAGRAM_HUES).map(([name, hue]) =>
    `${shape(`.node.${name}`)} { fill: ${tint(hue, 0.16)} !important; stroke: ${hue} !important; } .node.${name} { color: ${hue}; }`).join(" ")
  return `.node rect { rx: 8px; ry: 8px; } .cluster rect { rx: 12px; ry: 12px; stroke-dasharray: 4 4; } .nodeLabel { font-weight: 600; } .cluster-label .nodeLabel { font-weight: 500; }
${hues}
.node[role="button"] { cursor: pointer; outline: none; }
.node[role="button"]:hover, .node[role="button"]:focus-visible { filter: brightness(1.3); }
${shape('.node[aria-pressed="true"]')} { stroke: currentColor !important; stroke-width: 2.5px !important; }
.node[aria-pressed="true"] { filter: drop-shadow(0 0 5px currentColor); }`
}

// mermaidConfig is how a frame sets Mermaid up: Legato's colors and font in
// place of Mermaid's own look.
function mermaidConfig() {
  const theme = THEME_VARIABLES
  const [page, raised, line] = [theme["--color-background-primary"], theme["--color-background-secondary"], theme["--color-background-tertiary"]]
  const [text, quiet, faint] = [theme["--color-text-primary"], theme["--color-text-secondary"], theme["--color-text-tertiary"]]
  return {
    startOnLoad: false,
    // The frame is the sandbox, and the agent's own script already runs in
    // it: a label may hold markup, and a diagram's own click lines work.
    securityLevel: "loose",
    // A diagram Mermaid cannot read must not leave its error drawing behind.
    suppressErrorRendering: true,
    theme: "base",
    fontFamily: theme["--font-sans"],
    themeVariables: {
      darkMode: true,
      background: page,
      fontSize: "14px",
      primaryColor: raised,
      primaryTextColor: text,
      primaryBorderColor: "#52525b",
      secondaryColor: line,
      tertiaryColor: raised,
      lineColor: faint,
      textColor: text,
      titleColor: quiet,
      clusterBkg: tint("#ffffff", 0.03),
      clusterBorder: line,
      edgeLabelBackground: page,
      actorBkg: raised,
      actorBorder: "#52525b",
      actorTextColor: text,
      actorLineColor: line,
      signalColor: quiet,
      signalTextColor: text,
      labelBoxBkgColor: raised,
      labelBoxBorderColor: "#52525b",
      labelTextColor: text,
      loopTextColor: quiet,
      noteBkgColor: line,
      noteBorderColor: "#52525b",
      noteTextColor: text,
      activationBkgColor: line,
      activationBorderColor: faint,
    },
    themeCSS: diagramStyle(),
  }
}

// diagramsScript is the part of the frame's script that draws Mermaid
// diagrams: it adds legato.diagrams to the object the page already has. It
// runs inside the sandbox like the rest, and hides from the agent what a
// diagram needs to come out right there.
function diagramsScript(resourceDomains: string[]): string {
  return `(function () {
  var MERMAID_URL = ${JSON.stringify(mermaidScriptURL(resourceDomains))}
  var MERMAID_CONFIG = ${JSON.stringify(mermaidConfig()).replace(/</g, "\\u003c")}
  var loading = null
  var count = 0
  // While a diagram is on its way its source is not shown (see the theme's
  // style). Whatever happens, this ends: text that will never be a diagram
  // must be readable.
  var root = document.documentElement
  if (MERMAID_URL) root.setAttribute("data-diagrams", "loading")
  function settled() { root.removeAttribute("data-diagrams") }
  // A page that brought a Mermaid of its own keeps it.
  function library() {
    if (window.mermaid) return Promise.resolve(window.mermaid)
    if (!MERMAID_URL) return Promise.resolve(null)
    if (!loading) loading = new Promise(function (resolve) {
      var script = document.createElement("script")
      script.src = MERMAID_URL
      script.onload = function () { resolve(window.mermaid || null) }
      script.onerror = function () { resolve(null) }
      document.head.appendChild(script)
    })
    return loading
  }
  // What the agent wrote, as Mermaid reads a block: the markup itself, so a
  // <br> in a label stays, with the characters HTML escaped put back.
  function sourceOf(block) {
    return block.innerHTML.replace(/&lt;/g, "<").replace(/&gt;/g, ">").replace(/&nbsp;/g, " ").replace(/&amp;/g, "&").trim()
  }
  // Mermaid names a node's group "<diagram>-<kind>-<the name in the source>-<number>".
  // The page is told the name: the rest changes between Mermaid releases.
  var NODE_ID = /(?:^|-)(?:flowchart|state|classId|entity)-(.+)-\\d+$/
  function choose(block, node, onNode) {
    Array.prototype.forEach.call(block.querySelectorAll("[data-node-id]"), function (other) {
      other.setAttribute("aria-pressed", String(other === node))
    })
    // A diagram wider than the panel scrolls sideways, and the page's own
    // controls can choose a node that is out of view.
    if (node.scrollIntoView) node.scrollIntoView({ block: "nearest", inline: "nearest", behavior: "smooth" })
    onNode(node.getAttribute("data-node-id"), node)
  }
  // Each node becomes a button the person can click or reach with the keyboard.
  function wire(block, onNode) {
    Array.prototype.forEach.call(block.querySelectorAll("g.node"), function (node) {
      var name = NODE_ID.exec(node.id)
      if (!name) return
      node.setAttribute("data-node-id", name[1])
      node.setAttribute("role", "button")
      node.setAttribute("tabindex", "0")
      node.setAttribute("aria-pressed", "false")
      node.addEventListener("click", function () { choose(block, node, onNode) })
      node.addEventListener("keydown", function (event) {
        if (event.key !== "Enter" && event.key !== " ") return
        event.preventDefault()
        choose(block, node, onNode)
      })
    })
  }
  function draw(mermaid, block, options) {
    return mermaid.render("legato-diagram-" + (++count), sourceOf(block)).then(function (result) {
      block.innerHTML = result.svg
      block.setAttribute("data-processed", "true")
      if (result.bindFunctions) result.bindFunctions(block)
      // Mermaid lets a diagram shrink with its container without limit, and
      // in a narrow panel the text becomes too small to read. It stops where
      // its 14px text is 10px, and the block scrolls sideways from there.
      var svg = block.querySelector("svg")
      var natural = svg ? parseFloat(svg.style.maxWidth) : 0
      if (natural > 0) svg.style.minWidth = Math.round(natural * 0.7) + "px"
      if (typeof options.onNode === "function") wire(block, options.onNode)
    }, function (error) {
      // What Mermaid could not read stays as the text the agent wrote, and
      // says why. The other diagrams are still drawn.
      block.setAttribute("title", "Mermaid could not draw this: " + (error && error.message ? error.message : error))
    })
  }
  // A script may ask before the browser has read the blocks that follow it.
  function parsed() {
    if (document.readyState !== "loading") return Promise.resolve()
    return new Promise(function (resolve) { document.addEventListener("DOMContentLoaded", function () { resolve() }) })
  }
  var asked = false
  // Without the library a block stays as the text the agent wrote, which
  // still reads, and the page's own script carries on.
  window.legato.diagrams = function (options) {
    asked = true
    options = options || {}
    return Promise.all([library(), parsed()]).then(function (ready) {
      var mermaid = ready[0]
      if (!mermaid) return
      mermaid.initialize(MERMAID_CONFIG)
      var blocks = Array.prototype.slice.call(document.querySelectorAll(".mermaid:not([data-processed])"))
      return blocks.reduce(function (drawn, block) { return drawn.then(function () { return draw(mermaid, block, options) }) }, Promise.resolve())
    }).then(settled, function (error) { settled(); throw error })
  }
  // The page's own controls (a list, a "next" button) choose a node the way
  // a click on it does. A name no node has does nothing.
  window.legato.selectNode = function (id) {
    var nodes = document.querySelectorAll("[data-node-id]")
    for (var at = 0; at < nodes.length; at++) {
      if (nodes[at].getAttribute("data-node-id") !== String(id)) continue
      nodes[at].dispatchEvent(new Event("click", { bubbles: true }))
      return
    }
  }
  // A page that only wrote the blocks gets them drawn too. One that brought a
  // Mermaid of its own draws them itself.
  window.addEventListener("load", function () {
    if (asked) return
    if (!window.mermaid && document.querySelector(".mermaid")) window.legato.diagrams()
    else settled()
  })
})()`
}

// imagesScript is the part of the frame's script about the image files an
// agent showed: legato.image gives a script the address of one by its name,
// and a click on one asks the host to show it full size. The frame is as
// narrow as the panel, so the larger view is the host's to draw.
function imagesScript(images: ItemImage[], zoom: boolean): string {
  const addresses = Object.fromEntries(images.map((image) => [image.name, image.address]))
  return `(function () {
  var IMAGES = ${JSON.stringify(addresses).replace(/</g, "\\u003c")}
  window.legato.image = function (name) { return IMAGES[String(name)] || "" }
${zoom ? IMAGE_ZOOM : ""}})()`
}

// The part of imagesScript that opens an image full size. A page saved from
// Legato is without it: nothing out there draws the larger view.
const IMAGE_ZOOM = `  var known = {}
  Object.keys(IMAGES).forEach(function (name) { known[IMAGES[name]] = true })
  function post(method, params) { parent.postMessage({ jsonrpc: "2.0", method: method, params: params || {} }, "*") }
  // An image opens unless the page said otherwise, or made it part of a
  // control of its own.
  function opens(node) {
    if (!node || node.tagName !== "IMG" || !known[node.src] || !node.closest) return false
    return !node.closest('[data-legato-zoom="off"], a[href], button')
  }
  function all() { return Array.prototype.filter.call(document.images, opens) }
  function caption(image) {
    var figure = image.closest("figure")
    var text = figure ? figure.querySelector("figcaption") : null
    return text ? text.textContent.trim() : ""
  }
  function show(image) {
    var images = all()
    post("legato/show-image", {
      index: images.indexOf(image),
      images: images.map(function (other) { return { url: other.src, alt: other.alt || "", caption: caption(other) } })
    })
  }
  document.addEventListener("click", function (event) {
    if (!opens(event.target)) return
    event.preventDefault()
    show(event.target)
  })
  document.addEventListener("keydown", function (event) {
    if ((event.key !== "Enter" && event.key !== " ") || !opens(event.target)) return
    event.preventDefault()
    show(event.target)
  })
  // The person is shown which images open, and can reach them with the
  // keyboard. A script may add images later.
  function mark() {
    // A frame that is being taken down has no document left to look at.
    if (typeof document === "undefined") return
    all().forEach(function (image) {
      if (image.getAttribute("data-legato-zoom") !== "on") image.setAttribute("data-legato-zoom", "on")
      if (!image.hasAttribute("tabindex")) image.setAttribute("tabindex", "0")
    })
  }
  document.addEventListener("DOMContentLoaded", function () {
    mark()
    if (window.MutationObserver) new MutationObserver(mark).observe(document.body, { childList: true, subtree: true, attributes: true, attributeFilter: ["src"] })
  })
`

// imageSources are what a frame's policy must allow for it to load an item's
// images: the folder of stored images at each server they are on, and never
// the API beside it. An image already inside the document needs nothing.
function imageSources(images: ItemImage[]): string[] {
  const sources = new Set<string>()
  for (const { address } of images) {
    try {
      const url = new URL(address)
      if ((url.protocol === "http:" || url.protocol === "https:") && url.pathname.startsWith(IMAGE_ROUTE)) sources.add(`${url.origin}${IMAGE_ROUTE}`)
    } catch {
      // Not an address: nothing to allow.
    }
  }
  return [...sources]
}

// contentPolicy is the content security policy every page of agent HTML
// starts with. It allows no network except loading scripts, styles, fonts and
// images from the allowed hosts. Anything that is not a plain host name is
// dropped, so a value can never rewrite the policy. images are the item's own
// image files, which are served by Legato.
function contentPolicy(resourceDomains: string[], images: ItemImage[] = []): string {
  const hosts = resourceDomains.filter((domain) => HOST_NAME.test(domain)).map((domain) => `https://${domain}`).join(" ")
  const withHosts = (base: string) => (hosts ? `${base} ${hosts}` : base)
  return [
    "default-src 'none'",
    `script-src ${withHosts("'unsafe-inline'")}`,
    `style-src ${withHosts("'unsafe-inline'")}`,
    `img-src ${[withHosts("data: blob:"), ...imageSources(images)].join(" ")}`,
    `font-src ${withHosts("data:")}`,
    "media-src data: blob:",
    "connect-src 'none'",
    "frame-src 'none'",
    "child-src 'none'",
    "object-src 'none'",
    "form-action 'none'",
    "base-uri 'none'",
  ].join("; ")
}

// themeStyle is Legato's theme as a style sheet: the variables agent HTML is
// written against, and the page's base look. fill is a page that owns its
// whole view (the side panel, a saved file); otherwise a card that is as tall
// as its content.
function themeStyle(fill: boolean): string {
  const variables = Object.entries(THEME_VARIABLES).map(([name, value]) => `${name}: ${value};`).join(" ")
  return `:root { color-scheme: dark; ${variables} }
html { background: var(--color-background-primary); }
body { margin: 0; padding: ${fill ? "20px" : "12px"}; color: var(--color-text-primary); font-family: var(--font-sans); font-size: 14px; line-height: 1.5; overflow-wrap: anywhere; ${fill ? "" : "overflow-x: auto;"} }
a { color: #a5b4fc; }
img, svg, video, canvas { max-width: 100%; }
table { border-collapse: collapse; }
pre, code { font-family: var(--font-mono); }
img[data-legato-zoom="on"] { cursor: zoom-in; }
img[data-legato-zoom="on"]:focus-visible { outline: 2px solid var(--color-ring-primary); outline-offset: 2px; }
.legato-image-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(120px, 1fr)); gap: 8px; margin: 12px 0; }
.legato-image-grid figure { margin: 0; min-width: 0; }
.legato-image-grid img { display: block; width: 100%; aspect-ratio: 4 / 3; object-fit: cover; border: 1px solid var(--color-border-primary); border-radius: var(--border-radius-md); background: var(--color-background-secondary); }
.legato-image-grid figcaption { margin-top: 4px; font-size: 12px; line-height: 1.3; color: var(--color-text-secondary); }
.mermaid { overflow-x: auto; }
.mermaid[data-processed] { margin: 0; text-align: center; }
[data-diagrams="loading"] .mermaid:not([data-processed]) { visibility: hidden; }`
}

function escapeHTML(text: string): string {
  return text.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;")
}

export interface SandboxDocumentInput {
  html: string
  resourceDomains: string[]
  // fill: the frame is as tall as its container and the content scrolls
  // inside it (the side panel). Otherwise the frame grows to its content.
  fill: boolean
  // The image files the HTML uses, by the names it refers to them by.
  images?: ItemImage[]
}

// buildSandboxDocument wraps an agent's HTML in the document a sandboxed
// frame draws: a content security policy ahead of everything the agent wrote,
// Legato's theme, and the bootstrap script.
export function buildSandboxDocument({ html, resourceDomains, fill, images = [] }: SandboxDocumentInput): string {
  return `<!doctype html><html><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="${contentPolicy(resourceDomains, images)}"><meta name="referrer" content="no-referrer"><meta name="viewport" content="width=device-width, initial-scale=1"><style>${themeStyle(fill)}</style><script>${BOOTSTRAP}</script><script>${diagramsScript(resourceDomains)}</script><script>${imagesScript(images, true)}</script></head><body>${resolveImageReferences(html, images)}</body></html>`
}

export interface StandaloneDocumentInput {
  html: string
  title: string
  resourceDomains: string[]
  // The image files the HTML uses. An address here is best a data: one, so
  // that the file carries its images and opens without Legato.
  images?: ItemImage[]
}

// buildStandaloneDocument is an agent's HTML as a file to keep: a whole page
// that looks as it does in Legato when opened anywhere else. The agent wrote
// its styles against Legato's theme, which the frame supplies and a saved
// file would be without, so the theme goes into the file. The same content
// security policy goes with it: outside Legato nothing else limits what the
// agent's scripts may reach.
export function buildStandaloneDocument({ html, title, resourceDomains, images = [] }: StandaloneDocumentInput): string {
  // Nothing out there draws an image full size, so the page offers none.
  return `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="${contentPolicy(resourceDomains, images)}"><meta name="referrer" content="no-referrer"><meta name="viewport" content="width=device-width, initial-scale=1"><title>${escapeHTML(title)}</title><style>${themeStyle(true)}</style><script>${STANDALONE_STUB}</script><script>${diagramsScript(resourceDomains)}</script><script>${imagesScript(images, false)}</script></head><body>${resolveImageReferences(html, images)}</body></html>`
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
