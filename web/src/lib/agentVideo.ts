import type { AgentUIItem } from "./agentUi"

// A video an agent rendered with libretto comes with a manifest: what is on
// screen and what is being said at every moment. It is the content of a
// video item. Times are in milliseconds from the start of the video.
export interface VideoManifest {
  version: number
  durationMs: number
  // True when captions are part of the picture already.
  captionsBurned?: boolean
  chapters: VideoChapter[]
  scenes: VideoScene[]
}

export interface VideoChapter {
  title: string
  startMs: number
  endMs: number
}

export interface VideoScene {
  id: string
  kind: "card" | "code" | "image" | string
  chapter?: string
  startMs: number
  durationMs: number
  narration?: string
  source?: VideoSource
  actions?: VideoAction[]
  cues?: VideoCue[]
}

export interface VideoSource {
  file?: string
  diff?: string
  image?: string
  // The file as an absolute path on the machine that rendered the video.
  path?: string
  title?: string
  subtitle?: string
  bullets?: string[]
  // The code on screen, as it was when the video was rendered.
  lines?: { n: number; text: string; mark?: string }[]
}

export interface VideoAction {
  ms: number
  do: string
  lines?: number[]
  rect?: number[]
  index?: number
}

export interface VideoCue {
  startMs: number
  endMs: number
  text: string
}

// The manifest version this page reads (service.agentUIVideoManifestVersion).
const MANIFEST_VERSION = 1

// parseVideoManifest reads a video item's content. It returns null for
// anything that is not a manifest this page can draw.
export function parseVideoManifest(content: string | undefined): VideoManifest | null {
  if (!content) return null
  let parsed: unknown
  try {
    parsed = JSON.parse(content)
  } catch {
    return null
  }
  if (!parsed || typeof parsed !== "object") return null
  const manifest = parsed as Partial<VideoManifest>
  if (manifest.version !== MANIFEST_VERSION || typeof manifest.durationMs !== "number" || !Array.isArray(manifest.scenes) || manifest.scenes.length === 0) {
    return null
  }
  return { ...manifest, chapters: Array.isArray(manifest.chapters) ? manifest.chapters : [] } as VideoManifest
}

// Where the server gives out the image files agents showed
// (service.AgentUIImageRoute): a video's poster is one of them.
const IMAGE_ROUTE = "/agent-ui-images/"

// Where the server gives out the video files agents showed
// (service.AgentUIVideoRoute). It asks for no token: the address of a video
// is the hash of its content.
const VIDEO_ROUTE = "/agent-ui-videos/"

// videoAddress says where an item's video is, at the server the page talks
// to (the page's own when baseUrl is empty). It is null for an item with no
// video, and for an address that is not one of Legato's stored videos.
export function videoAddress(baseUrl: string, item: Pick<AgentUIItem, "video">): string | null {
  const url = item.video?.url
  if (typeof url !== "string" || !url.startsWith(VIDEO_ROUTE)) return null
  const origin = baseUrl || window.location.origin
  try {
    const address = new URL(url, origin)
    return address.origin === new URL(origin).origin && address.pathname.startsWith(VIDEO_ROUTE) ? address.href : null
  } catch {
    return null
  }
}

// posterAddress says where the picture to show before a video is played is.
// It is one of Legato's stored images, and null for a video with none.
export function posterAddress(baseUrl: string, item: Pick<AgentUIItem, "video">): string | null {
  const url = item.video?.poster
  if (typeof url !== "string" || !url.startsWith(IMAGE_ROUTE)) return null
  const origin = baseUrl || window.location.origin
  try {
    const address = new URL(url, origin)
    return address.origin === new URL(origin).origin && address.pathname.startsWith(IMAGE_ROUTE) ? address.href : null
  } catch {
    return null
  }
}

