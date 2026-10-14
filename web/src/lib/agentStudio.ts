import { apiFetch } from "./api"

// A libretto studio, as Legato's panel talks to it. libretto serves the
// studio on the machine Legato runs on; the page reaches it through Legato's
// own server, under the studio item it belongs to. The shapes here are
// libretto's (internal/studio in the libretto repository), at the level
// STUDIO_API.

export const STUDIO_API = 5

// studioPace is how long the studio's panel waits where it waits. A test
// asks for less.
export const studioPace = {
  // The count from three before a take starts, in all.
  countdownMs: 3000,
}

export interface StudioTake {
  // The recording as the storyboard names it.
  src: string
  // Where the studio plays it from.
  url: string
  startMs?: number
  endMs?: number
  // The stretches between the two that the scene leaves out.
  skip?: number[][]
  // How long the whole recording is.
  durationMs?: number
  current?: boolean
}

// A word of a take, with when it was said: milliseconds into the recording.
export interface StudioWord {
  text: string
  startMs: number
  endMs: number
}

// A stretch of a take in which the person left the plan, with what the
// agent says of it and, once they have chosen, what the person decided.
export interface StudioOffPlan {
  from: number
  to: number
  about?: string
  note: string
  decision?: "add" | "keep" | "cut"
}

// What the agent made of a take: where each talking point (counted from 1)
// was covered, and where the person left the plan. Times are milliseconds
// into the recording.
export interface StudioReview {
  scene?: string
  points?: { point: number; from: number; to: number }[]
  offPlan?: StudioOffPlan[]
}

export interface StudioScene {
  id: string
  kind: string
  chapter?: string
  notes?: string
  // What the scene says: the narration as written, or what was heard.
  says?: string
  // What its take says, whatever the narration says.
  heard?: string
  // How much of a written narration is in the take.
  match?: { found: number; total: number }
  // recorded: a take of the person's. waiting: for one. voice: a voice
  // says its narration. silent: nothing is said.
  status: "recorded" | "waiting" | "voice" | "silent"
  // The address of the scene's picture, inside the studio.
  still: string
  take?: StudioTake
  takes: StudioTake[]
  // What the person wrote about the scene for the agent, oldest first. The
  // agent deletes one when it has dealt with it.
  comments?: string[]
  // What the scene's picture is to show, in a line. planned is true while
  // that line is all there is: the picture has not been drawn.
  shows?: string
  planned?: boolean
  // How long the scene runs; absent while it waits for a take.
  durationMs?: number
  // The words of the part of the take the scene uses.
  transcript?: StudioWord[]
  // Absent until the agent has looked at the take.
  review?: StudioReview
}

export interface StudioProject {
  api: number
  title: string
  storyboard: string
  face: string
  // What stops the storyboard from rendering.
  problems: string[]
  warnings: string[]
  // Why the last take could not be used, when it could not.
  notice?: string
  // What the person wants the video to cover, in their words.
  brief?: string
  // What the storyboard holds for the agent about the video as a whole: a
  // scene the person removed.
  comments?: string[]
  // How long the video will be, as far as that is known.
  durationMs?: number
  scenes: StudioScene[]
  // file is the whole path of the video on the machine libretto runs on;
  // a libretto from before it said so gives none.
  render: { running: boolean; log: string[]; error?: string; video?: string; file?: string; stale?: boolean }
}

// StudioError is a studio that cannot be reached, with a code for why:
// libretto_missing and libretto_old are the person's to fix; legato_old is
// a studio newer than this page.
export class StudioError extends Error {
  code: string
  constructor(code: string, message: string) {
    super(message)
    this.name = "StudioError"
    this.code = code
  }
}

// studioAddress is where a path of the studio is reached through Legato.
export function studioAddress(itemId: string, path: string): string {
  return `/api/ui-items/${encodeURIComponent(itemId)}/studio${path}`
}

async function studioCall(baseUrl: string, itemId: string, path: string, init?: RequestInit): Promise<Response> {
  const res = await apiFetch(baseUrl, studioAddress(itemId, path), init)
  if (res.ok) return res
  const body = await res.json().catch(() => null) as { error?: string; code?: string } | null
  throw new StudioError(body?.code ?? "studio_failed", body?.error ?? `The studio answered ${res.status}.`)
}

async function projectFrom(res: Response): Promise<StudioProject> {
  const project = await res.json().catch(() => null) as StudioProject | null
  if (!project || !Array.isArray(project.scenes)) throw new StudioError("studio_failed", "The studio's answer could not be read.")
  // A studio at a higher level may mean something else by the same words.
  if (project.api > STUDIO_API) {
    throw new StudioError("legato_old", `The libretto on this machine has a newer studio (level ${project.api}) than this Legato knows (level ${STUDIO_API}). To use it, update Legato.`)
  }
  return project
}

export async function fetchStudioProject(baseUrl: string, itemId: string): Promise<StudioProject> {
  return projectFrom(await studioCall(baseUrl, itemId, "/api/project"))
}

// NOTIFY is the header that asks Legato to tell the task's agent what the
// person just did in the studio: a take to review, a comment to act on.
// Without it nothing is said, and the agent learns of it when asked.
export const NOTIFY = "X-Legato-Studio-Notify"

function told(notify: boolean): Record<string, string> {
  return notify ? { [NOTIFY]: "1" } : {}
}

// uploadTake gives the studio a take of one scene: the recording of its
// sound, and with the camera on a second recording, of camera and sound
// together, for the picture. The studio keeps the picture of the second
// with the sound of the first, writes down what was said, and names the
// take in the storyboard.
export async function uploadTake(baseUrl: string, itemId: string, sceneId: string, sound: Blob, picture?: Blob, notify = false): Promise<StudioProject> {
  const path = `/api/scenes/${encodeURIComponent(sceneId)}/takes`
  if (!picture) {
    return projectFrom(await studioCall(baseUrl, itemId, path, { method: "POST", headers: { "Content-Type": sound.type || "audio/webm", ...told(notify) }, body: sound }))
  }
  // The browser writes the form's own content type, with its boundary.
  const form = new FormData()
  form.append("take", picture, "take")
  form.append("sound", sound, "sound")
  return projectFrom(await studioCall(baseUrl, itemId, path, { method: "POST", headers: told(notify), body: form }))
}

// commentOnScene leaves the agent a comment on a scene. It is kept in the
// storyboard file, which is where the agent reads it.
export async function commentOnScene(baseUrl: string, itemId: string, sceneId: string, text: string, notify = false): Promise<StudioProject> {
  return projectFrom(await studioCall(baseUrl, itemId, `/api/scenes/${encodeURIComponent(sceneId)}/comments`, {
    method: "POST", headers: { "Content-Type": "application/json", ...told(notify) }, body: JSON.stringify({ text }),
  }))
}

// decideOffPlan is the person's answer to a stretch of their take that left
// the plan, named by its place in the review, counted from 1: add a scene
// for it (the agent is asked to), keep it where it is, or cut it out.
export async function decideOffPlan(baseUrl: string, itemId: string, sceneId: string, place: number, decision: "add" | "keep" | "cut", notify = false): Promise<StudioProject> {
  return projectFrom(await studioCall(baseUrl, itemId, `/api/scenes/${encodeURIComponent(sceneId)}/offplan/${place}`, {
    method: "POST", headers: { "Content-Type": "application/json", ...told(notify && decision === "add") }, body: JSON.stringify({ decision }),
  }))
}

// The plan. Before anything is recorded the person and the agent agree what
// the video covers: scenes that say what they will show and what is to be
// said. What needs no judgement the person changes here, and libretto writes
// it into the storyboard; what needs drawing or writing is the agent's.

function studioJSON(method: string, body: unknown, notify = false): RequestInit {
  return { method, headers: { "Content-Type": "application/json", ...told(notify) }, body: JSON.stringify(body) }
}