// clock writes a time as m:ss.
export function clock(ms: number): string {
  const seconds = Math.max(0, Math.floor(ms / 1000))
  return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, "0")}`
}

// A moment is what a video shows and says at one time.
export interface VideoMoment {
  ms: number
  durationMs: number
  chapter?: string
  scene: VideoScene
  // The caption on screen, or the last one of this scene when the time
  // falls in a pause.
  saying?: string
  // The first and last row lit in a code scene, when rows are.
  highlighted?: [number, number]
  // The part of an image the picture is zoomed to: x, y, width, height.
  zoomed?: number[]
  // How many bullets of a card are shown so far.
  revealed: number
}

// momentAt reports what the video shows at ms. A time past the end is the
// end. It follows libretto's own reading of a manifest (Manifest.At).
export function momentAt(manifest: VideoManifest, ms: number): VideoMoment {
  const at = Math.max(0, Math.min(ms, manifest.durationMs - 1))
  const scene = manifest.scenes.find((candidate) => at < candidate.startMs + candidate.durationMs) ?? manifest.scenes[manifest.scenes.length - 1]
  const moment: VideoMoment = { ms: at, durationMs: manifest.durationMs, scene, revealed: 0 }
  moment.chapter = manifest.chapters.find((chapter) => at >= chapter.startMs && at < chapter.endMs)?.title
  for (const cue of scene.cues ?? []) {
    if (cue.startMs <= at) moment.saying = cue.text
  }
  const actions = scene.actions ?? []
  for (const action of actions) {
    if (action.ms > at) break
    if (action.do === "highlight" && action.lines?.length === 2) moment.highlighted = [action.lines[0], action.lines[1]]
    if (action.do === "clear") moment.highlighted = undefined
    if (action.do === "zoom") moment.zoomed = action.rect
    if (action.do === "reset") moment.zoomed = undefined
    if (action.do === "reveal") moment.revealed = Math.max(moment.revealed, action.index ?? 0)
  }
  // A card with no reveal actions shows every bullet from the start.
  if (scene.kind === "card" && !actions.some((action) => action.do === "reveal")) {
    moment.revealed = scene.source?.bullets?.length ?? 0
  }
  return moment
}

// How many rows of code a description shows around the highlighted ones.
const CONTEXT_LINES = 3
// How many rows it shows of a code scene with nothing highlighted.
const UNLIT_LINES = 40

// onScreen says in a few words what a moment shows, for the player itself.
export function onScreen(moment: VideoMoment): string {
  const source = moment.scene.source ?? {}
  const name = (path?: string) => (path ? path.split("/").pop() ?? path : "")
  if (moment.scene.kind === "code") {
    const what = source.title || name(source.path) || "code"
    return moment.highlighted ? `${what}, lines ${moment.highlighted[0]} to ${moment.highlighted[1]}` : what
  }
  if (moment.scene.kind === "image") return source.title || name(source.path) || "an image"
  return source.title || "a title card"
}

// describeMoment describes a moment for a reader who cannot see the video:
// the agent that is asked a question about it. The code is the manifest's
// own copy, so the description holds even after the files change.
export function describeMoment(moment: VideoMoment): string {
  const lines: string[] = []
  const where = moment.chapter ? `, in the chapter ${JSON.stringify(moment.chapter)}` : ""
  lines.push(`Video paused at ${clock(moment.ms)} of ${clock(moment.durationMs)}${where}, scene \`${moment.scene.id}\`.`)
  if (moment.saying) lines.push("", `Being said: ${JSON.stringify(moment.saying)}`)

  const source = moment.scene.source ?? {}
  if (moment.scene.kind === "card") {
    const subtitle = source.subtitle ? `, with the subtitle ${JSON.stringify(source.subtitle)}` : ""
    lines.push("", `On screen: a title card, ${JSON.stringify(source.title ?? "")}${subtitle}.`)
    for (const bullet of (source.bullets ?? []).slice(0, moment.revealed)) lines.push(`- ${bullet}`)
  } else if (moment.scene.kind === "image") {
    const title = source.title ? ` (${source.title})` : ""
    const zoom = moment.zoomed?.length === 4
      ? `, zoomed to x ${Math.round(moment.zoomed[0])}, y ${Math.round(moment.zoomed[1])}, ${Math.round(moment.zoomed[2])} by ${Math.round(moment.zoomed[3])} pixels`
      : ""
    lines.push("", `On screen: the image \`${source.path ?? source.image ?? ""}\`${title}${zoom}.`)
  } else if (moment.scene.kind === "code") {
    const rows = source.lines ?? []
    const what = `${source.diff ? "the diff " : ""}\`${source.path ?? source.file ?? source.diff ?? ""}\``
    let first = 0
    let last = rows.length - 1
    let lit = ""
    if (moment.highlighted) {
      const [from, to] = moment.highlighted
      lit = `, with lines ${from} to ${to} highlighted`
      rows.forEach((row, index) => {
        if (row.n === from) first = index
        if (row.n === to) last = index
      })
      first = Math.max(0, first - CONTEXT_LINES)
      last = Math.min(rows.length - 1, last + CONTEXT_LINES)
    } else {
      last = Math.min(last, UNLIT_LINES)
    }
    const range = rows.length > 0 ? `, lines ${rows[0].n} to ${rows[rows.length - 1].n}` : ""
    lines.push("", `On screen: ${what}${range}${lit}.`, "", "```")
    for (const row of rows.slice(first, last + 1)) {
      const isLit = moment.highlighted && row.n >= moment.highlighted[0] && row.n <= moment.highlighted[1]
      const mark = row.mark === "add" ? "+" : row.mark === "del" ? "-" : isLit ? ">" : " "
      lines.push(`${mark} ${String(row.n).padStart(4)}  ${row.text}`)
    }
    lines.push("```")
    if (moment.highlighted) lines.push("Lines marked > are the highlighted ones. The code is as it was when the video was rendered.")
  }
  return lines.join("\n")
}

// questionAbout is the text put in the person's message box when they ask
// about a paused video: what the video showed then, and room for the
// question. The person writes the question and sends it; nothing is sent
// for them.
export function questionAbout(title: string, moment: VideoMoment): string {
  const name = title.trim() ? ` ${JSON.stringify(title.trim())}` : ""
  return `About the video${name}:\n\n${describeMoment(moment)}\n\nMy question: `
}