// writeBrief keeps what the person wants the video to cover. Told of it, the
// agent drafts the plan.
export async function writeBrief(baseUrl: string, itemId: string, text: string, notify = false): Promise<StudioProject> {
  return projectFrom(await studioCall(baseUrl, itemId, "/api/brief", studioJSON("POST", { text }, notify)))
}

// rewriteScene changes what a scene is to say (notes) and what it is to
// show. A scene whose picture is already drawn is not changed by a new line
// about it, so the agent is asked to draw it again; a planned scene, and the
// notes of any scene, need nobody.
export async function rewriteScene(baseUrl: string, itemId: string, scene: StudioScene, change: { notes?: string; shows?: string }, notify = false): Promise<StudioProject> {
  const redraw = change.shows !== undefined && !scene.planned && change.shows.trim() !== (scene.shows ?? "").trim()
  return projectFrom(await studioCall(baseUrl, itemId, `/api/scenes/${encodeURIComponent(scene.id)}`, studioJSON("PATCH", change, notify && redraw)))
}

// moveScene takes a scene one place up (-1) or down (1) the plan. Past the
// edge of its chapter it joins the next one.
export async function moveScene(baseUrl: string, itemId: string, sceneId: string, by: 1 | -1): Promise<StudioProject> {
  return projectFrom(await studioCall(baseUrl, itemId, `/api/scenes/${encodeURIComponent(sceneId)}/move`, studioJSON("POST", { by })))
}

// renameChapter gives a chapter another title.
export async function renameChapter(baseUrl: string, itemId: string, from: string, to: string): Promise<StudioProject> {
  return projectFrom(await studioCall(baseUrl, itemId, "/api/chapters", studioJSON("POST", { from, to })))
}

// addScene puts a scene in the plan from one line about it, after the scene
// named. It is there at once; the agent drafts what is said in it.
export async function addScene(baseUrl: string, itemId: string, shows: string, after?: string, notify = false): Promise<StudioProject> {
  return projectFrom(await studioCall(baseUrl, itemId, "/api/scenes", studioJSON("POST", { shows, after: after ?? "" }, notify)))
}

// agreePlan says the plan is settled: the agent draws the pictures.
export async function agreePlan(baseUrl: string, itemId: string, notify = false): Promise<StudioProject> {
  return projectFrom(await studioCall(baseUrl, itemId, "/api/agree", { method: "POST", headers: told(notify) }))
}

// startRender has libretto render the video from the storyboard as it is.
export async function startRender(baseUrl: string, itemId: string): Promise<StudioProject> {
  return projectFrom(await studioCall(baseUrl, itemId, "/api/render", { method: "POST" }))
}

// takeBackComment removes a comment of the person's, by its place among the
// scene's comments, counted from 1.
export async function takeBackComment(baseUrl: string, itemId: string, sceneId: string, place: number): Promise<StudioProject> {
  return projectFrom(await studioCall(baseUrl, itemId, `/api/scenes/${encodeURIComponent(sceneId)}/comments/${place}`, { method: "DELETE" }))
}

// removeScene takes a scene out of the storyboard. Its takes are kept, and
// the storyboard is left a comment that tells the agent.
export async function removeScene(baseUrl: string, itemId: string, sceneId: string, notify = false): Promise<StudioProject> {
  return projectFrom(await studioCall(baseUrl, itemId, `/api/scenes/${encodeURIComponent(sceneId)}`, { method: "DELETE", headers: told(notify) }))
}

// chooseTake makes an earlier recording the scene's take again.
export async function chooseTake(baseUrl: string, itemId: string, sceneId: string, src: string): Promise<StudioProject> {
  return projectFrom(await studioCall(baseUrl, itemId, `/api/scenes/${encodeURIComponent(sceneId)}/take`, {
    method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ src }),
  }))
}

// fetchStudioFile reads a picture or a recording from the studio. The route
// asks for Legato's token, which an <img> or <audio> element cannot send,
// so the file is fetched and handed over as an address of this page's own.
export async function fetchStudioFile(baseUrl: string, itemId: string, path: string): Promise<string> {
  const res = await studioCall(baseUrl, itemId, path)
  return URL.createObjectURL(await res.blob())
}

// recordedCount is how many of the scenes that are the person's to say have
// been said.
export function recordedCount(project: StudioProject): { recorded: number; toRecord: number } {
  const theirs = project.scenes.filter((scene) => scene.status === "recorded" || scene.status === "waiting")
  return { recorded: theirs.filter((scene) => scene.status === "recorded").length, toRecord: theirs.length }
}

// leftTheScript reports a take that says something other than the narration
// written for it. That is not an error: the person said what they thought
// worth saying, and the storyboard is what should follow.
export function leftTheScript(scene: StudioScene): boolean {
  const match = scene.match
  return Boolean(match && match.total > 0 && match.found < 0.6 * match.total)
}

// chapterHeadings gives, for each scene in order, the chapter it starts, or
// "" when it carries on the one before: what the plan is headed by.
export function chapterHeadings(scenes: StudioScene[]): string[] {
  const headings: string[] = []
  let chapter = ""
  for (const scene of scenes) {
    headings.push(scene.chapter && scene.chapter !== chapter ? scene.chapter : "")
    if (scene.chapter) chapter = scene.chapter
  }
  return headings
}

// talkingPoints reads a scene's notes as the points the person speaks to. The
// agent writes them a line each; they are reminders of what to get across,
// not words to read out, and the panel draws them as a list.
export function talkingPoints(notes: string | undefined): string[] {
  return (notes ?? "").split("\n")
    .map((line) => line.trim().replace(/^(?:[-*•]|\d+[.)])\s+/, "").trim())
    .filter((line) => line !== "")
}

// focusedScene is the scene the agent last put in front of the person, read
// from a studio item's content; null when it named none.
export function focusedScene(content: string | undefined): string | null {
  try {
    const parsed = JSON.parse(content ?? "") as { scene?: unknown }
    return typeof parsed.scene === "string" && parsed.scene !== "" ? parsed.scene : null
  } catch {
    return null
  }
}

// sceneSignature is what a scene is planned to be: its picture and what is
// to be said over it. Whether it has been recorded is not part of the plan.
export function sceneSignature(scene: StudioScene): string {
  return JSON.stringify([scene.kind, scene.chapter ?? "", scene.notes ?? "", scene.still, scene.shows ?? ""])
}

function listed(ids: string[]): string {
  if (ids.length <= 1) return ids.join("")
  return `${ids.slice(0, -1).join(", ")} and ${ids[ids.length - 1]}`
}

// planChanges compares the plan the person last saw (each scene's signature
// by id) with the plan as it is now, so the panel can show what the agent
// just did: which scenes to mark, which are gone, and the change in a
// sentence. The summary is empty when the plan is the same.
export function planChanges(before: Map<string, string>, scenes: StudioScene[]): { marks: Record<string, "new" | "changed">; removed: string[]; summary: string } {
  const marks: Record<string, "new" | "changed"> = {}
  const now = new Set(scenes.map((scene) => scene.id))
  for (const scene of scenes) {
    const was = before.get(scene.id)
    if (was === undefined) marks[scene.id] = "new"
    else if (was !== sceneSignature(scene)) marks[scene.id] = "changed"
  }
  const removed = [...before.keys()].filter((id) => !now.has(id))
  const added = Object.keys(marks).filter((id) => marks[id] === "new")
  const changed = Object.keys(marks).filter((id) => marks[id] === "changed")
  const parts = [
    added.length > 0 ? `added ${listed(added)}` : "",
    removed.length > 0 ? `removed ${listed(removed)}` : "",
    changed.length > 0 ? `changed ${listed(changed)}` : "",
  ].filter((part) => part !== "")
  return { marks, removed, summary: parts.length > 0 ? parts.join("; ") + "." : "" }
}

const KIND: Record<string, string> = {
  card: "a title card", code: "code", image: "an image", clip: "a recording", terminal: "a terminal",
}

// questionAboutScene is what goes in the agent's message box when the person
// wants to talk about a scene: which scene, what it is, and what it is to
// say. They add what they think and send it.
export function questionAboutScene(title: string, project: StudioProject, scene: StudioScene): string {
  const lines = [`About the scene "${scene.id}" in the studio "${title}" (storyboard ${project.storyboard}):`, ""]
  lines.push(`It shows ${KIND[scene.kind] ?? scene.kind}${scene.chapter ? `, and starts the chapter "${scene.chapter}"` : ""}.`)
  const points = talkingPoints(scene.notes)
  if (points.length > 0) lines.push(`Talking points: ${points.join("; ")}`)
  if (scene.status === "recorded") lines.push(`Recorded. I said: ${scene.heard || scene.says || "(no transcript)"}`)
  else if (scene.says) lines.push(`Script: ${scene.says}`)
  else lines.push("Not recorded yet.")
  lines.push("", "What I think: ")
  return lines.join("\n")
}

// lineUpRequest asks the agent to edit the storyboard to follow what the
// person said in their takes. It is written in the person's voice: it goes
// in their message box, for them to send.
export function lineUpRequest(title: string, project: StudioProject): string {
  const recorded = project.scenes.filter((scene) => scene.status === "recorded")
  const waiting = project.scenes.filter((scene) => scene.status === "waiting")
  const wandered = recorded.filter(leftTheScript)
  const lines = [
    `I have recorded "${title}" (storyboard ${project.storyboard}). Line it up with what I said.`, "",
    `Recorded: ${recorded.map((scene) => `${scene.id}${scene.take ? ` (${scene.take.src})` : ""}`).join(", ") || "nothing yet"}.`,
  ]
  if (waiting.length > 0) lines.push(`Not recorded yet: ${waiting.map((scene) => scene.id).join(", ")}.`)
  if (wandered.length > 0) lines.push(`I went my own way in: ${wandered.map((scene) => scene.id).join(", ")}. That was on purpose.`)
  lines.push(
    "",
    "Read each take with libretto transcribe, then edit the storyboard to follow what I said, not the plan: add a picture for anything I went into (a terminal for commands I named, a diagram, the code), drop what I skipped, and put a marker before each word where something should happen. Tell me what you changed, then render it and show me the video.",
  )
  return lines.join("\n")
}

// clock is a length of time as a person reads one: 4:38.
export function clock(ms: number): string {
  const seconds = Math.max(0, Math.floor(ms / 1000))
  return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, "0")}`
}

// A block of the timeline: one scene, as wide as it is long.
export interface TimelineBlock {
  id: string
  // How much of the strip the scene takes, against the others.
  weight: number
  status: StudioScene["status"]
  // True when the person has something to decide about its take.
  decide: boolean
}

// plannedMs is a guess at how long a scene will run before it is said: a
// breath, and a few sentences for each thing it is to say. A person covers a
// talking point in ten seconds or so.
export function plannedMs(scene: StudioScene): number {
  return 4000 + 10000 * Math.max(1, talkingPoints(scene.notes).length)
}

// lengthOf is how long a scene runs, or is likely to: sure is false for a
// guess.
export function lengthOf(scene: StudioScene): { ms: number; sure: boolean } {
  return scene.durationMs ? { ms: scene.durationMs, sure: true } : { ms: plannedMs(scene), sure: false }
}

// planLength is how long the whole video is likely to be.
export function planLength(project: StudioProject): { ms: number; sure: boolean } {
  let ms = 0
  let sure = true
  for (const scene of project.scenes) {
    const length = lengthOf(scene)
    ms += length.ms
    sure = sure && length.sure
  }
  return { ms, sure }
}

// timeline lays the scenes out as blocks, each as wide as the scene is long.
export function timeline(project: StudioProject): TimelineBlock[] {
  return project.scenes.map((scene) => ({
    id: scene.id,
    weight: Math.max(2000, lengthOf(scene).ms),
    status: scene.status,
    decide: openOffPlan(scene).length > 0,
  }))
}

// openOffPlan is the stretches of a scene's take the person has yet to
// decide about, each with its place in the review, counted from 1.
export function openOffPlan(scene: StudioScene): { place: number; part: StudioOffPlan }[] {
  return (scene.review?.offPlan ?? []).map((part, n) => ({ place: n + 1, part })).filter(({ part }) => !part.decision)
}

// pointsHit is which of a scene's talking points its take covers, counted
// from 1, as the agent marked them.
export function pointsHit(scene: StudioScene): number[] {
  const count = talkingPoints(scene.notes).length
  const hit = new Set<number>()
  for (const span of scene.review?.points ?? []) if (span.point >= 1 && span.point <= count) hit.add(span.point)
  return [...hit].sort((a, b) => a - b)
}

// intoTake is when in the scene's own sound a moment of the recording is
// heard: its time from where the scene's part starts, less what was left
// out before it.
export function intoTake(take: StudioTake | undefined, fileMs: number): number {
  let ms = fileMs - (take?.startMs ?? 0)
  for (const [from, to] of take?.skip ?? []) if (fileMs > from) ms -= Math.min(fileMs, to) - from
  return Math.max(0, ms)
}

// A run of a transcript: words drawn alike, one after another.
export interface TranscriptRun {
  text: string
  // The recording's times of the run's first and last word.
  startMs: number
  endMs: number
  // Set on the run a sentence starts with: when it is said, from the start
  // of the scene's part of the take.
  at?: number
  // The talking point the run covers, counted from 1.
  point?: number
  // The stretch off the plan the run is in, by its place in the review,
  // while the person has yet to decide about it.
  offPlan?: number
}

// A pause this long starts a new sentence, whatever the punctuation says.
const PAUSE_MS = 700

// transcriptRuns is a scene's transcript as it is drawn: the words in order,
// joined into runs that share a mark, with the time at each sentence's
// start. A word is in a stretch when its middle is.
export function transcriptRuns(scene: StudioScene): TranscriptRun[] {
  const points = scene.review?.points ?? []
  const open = openOffPlan(scene)
  const runs: TranscriptRun[] = []
  let before: StudioWord | null = null
  for (const word of scene.transcript ?? []) {
    const middle = (word.startMs + word.endMs) / 2
    const point = points.find((span) => middle >= span.from && middle < span.to)?.point
    const offPlan = open.find(({ part }) => middle >= part.from && middle < part.to)?.place
    const starts = !before || /[.?!]$/.test(before.text) || word.startMs - before.endMs >= PAUSE_MS
    const last = runs[runs.length - 1]
    if (last && !starts && last.point === point && last.offPlan === offPlan) {
      last.text += " " + word.text
      last.endMs = word.endMs
    } else {
      runs.push({ text: word.text, startMs: word.startMs, endMs: word.endMs, point, offPlan, ...(starts ? { at: intoTake(scene.take, word.startMs) } : {}) })
    }
    before = word
  }
  return runs
}

// reviewRequest is what the person asks the agent when a take has not been
// looked at: the same thing Legato asks by itself when it is allowed to.
export function reviewRequest(title: string, project: StudioProject, scene: StudioScene): string {
  return [
    `Review my take of the scene "${scene.id}" in "${title}" (storyboard ${project.storyboard}${scene.take ? `, ${scene.take.src}` : ""}).`,
    "",
    "Read it with libretto transcribe, mark where I covered each talking point and where I left the plan with libretto review, and bring the scene's narration, markers and cuts in line with what I said.",
  ].join("\n")
}
