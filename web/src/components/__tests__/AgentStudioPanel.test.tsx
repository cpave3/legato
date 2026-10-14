import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { AgentStudioPanel } from "../AgentStudioPanel"
import { AgentUIItemView } from "../AgentUIItemView"
import { onComposerProposal, type AgentUIItem } from "../../lib/agentUi"
import { studioPace, type StudioProject, type StudioScene } from "../../lib/agentStudio"

vi.mock("../../hooks/useServer", () => ({
  useServer: () => ({ baseUrl: "http://legato.test:3080" }),
}))

const STUDIO = "http://legato.test:3080/api/ui-items/ui-0123456789/studio"

function item(overrides: Partial<AgentUIItem> = {}, scene?: string): AgentUIItem {
  return {
    id: "ui-0123456789", task_id: "task-1", placement: "panel", format: "studio", title: "Restack demo",
    revision: 1, oldest_revision: 1, content: JSON.stringify({ storyboard: "/talks/restack/storyboard.json", scene }),
    created_at: "2026-10-13 01:00:00", updated_at: "2026-10-13 01:00:00", ...overrides,
  }
}

// The panel has two views. The outline, the plan's list of scenes, is in the
// Plan view; the strip of scenes is in the Record view.
const openView = (name: "Plan" | "Record") => {
  const tab = screen.getByRole("tab", { name: new RegExp(name) })
  if (tab.getAttribute("aria-selected") !== "true") fireEvent.click(tab)
}
const outline = () => { openView("Plan"); return within(screen.getByRole("navigation", { name: "Outline" })) }
const strip = () => { openView("Record"); return within(screen.getByRole("navigation", { name: "Timeline" })) }

function scene(overrides: Partial<StudioScene> = {}): StudioScene {
  return { id: "intro", kind: "card", status: "waiting", still: "/api/scenes/intro/still.png?v=abc", takes: [], ...overrides }
}

function project(scenes: StudioScene[], overrides: Partial<StudioProject> = {}): StudioProject {
  return {
    api: 5, title: "Restack demo", storyboard: "/talks/restack/storyboard.json", face: "corner",
    problems: [], warnings: [], scenes, render: { running: false, log: [] }, ...overrides,
  }
}

const planned = () => project([
  scene({ id: "intro", status: "recorded", says: "Hello, this is restack.", heard: "Hello, this is restack.", take: { src: "takes/intro-01.webm", url: "/media/takes/intro-01.webm", current: true }, takes: [{ src: "takes/intro-01.webm", url: "/media/takes/intro-01.webm", current: true }] }),
  scene({ id: "steps", kind: "terminal", chapter: "How it goes", notes: "- Run restack and show the log\n- One command, nothing by hand", still: "/api/scenes/steps/still.png?v=def" }),
])

// What the studio is asked for, and what it answers. A test changes
// `studio.project` to change the storyboard, and `studio.fail` to take the
// studio away.
// `studio.answer` is what a request that changes something leaves the
// storyboard as.
let studio: {
  project: StudioProject
  fail: { status: number; body: unknown } | null
  calls: { method: string; address: string; body: unknown; headers?: unknown }[]
  answer?: (method: string, address: string, body: unknown) => StudioProject | undefined
}

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })
}

// A microphone and a camera that are not there.
function fakeStream(labels: { audio: string; video?: string }) {
  const audio = [{ kind: "audio", label: labels.audio, stop: vi.fn() }]
  const video = labels.video ? [{ kind: "video", label: labels.video, stop: vi.fn() }] : []
  return { active: true, getTracks: () => [...audio, ...video], getAudioTracks: () => audio, getVideoTracks: () => video }
}

// The sound of a take is taken as samples, on the browser's audio thread.
// SOUND_RATE of them make a second here. `microphone` is what the thread
// hands over for a take that lasted so many milliseconds; unless a test says
// otherwise that is sound for the whole of it.
const SOUND_RATE = 1000
const sound = (ms: number, value = 0.5) => new Float32Array((ms * SOUND_RATE) / 1000).fill(value)
let microphone: (ms: number) => Float32Array[]
// What the microphone gives before a take starts: something, unless a test
// makes it nothing.
let listening = 0.1

class FakeSoundNode {
  static made: FakeSoundNode[] = []
  began = 0
  port = {
    onmessage: null as ((event: { data: unknown }) => void) | null,
    postMessage: (message: string) => {
      if (message === "start") this.began = now
      if (message !== "stop") return
      for (const samples of microphone(now - this.began)) this.port.onmessage?.({ data: samples })
      this.port.onmessage?.({ data: "done" })
    },
  }
  constructor() { FakeSoundNode.made.push(this) }
  // give hands the page samples while the take is still being recorded.
  give(samples: Float32Array) { this.port.onmessage?.({ data: samples }) }
  connect() {}
  disconnect() {}
}

class FakeAudioContext {
  state = "running"
  sampleRate = SOUND_RATE
  destination = {}
  audioWorklet = { addModule: async () => {} }
  resume = async () => {}
  close = async () => {}
  createMediaStreamSource() { return { connect() {}, disconnect() {} } }
  createAnalyser() {
    return { fftSize: 0, connect() {}, getByteTimeDomainData() {}, getFloatTimeDomainData(into: Float32Array) { into.fill(listening) } }
  }
}

// What the browser's recorder hands over: enough bytes to be a recording.
// `filmed` is the camera's; `recorded` is the sound's, in a browser that
// cannot give samples.
let recorded = "a take ".repeat(1000)
let filmed = "a picture ".repeat(3000)
let stops = 0

class FakeRecorder {
  static isTypeSupported = () => true
  state = "inactive"
  mimeType: string
  film: boolean
  ondataavailable: ((event: { data: Blob }) => void) | null = null
  onstop: (() => void) | null = null
  constructor(stream: { getVideoTracks(): unknown[] }, options: { mimeType?: string }) {
    this.mimeType = options.mimeType ?? "video/webm"
    this.film = stream.getVideoTracks().length > 0
  }
  start() { this.state = "recording" }
  stop() {
    stops++
    this.state = "inactive"
    this.ondataavailable?.({ data: new Blob([this.film ? filmed : recorded], { type: this.mimeType }) })
    this.onstop?.()
  }
}

// A stream made of some tracks of another, as each recorder is given.
class FakeTracks {
  active = true
  tracks: { kind: string }[]
  constructor(tracks: { kind: string }[]) { this.tracks = tracks }
  getTracks() { return this.tracks }
  getAudioTracks() { return this.tracks.filter((track) => track.kind === "audio") }
  getVideoTracks() { return this.tracks.filter((track) => track.kind === "video") }
}

let getUserMedia: ReturnType<typeof vi.fn>
let now = 0

beforeEach(() => {
  studio = { project: planned(), fail: null, calls: [] }
  recorded = "a take ".repeat(1000)
  filmed = "a picture ".repeat(3000)
  stops = 0
  microphone = (ms) => [sound(ms)]
  listening = 0.1
  // Most tests do not wait through the count before a take.
  studioPace.countdownMs = 0
  FakeSoundNode.made = []
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    const address = String(input)
    studio.calls.push({ method: init?.method ?? "GET", address, body: init?.body, headers: init?.headers })
    if (studio.fail) return json(studio.fail.status, studio.fail.body)
    if (address.includes("/still.png") || address.includes("/media/") || /\/take(\?|$)/.test(address)) return new Response(new Blob(["bytes"]))
    if (studio.answer && (init?.method ?? "GET") !== "GET") studio.project = studio.answer(init?.method ?? "", address, init?.body) ?? studio.project
    return json(200, studio.project)
  })
  URL.createObjectURL = vi.fn(() => "blob:made-here")
  URL.revokeObjectURL = vi.fn()
  getUserMedia = vi.fn().mockResolvedValue(fakeStream({ audio: "Desk microphone", video: "Laptop camera" }))
  Object.defineProperty(navigator, "mediaDevices", { configurable: true, value: { getUserMedia, enumerateDevices: vi.fn().mockResolvedValue([]) } })
  vi.stubGlobal("MediaRecorder", FakeRecorder)
  vi.stubGlobal("MediaStream", FakeTracks)
  vi.stubGlobal("AudioContext", FakeAudioContext)
  vi.stubGlobal("AudioWorkletNode", FakeSoundNode)
  vi.spyOn(HTMLMediaElement.prototype, "play").mockResolvedValue(undefined)
  now = 1_000_000
  vi.spyOn(Date, "now").mockImplementation(() => now)
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe("AgentStudioPanel", () => {
  it("shows the storyboard's scenes and opens on the first that waits to be said", async () => {
    render(<AgentUIItemView item={item()} fill />)

    // What the person speaks to: the scene's talking points, a line each,
    // under its picture. Points to speak to, not a script to read.
    const points = within(await screen.findByRole("list", { name: "Talking points" })).getAllByRole("listitem")
    expect(points.map((point) => point.textContent)).toEqual(["Run restack and show the log", "One command, nothing by hand"])
    expect(strip().getByRole("button", { name: "Scene 2: steps" }).getAttribute("aria-current")).toBe("true")
    expect(screen.getByText("1 of 2 recorded")).toBeTruthy()
    expect(screen.getByText(/Scene 2 of 2/)).toBeTruthy()
    // The picture is fetched through Legato, under the studio's item.
    await waitFor(() => expect(studio.calls.some((call) => call.address === STUDIO + "/api/scenes/steps/still.png?v=def")).toBe(true))
    expect((await screen.findByAltText("The scene steps") as HTMLImageElement).src).toBe("blob:made-here")
    // With something recorded and nothing left to draw, the panel opens where
    // the recording is done. The plan is a step back, laid out by chapter.
    expect(screen.getByRole("tab", { name: /Record/ }).getAttribute("aria-selected")).toBe("true")
    expect(outline().getByRole("button", { name: /steps/ }).getAttribute("aria-current")).toBe("true")
    expect(outline().getByRole("button", { name: /intro/ }).getAttribute("title")).toBe("Recorded")
    expect(outline().getByText("How it goes")).toBeTruthy()
  })

  it("says what to do when libretto is not installed, and offers nothing that cannot work", async () => {
    studio.fail = { status: 503, body: { code: "libretto_missing", error: "libretto is not installed on this machine, so the studio cannot open. To use it, install libretto and make sure the libretto command is on the PATH of the Legato server." } }
    render(<AgentUIItemView item={item()} fill />)

    const alert = await screen.findByRole("alert")
    expect(alert.textContent).toContain("The studio cannot open on this machine")
    expect(alert.textContent).toContain("install libretto")
    // The storyboard is safe, and the person is told so.
    expect(alert.textContent).toContain("Nothing is lost")
    expect(screen.queryByRole("button", { name: /Record/ })).toBeNull()
    expect(screen.queryByRole("button", { name: /Line up/ })).toBeNull()
  })

  it("says the same of a libretto that is too old", async () => {
    studio.fail = { status: 503, body: { code: "libretto_old", error: "The libretto installed on this machine is too old for the studio: it is version 0.3.0. To use it, update libretto." } }
    render(<AgentUIItemView item={item()} fill />)
    const alert = await screen.findByRole("alert")
    expect(alert.textContent).toContain("too old for the studio")
    expect(alert.textContent).toContain("update libretto")
  })

  it("puts a scene in the message box, for the person to say what they think of the plan", async () => {
    const proposals: string[] = []
    const stop = onComposerProposal("task-1", (text) => proposals.push(text))
    render(<AgentUIItemView item={item()} fill />)

    fireEvent.click(await screen.findByRole("button", { name: /Ask about this scene/ }))
    stop()
    expect(proposals).toHaveLength(1)
    expect(proposals[0]).toContain(`About the scene "steps" in the studio "Restack demo"`)
    expect(proposals[0]).toContain("Talking points: Run restack and show the log; One command, nothing by hand")
    expect(proposals[0].endsWith("What I think: ")).toBe(true)
  })

  it("asks the agent to line the video up with what was said", async () => {
    const proposals: string[] = []
    const stop = onComposerProposal("task-1", (text) => proposals.push(text))
    render(<AgentUIItemView item={item()} fill />)

    fireEvent.click(await screen.findByRole("button", { name: /Make the video from my takes/ }))
    stop()
    expect(proposals[0]).toContain("intro (takes/intro-01.webm)")
    expect(proposals[0]).toContain("Not recorded yet: steps")
    expect(proposals[0]).toContain("follow what I said")
  })

  it("has nothing to line up before anything is recorded", async () => {
    studio.project = project([scene({ notes: "Say hello." })])
    render(<AgentUIItemView item={item()} fill />)
    // Nothing is recorded, so the panel opens on the plan.
    expect((await screen.findByRole("tab", { name: /Plan/ })).getAttribute("aria-selected")).toBe("true")
    openView("Record")
    expect((screen.getByRole("button", { name: /Make the video from my takes/ }) as HTMLButtonElement).disabled).toBe(true)
  })

  it("records a take of the scene and gives it to the studio", async () => {
    render(<AgentUIItemView item={item()} fill />)

    fireEvent.click(await screen.findByRole("button", { name: "Record" }))
    const stopButton = await screen.findByRole("button", { name: "Stop" })
    expect(getUserMedia).toHaveBeenCalledTimes(1)

    // Twelve seconds later the person stops, and the studio now has a take.
    now += 12_000
    studio.project = project([planned().scenes[0], { ...planned().scenes[1], status: "recorded", says: "So we run restack.", heard: "So we run restack.", takes: [{ src: "takes/steps-01.webm", url: "/media/takes/steps-01.webm", current: true }] }])
    fireEvent.click(stopButton)
    // The take runs on for a moment after the click: the sound of the last
    // word is still on its way from the microphone.
    expect(stops).toBe(0)
    expect(screen.getByRole("button", { name: "Finishing…" })).toBeTruthy()

    await screen.findByRole("button", { name: "New take" })
    const sent = studio.calls.find((call) => call.method === "POST")
    expect(sent?.address).toBe(STUDIO + "/api/scenes/steps/takes")
    // With the camera on, two files are sent: the camera's recording, and
    // the sound as the samples the microphone gave, all twelve seconds.
    const form = sent?.body as FormData
    expect((form.get("take") as Blob).size).toBe(filmed.length)
    expect((form.get("sound") as Blob).type).toBe("audio/wav")
    expect((form.get("sound") as Blob).size).toBe(44 + 2 * 12 * SOUND_RATE)
    // The camera is recorded by itself. The sound is not left to a recorder.
    expect(stops).toBe(1)
    // A whole take leaves nothing to say.
    expect(within(screen.getByRole("region", { name: "Scene steps" })).getAllByRole("status").every((line) => line.textContent === "")).toBe(true)
    // The person stays on the scene they just said, and sees what was heard.
    expect(screen.getByText("Transcript")).toBeTruthy()
    expect(screen.getByText("So we run restack.")).toBeTruthy()
    expect(screen.getByText(/2 of 2 recorded/)).toBeTruthy()
    // The scene's takes, with the one in use marked.
    expect(within(screen.getByRole("list", { name: "Takes" })).getByRole("button", { name: "Take 1, in use" })).toBeTruthy()
  })

  it("does not keep a take stopped as soon as it began", async () => {
    render(<AgentUIItemView item={item()} fill />)
    fireEvent.click(await screen.findByRole("button", { name: "Record" }))
    now += 300
    fireEvent.click(await screen.findByRole("button", { name: "Stop" }))

    expect(await screen.findByText(/too short to hold a word/)).toBeTruthy()
    expect(studio.calls.some((call) => call.method === "POST")).toBe(false)
  })

  it("records sound alone when the camera cannot be opened, and says why", async () => {
    // Another program has the camera: the browser allows it, then cannot
    // open it. Sound alone can still be had.
    const busy = Object.assign(new Error("Could not start video source"), { name: "NotReadableError" })
    getUserMedia.mockReset()
    getUserMedia.mockRejectedValueOnce(busy).mockResolvedValueOnce(fakeStream({ audio: "Desk microphone" }))
    render(<AgentUIItemView item={item()} fill />)

    fireEvent.click(await screen.findByRole("button", { name: "Record" }))
    await screen.findByRole("button", { name: "Stop" })
    expect(getUserMedia).toHaveBeenCalledTimes(2)
    expect(getUserMedia.mock.calls[1][0].video).toBe(false)
    expect(screen.getByText(/The camera could not be opened \(another program may be using it\), so this take is sound alone/)).toBeTruthy()
  })

  it("says so when there is no microphone to record", async () => {
    getUserMedia.mockReset()
    getUserMedia.mockRejectedValue(Object.assign(new Error("denied"), { name: "NotAllowedError" }))
    render(<AgentUIItemView item={item()} fill />)

    fireEvent.click(await screen.findByRole("button", { name: "Record" }))
    expect(await screen.findByText(/The microphone could not be opened: the browser or the system did not allow it/)).toBeTruthy()
    expect(screen.getByRole("button", { name: "Record" })).toBeTruthy()
  })

  it("treats a take that left the script as direction, not as a mistake", async () => {
    studio.project = project([scene({
      id: "steps", status: "recorded", says: "First we make a branch, then we restack it.", heard: "Actually let me show you sync first.",
      match: { found: 2, total: 9 }, take: { src: "takes/steps-01.webm", url: "/media/takes/steps-01.webm", current: true },
    })])
    render(<AgentUIItemView item={item()} fill />)

    const told = await screen.findByText(/You went your own way here/)
    expect(told.textContent).toContain("That is fine")
    expect(screen.getByText("Actually let me show you sync first.")).toBeTruthy()
    // Nothing on the panel calls it a problem.
    expect(screen.queryByRole("alert")).toBeNull()
    expect(screen.queryByText(/cannot be rendered/)).toBeNull()
  })

  it("shows what stops the storyboard from rendering", async () => {
    studio.project = project([scene({ notes: "Say hello." })], { problems: [`scene "intro": card.title is empty`] })
    render(<AgentUIItemView item={item()} fill />)
    expect(await screen.findByText(`scene "intro": card.title is empty`)).toBeTruthy()
    expect(screen.getByText("The storyboard cannot be rendered yet")).toBeTruthy()
  })

  it("keeps the microphone and the camera out of the way, and remembers which were chosen", async () => {
    const stored: Record<string, string> = {}
    vi.spyOn(Storage.prototype, "setItem").mockImplementation((key, value) => { stored[key] = value })
    vi.spyOn(Storage.prototype, "getItem").mockImplementation((key) => stored[key] ?? null)
    Object.defineProperty(navigator, "mediaDevices", { configurable: true, value: { getUserMedia, enumerateDevices: vi.fn().mockResolvedValue([
      { kind: "audioinput", deviceId: "mic-desk", label: "Desk microphone" }, { kind: "audioinput", deviceId: "mic-headset", label: "Headset" },
      { kind: "videoinput", deviceId: "cam-laptop", label: "Laptop camera" },
    ]) } })
    const { unmount } = render(<AgentUIItemView item={item()} fill />)

    // Set once and forgotten: nothing about devices is on the panel until
    // the person asks for it.
    await screen.findByRole("button", { name: "Record" })
    expect(screen.queryByRole("combobox")).toBeNull()
    fireEvent.click(screen.getByRole("button", { name: "Microphone and camera" }))
    const devices = within(screen.getByRole("group", { name: "Microphone and camera" }))
    fireEvent.change(devices.getByLabelText("Microphone"), { target: { value: "mic-headset" } })
    fireEvent.change(devices.getByLabelText("Camera"), { target: { value: "none" } })
    expect(stored).toEqual({ "legato:studio-mic": "mic-headset", "legato:studio-cam": "none" })

    // The next visit records with them, without being told again.
    unmount()
    getUserMedia.mockResolvedValue(fakeStream({ audio: "Headset" }))
    render(<AgentUIItemView item={item()} fill />)
    fireEvent.click(await screen.findByRole("button", { name: "Record" }))
    await screen.findByRole("button", { name: "Stop" })
    expect(getUserMedia.mock.calls[0][0]).toEqual({ audio: { echoCancellation: false, deviceId: { exact: "mic-headset" } }, video: false })
  })

  it("shows the plan change as the agent changes it", async () => {
    render(<AgentStudioPanel item={item()} fill pollMs={15} />)
    await screen.findByRole("list", { name: "Talking points" })
    expect(screen.queryByText(/The plan changed/)).toBeNull()

    // "Add a part on sync, drop the intro, and go deeper on the steps."
    studio.project = project([
      { ...planned().scenes[1], notes: "- Run restack\n- Show the log before and after\n- Say what happens on a conflict" },
      scene({ id: "sync", kind: "terminal", chapter: "Keeping up", notes: "- Pull main and restack in one go", still: "/api/scenes/sync/still.png?v=new" }),
    ])

    expect(await screen.findByText(/The plan changed: added sync; removed intro; changed steps\./)).toBeTruthy()
    // The scene in front of the person is redrawn with its new points.
    expect(within(screen.getByRole("list", { name: "Talking points" })).getAllByRole("listitem")).toHaveLength(3)
    expect(outline().queryByRole("button", { name: /intro/ })).toBeNull()
    expect(outline().getByRole("button", { name: /sync/ }).textContent).toContain("new")
    // And it is not marked: they are looking at it.
    expect(outline().getByRole("button", { name: /steps/ }).textContent).not.toContain("changed")

    // A scene the person goes to is no longer news.
    fireEvent.click(outline().getByRole("button", { name: /sync/ }))
    expect(outline().getByRole("button", { name: /sync/ }).textContent).not.toContain("new")
    expect(await screen.findByDisplayValue("Pull main and restack in one go")).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Dismiss" }))
    expect(screen.queryByText(/The plan changed/)).toBeNull()
  })

  it("turns to the scene the agent puts in front of the person", async () => {
    const { rerender } = render(<AgentStudioPanel item={item({}, "intro")} fill />)
    await screen.findByRole("navigation", { name: "Timeline" })
    await waitFor(() => expect(strip().getByRole("button", { name: "Scene 1: intro" }).getAttribute("aria-current")).toBe("true"))

    // The conversation moves on, and the agent shows the studio again.
    rerender(<AgentStudioPanel item={item({ revision: 2 }, "steps")} fill />)
    await waitFor(() => expect(strip().getByRole("button", { name: "Scene 2: steps" }).getAttribute("aria-current")).toBe("true"))
    expect(screen.getByRole("list", { name: "Talking points" })).toBeTruthy()
  })

  it("steps from one scene to the next", async () => {
    render(<AgentUIItemView item={item()} fill />)
    await screen.findByText(/Scene 2 of 2/)
    expect((screen.getByRole("button", { name: "Next scene" }) as HTMLButtonElement).disabled).toBe(true)

    fireEvent.click(screen.getByRole("button", { name: "Previous scene" }))
    expect(await screen.findByText(/Scene 1 of 2/)).toBeTruthy()
    expect(screen.getByText("Hello, this is restack.")).toBeTruthy()
  })

  it("lets the microphone go by itself a while after a test", async () => {
    const stream = fakeStream({ audio: "Desk microphone", video: "Laptop camera" })
    getUserMedia.mockReset()
    getUserMedia.mockResolvedValue(stream)
    render(<AgentStudioPanel item={item()} fill testHoldMs={50} />)
    fireEvent.click(await screen.findByRole("button", { name: "Microphone and camera" }))
    fireEvent.click(screen.getByRole("button", { name: "Test" }))
    expect(await screen.findByText("Using Desk microphone and Laptop camera")).toBeTruthy()
    expect(stream.getTracks().some((track) => track.stop.mock.calls.length > 0)).toBe(false)

    // A browser gives every page the same feed from a microphone. A page
    // that keeps one open that it is not using spoils the takes of every
    // other, so a test does not last.
    await waitFor(() => expect(stream.getTracks().every((track) => track.stop.mock.calls.length > 0)).toBe(true))
    expect(screen.queryByText("Using Desk microphone and Laptop camera")).toBeNull()
  })

  it("opens the microphone afresh for each take, and lets it go between them", async () => {
    const first = fakeStream({ audio: "Desk microphone", video: "Laptop camera" })
    const second = fakeStream({ audio: "Desk microphone", video: "Laptop camera" })
    getUserMedia.mockReset()
    getUserMedia.mockResolvedValueOnce(first).mockResolvedValueOnce(second)
    render(<AgentUIItemView item={item()} fill />)

    fireEvent.click(await screen.findByRole("button", { name: "Record" }))
    const stopButton = await screen.findByRole("button", { name: "Stop" })
    now += 5_000
    fireEvent.click(stopButton)
    await waitFor(() => expect(studio.calls.filter((call) => call.method === "POST")).toHaveLength(1))
    // A microphone left open between takes hands the next take sound from
    // before it began, and loses the end of what is said. Each take gets
    // its own.
    await waitFor(() => expect(first.getTracks().every((track) => track.stop.mock.calls.length > 0)).toBe(true))

    fireEvent.click(await screen.findByRole("button", { name: /^(Record|New take)$/ }))
    await screen.findByRole("button", { name: "Stop" })
    expect(getUserMedia).toHaveBeenCalledTimes(2)
  })

  it("keeps nothing of a take in which the microphone gave no sound", async () => {
    // A microphone that is muted gives samples of exactly nothing.
    microphone = (ms) => [sound(ms, 0)]
    render(<AgentUIItemView item={item()} fill />)
    fireEvent.click(await screen.findByRole("button", { name: "Record" }))
    const stopButton = await screen.findByRole("button", { name: "Stop" })
    now += 8_000
    fireEvent.click(stopButton)

    expect(await screen.findByText(/Nothing was recorded: the microphone gave no sound/)).toBeTruthy()
    expect(studio.calls.some((call) => call.method === "POST")).toBe(false)
  })

  it("says when the microphone stopped for a stretch of the take", async () => {
    // Five seconds of sound, six of exactly nothing, and one more of sound.
    microphone = () => [sound(5000), sound(6000, 0), sound(1000)]
    render(<AgentUIItemView item={item()} fill />)
    fireEvent.click(await screen.findByRole("button", { name: "Record" }))
    const stopButton = await screen.findByRole("button", { name: "Stop" })
    now += 12_000
    fireEvent.click(stopButton)

    expect(await screen.findByText(/The microphone gave nothing at all for 6\.0 s of this 12\.0 s take, from 0:05 into it\. If you were speaking then, another page or program may have had the microphone: close it, and record the take again/)).toBeTruthy()
    // The take is kept all the same: the person may have been pausing.
    expect(studio.calls.filter((call) => call.method === "POST")).toHaveLength(1)
  })

  it("says when less sound came than the take lasted", async () => {
    microphone = () => [sound(4000)]
    render(<AgentUIItemView item={item()} fill />)
    fireEvent.click(await screen.findByRole("button", { name: "Record" }))
    const stopButton = await screen.findByRole("button", { name: "Stop" })
    now += 12_000
    fireEvent.click(stopButton)

    expect(await screen.findByText(/Only 4\.0 s of sound came from a take of 12\.0 s/)).toBeTruthy()
  })

  it("says how much of a take the microphone gave nothing for, when it came and went", async () => {
    // Sound in short bursts, as a microphone gives that another page has.
    microphone = () => [sound(2500, 0), sound(400), sound(11000, 0), sound(400), sound(9000, 0), sound(400), sound(6300, 0)]
    render(<AgentUIItemView item={item()} fill />)
    fireEvent.click(await screen.findByRole("button", { name: "Record" }))
    const stopButton = await screen.findByRole("button", { name: "Stop" })
    now += 30_000
    fireEvent.click(stopButton)

    expect(await screen.findByText(/The microphone gave nothing at all for 28\.8 s of this 30\.0 s take, 11\.0 s of it in one stretch from 0:02 into it\./)).toBeTruthy()
  })

  it("warns while the take is being recorded that the microphone has stopped", async () => {
    render(<AgentUIItemView item={item()} fill />)
    fireEvent.click(await screen.findByRole("button", { name: "Record" }))
    await screen.findByRole("button", { name: "Stop" })
    expect(screen.queryByRole("alert")).toBeNull()

    FakeSoundNode.made[0].give(sound(2000))
    FakeSoundNode.made[0].give(sound(4000, 0))
    // Where a person who is presenting will see it, not in the small print.
    const alert = await screen.findByRole("alert")
    expect(alert.textContent).toMatch(/The microphone is giving no sound, and has not for 4 s\. If you are speaking, cancel the take: another page or program may have the microphone/)
    // It was a pause, or the microphone came back.
    FakeSoundNode.made[0].give(sound(500))
    await waitFor(() => expect(screen.queryByRole("alert")).toBeNull())
  })

  it("records the sound with the browser's recorder when it cannot be taken as samples, and says so", async () => {
    vi.stubGlobal("AudioContext", undefined)
    render(<AgentUIItemView item={item()} fill />)
    fireEvent.click(await screen.findByRole("button", { name: "Record" }))
    const stopButton = await screen.findByRole("button", { name: "Stop" })
    now += 12_000
    fireEvent.click(stopButton)

    await waitFor(() => expect(studio.calls.filter((call) => call.method === "POST")).toHaveLength(1))
    const form = studio.calls.find((call) => call.method === "POST")?.body as FormData
    expect((form.get("sound") as Blob).size).toBe(recorded.length)
    expect((form.get("take") as Blob).size).toBe(filmed.length)
    expect(stops).toBe(2)
    expect(await screen.findByText(/This browser could not give the take's sound as samples, so its own recorder was used\. That recorder can lose the end of a take: listen to it/)).toBeTruthy()
  })

  it("says what the studio says when it cannot keep a take", async () => {
    render(<AgentUIItemView item={item()} fill />)
    fireEvent.click(await screen.findByRole("button", { name: "Record" }))
    await screen.findByRole("button", { name: "Stop" })
    now += 8_000
    studio.fail = { status: 422, body: { error: "The recording could not be read: the browser sent 110 bytes with no sound in them, so nothing was kept. Record it again." } }
    fireEvent.click(screen.getByRole("button", { name: "Stop" }))

    expect(await screen.findByText(/The recording could not be read/)).toBeTruthy()
  })

  it("cancels a botched take: nothing is kept, nothing is transcribed, and the scene is ready to try again", async () => {
    const stream = fakeStream({ audio: "Desk microphone", video: "Laptop camera" })
    getUserMedia.mockReset()
    getUserMedia.mockResolvedValue(stream)
    render(<AgentUIItemView item={item()} fill />)

    // No way to cancel what has not started.
    const record = await screen.findByRole("button", { name: "Record" })
    expect(screen.queryByRole("button", { name: "Cancel take" })).toBeNull()
    fireEvent.click(record)
    await screen.findByRole("button", { name: "Stop" })
    now += 20_000
    fireEvent.click(screen.getByRole("button", { name: "Cancel take" }))

    // At once, with no wait for the last word: the take is thrown away.
    expect(await screen.findByText(/That take was thrown away/)).toBeTruthy()
    expect(screen.getByRole("button", { name: "Record" })).toBeTruthy()
    expect(screen.queryByRole("button", { name: "Cancel take" })).toBeNull()
    expect(studio.calls.some((call) => call.method === "POST")).toBe(false)
    expect(stream.getTracks().every((track) => track.stop.mock.calls.length > 0)).toBe(true)
    expect(screen.getByText("1 of 2 recorded")).toBeTruthy()

    // And the next take is an ordinary one.
    fireEvent.click(screen.getByRole("button", { name: "Record" }))
    const stopButton = await screen.findByRole("button", { name: "Stop" })
    now += 6_000
    fireEvent.click(stopButton)
    await waitFor(() => expect(studio.calls.filter((call) => call.method === "POST")).toHaveLength(1))
  })

  it("can still cancel a take that is finishing", async () => {
    render(<AgentUIItemView item={item()} fill />)
    fireEvent.click(await screen.findByRole("button", { name: "Record" }))
    const stopButton = await screen.findByRole("button", { name: "Stop" })
    now += 6_000
    fireEvent.click(stopButton)
    // Stop was pressed, and then the person thought better of the take.
    fireEvent.click(screen.getByRole("button", { name: "Cancel take" }))

    expect(await screen.findByText(/That take was thrown away/)).toBeTruthy()
    expect(studio.calls.some((call) => call.method === "POST")).toBe(false)
  })

  it("keeps the words of a take whose camera gave nothing", async () => {
    // The camera never gave a picture, so the recording of camera and sound
    // is the start of a file and nothing after it.
    filmed = "x".repeat(110)
    render(<AgentUIItemView item={item()} fill />)
    fireEvent.click(await screen.findByRole("button", { name: "Record" }))
    const stopButton = await screen.findByRole("button", { name: "Stop" })
    now += 40_000
    fireEvent.click(stopButton)

    // The sound was taken by itself, and that is sent as the take.
    await waitFor(() => expect(studio.calls.filter((call) => call.method === "POST")).toHaveLength(1))
    const sent = studio.calls.find((call) => call.method === "POST")
    expect(sent?.body).toBeInstanceOf(Blob)
    expect((sent?.body as Blob).type).toBe("audio/wav")
    expect((sent?.body as Blob).size).toBe(44 + 2 * 40 * SOUND_RATE)
  })

  it("records nothing but the sound when no camera is wanted", async () => {
    const stored: Record<string, string> = { "legato:studio-cam": "none" }
    vi.spyOn(Storage.prototype, "getItem").mockImplementation((key) => stored[key] ?? null)
    getUserMedia.mockResolvedValue(fakeStream({ audio: "Desk microphone" }))
    render(<AgentUIItemView item={item()} fill />)
    fireEvent.click(await screen.findByRole("button", { name: "Record" }))
    const stopButton = await screen.findByRole("button", { name: "Stop" })
    now += 9_000
    fireEvent.click(stopButton)

    await waitFor(() => expect(studio.calls.filter((call) => call.method === "POST")).toHaveLength(1))
    expect(stops).toBe(0)
    const sent = studio.calls.find((call) => call.method === "POST")?.body as Blob
    expect(sent).toBeInstanceOf(Blob)
    expect(sent.type).toBe("audio/wav")
  })

  it("leaves the agent a comment on a scene, and takes one back", async () => {
    studio.answer = (method, address, body) => {
      const steps = studio.project.scenes[1]
      if (method === "POST" && address.endsWith("/api/scenes/steps/comments")) {
        return project([studio.project.scenes[0], { ...steps, comments: [...(steps.comments ?? []), JSON.parse(String(body)).text] }])
      }
      if (method === "DELETE" && address.endsWith("/api/scenes/steps/comments/1")) {
        return project([studio.project.scenes[0], { ...steps, comments: (steps.comments ?? []).slice(1) }])
      }
    }
    render(<AgentUIItemView item={item()} fill />)
    const box = await screen.findByRole("textbox", { name: "A comment for the agent on this scene" })
    // Nothing to send yet.
    expect((screen.getByRole("button", { name: "Add comment" }) as HTMLButtonElement).disabled).toBe(true)
    fireEvent.change(box, { target: { value: "Show the real log here, not a made-up one." } })
    fireEvent.click(screen.getByRole("button", { name: "Add comment" }))

    const comments = within(await screen.findByRole("list", { name: "Comments for the agent" }))
    expect(comments.getByText("Show the real log here, not a made-up one.")).toBeTruthy()
    const sent = studio.calls.find((call) => call.method === "POST")
    expect(sent?.address).toBe(STUDIO + "/api/scenes/steps/comments")
    expect(JSON.parse(String(sent?.body))).toEqual({ text: "Show the real log here, not a made-up one." })
    expect((box as HTMLTextAreaElement).value).toBe("")
    // The outline says which scenes have something waiting for the agent.
    expect(outline().getByRole("button", { name: /steps/ }).textContent).toContain("1 comment")

    // The plan has the scene's comments too, and one can be taken back there.
    fireEvent.click(within(screen.getByRole("list", { name: "Comments for the agent" })).getByRole("button", { name: "Take this comment back" }))
    await waitFor(() => expect(screen.queryByRole("list", { name: "Comments for the agent" })).toBeNull())
    expect(studio.calls.some((call) => call.method === "DELETE" && call.address === STUDIO + "/api/scenes/steps/comments/1")).toBe(true)
  })

  it("removes a scene from the plan, once the person is sure", async () => {
    studio.answer = (method, address) => {
      if (method === "DELETE" && address.endsWith("/api/scenes/steps")) {
        return project([studio.project.scenes[0]], { comments: ['The person removed the scene "steps" in the studio.'] })
      }
    }
    render(<AgentUIItemView item={item()} fill />)
    fireEvent.click(await screen.findByRole("button", { name: "Remove this scene" }))
    // Asked first, and nothing is sent until the answer.
    expect(screen.getByText(/Remove steps from the video\? Its takes are kept\./)).toBeTruthy()
    expect(studio.calls.some((call) => call.method === "DELETE")).toBe(false)
    fireEvent.click(screen.getByRole("button", { name: "Keep it" }))
    expect(screen.queryByText(/Remove steps from the video/)).toBeNull()

    fireEvent.click(screen.getByRole("button", { name: "Remove this scene" }))
    fireEvent.click(screen.getByRole("button", { name: "Remove steps" }))
    await waitFor(() => expect(outline().queryByRole("button", { name: /steps/ })).toBeNull())
    expect(studio.calls.some((call) => call.method === "DELETE" && call.address === STUDIO + "/api/scenes/steps")).toBe(true)
    // What the agent has yet to read is shown, so the person knows it was noted.
    expect(within(screen.getByRole("list", { name: "For the agent" })).getByText(/removed the scene "steps"/)).toBeTruthy()
    // Their own removal is not news to them.
    expect(screen.queryByText(/The plan changed/)).toBeNull()
  })

  it("counts down from three before a take starts", async () => {
    studioPace.countdownMs = 450
    render(<AgentUIItemView item={item()} fill />)
    fireEvent.click(await screen.findByRole("button", { name: "Record" }))

    const count = await screen.findByRole("timer", { name: "The take starts in" })
    expect(count.textContent).toBe("3")
    // Nothing is being recorded yet.
    expect(screen.queryByRole("button", { name: "Stop" })).toBeNull()
    expect(FakeSoundNode.made[0]?.began ?? 0).toBe(0)
    await waitFor(() => expect(screen.getByRole("timer", { name: "The take starts in" }).textContent).toBe("1"))

    await screen.findByRole("button", { name: "Stop" })
    expect(screen.queryByRole("timer", { name: "The take starts in" })).toBeNull()
    expect(FakeSoundNode.made[0].began).toBe(now)
  })

  it("does not start a take with a microphone that gives no sound", async () => {
    const stream = fakeStream({ audio: "Desk microphone", video: "Laptop camera" })
    getUserMedia.mockReset()
    getUserMedia.mockResolvedValue(stream)
    // Another page has the microphone: this one is given nothing at all.
    listening = 0
    studioPace.countdownMs = 300
    render(<AgentUIItemView item={item()} fill />)
    fireEvent.click(await screen.findByRole("button", { name: "Record" }))

    expect(await screen.findByText(/The microphone is giving no sound, so the take was not started\. Another page or program may have it: close that, then record again/)).toBeTruthy()
    expect(screen.getByRole("button", { name: "Record" })).toBeTruthy()
    expect(screen.queryByRole("button", { name: "Stop" })).toBeNull()
    expect(stream.getTracks().every((track) => track.stop.mock.calls.length > 0)).toBe(true)
    expect(studio.calls.some((call) => call.method === "POST")).toBe(false)
  })

  it("can be called off while it counts down", async () => {
    studioPace.countdownMs = 600
    render(<AgentUIItemView item={item()} fill />)
    fireEvent.click(await screen.findByRole("button", { name: "Record" }))
    await screen.findByRole("timer", { name: "The take starts in" })
    fireEvent.click(screen.getByRole("button", { name: "Cancel take" }))

    await waitFor(() => expect(screen.queryByRole("timer", { name: "The take starts in" })).toBeNull())
    expect(await screen.findByRole("button", { name: "Record" })).toBeTruthy()
    await new Promise((resolve) => setTimeout(resolve, 700))
    expect(screen.queryByRole("button", { name: "Stop" })).toBeNull()
    expect(studio.calls.some((call) => call.method === "POST")).toBe(false)
  })

  // A scene whose take the agent has looked at: two of its three points
  // covered, and a stretch that left the plan.
  const reviewedScene = () => scene({
    id: "steps", kind: "terminal", chapter: "How it goes", status: "recorded", durationMs: 41000,
    notes: "- Run restack and show the log\n- One command, nothing by hand\n- It is safe to run twice",
    take: { src: "takes/steps-02.webm", url: "/api/scenes/steps/take?v=1", startMs: 1000, endMs: 40000, durationMs: 41000, current: true },
    takes: [
      { src: "takes/steps-01.webm", url: "/media/takes/steps-01.webm", durationMs: 52000 },
      { src: "takes/steps-02.webm", url: "/media/takes/steps-02.webm", durationMs: 41000, current: true },
    ],
    transcript: [
      { text: "So", startMs: 1200, endMs: 1400 }, { text: "we", startMs: 1400, endMs: 1500 }, { text: "run", startMs: 1500, endMs: 1800 }, { text: "restack.", startMs: 1800, endMs: 2400 },
      { text: "Oh,", startMs: 9000, endMs: 9300 }, { text: "and", startMs: 9300, endMs: 9500 }, { text: "sync", startMs: 9500, endMs: 9900 }, { text: "too.", startMs: 9900, endMs: 10400 },
      { text: "One", startMs: 23000, endMs: 23300 }, { text: "command.", startMs: 23300, endMs: 24000 },
    ],
    review: {
      scene: "steps",
      points: [{ point: 1, from: 1200, to: 2400 }, { point: 2, from: 23000, to: 24000 }],
      offPlan: [{ from: 9000, to: 10400, about: "sync", note: "You walked through sync, which has no scene. Add a scene after steps and move that part of this take into it?" }],
    },
  })
  const reviewed = () => project([planned().scenes[0], reviewedScene()], { durationMs: 59000 })

  it("lays the video out as a strip of scenes, and goes to the one that is pressed", async () => {
    studio.project = reviewed()
    render(<AgentStudioPanel item={item({}, "steps")} fill />)
    const strip = within(await screen.findByRole("navigation", { name: "Timeline" }))
    await waitFor(() => expect(strip.getByRole("button", { name: "Scene 2: steps" }).getAttribute("aria-current")).toBe("true"))
    expect(strip.getByRole("button", { name: "Scene 2: steps" }).textContent).toBe("2")
    expect(screen.getByRole("navigation", { name: "Timeline" }).textContent).toContain("2 recorded · ◌ 1 to decide")
    expect(screen.getByRole("navigation", { name: "Timeline" }).textContent).toContain("0:59")
    expect(screen.getByText("Scene 2 of 2 · How it goes · terminal · 0:41")).toBeTruthy()

    fireEvent.click(strip.getByRole("button", { name: "Scene 1: intro" }))
    expect(strip.getByRole("button", { name: "Scene 1: intro" }).getAttribute("aria-current")).toBe("true")
    expect(screen.getByText(/Scene 1 of 2/)).toBeTruthy()
  })

  it("marks in the transcript what covers a talking point and what left the plan", async () => {
    studio.project = reviewed()
    render(<AgentStudioPanel item={item({}, "steps")} fill />)

    expect((await screen.findByTitle("Covers: Run restack and show the log")).textContent).toBe("So we run restack.")
    expect(screen.getByTitle("Covers: One command, nothing by hand").textContent).toBe("One command.")
    expect(screen.getByTitle("Off the plan").textContent).toBe("Oh, and sync too.")
    // Each sentence says when it was said, from the start of the scene's part.
    expect(screen.getByText("0:08")).toBeTruthy()
    expect(screen.getByText("0:22")).toBeTruthy()
    // Two of three were covered, and the list says which.
    expect(screen.getByText("2 / 3")).toBeTruthy()
    const points = within(screen.getByRole("list", { name: "Talking points" })).getAllByRole("listitem")
    expect(points.map((point) => point.title)).toEqual(["Covered in your take", "Covered in your take", "Not covered in your take"])
  })

  it("asks the person what to do with what left the plan, and does what they choose", async () => {
    studio.project = reviewed()
    studio.answer = (method, address, body) => {
      if (method !== "POST" || !address.endsWith("/api/scenes/steps/offplan/1")) return
      const steps = reviewedScene()
      steps.review!.offPlan![0].decision = JSON.parse(String(body)).decision
      return project([planned().scenes[0], steps])
    }
    render(<AgentStudioPanel item={item({}, "steps")} fill />)
    const card = within(await screen.findByRole("group", { name: "Off the plan: sync" }))
    expect(card.getByText("Your take went off-plan")).toBeTruthy()
    expect(card.getByText("0:08 to 0:09")).toBeTruthy()
    expect(card.getByText(/You walked through sync, which has no scene/)).toBeTruthy()
    expect(card.getByRole("button", { name: "Add scene" })).toBeTruthy()
    expect(card.getByRole("button", { name: "Keep in this scene" })).toBeTruthy()

    fireEvent.click(card.getByRole("button", { name: "Cut it" }))
    await waitFor(() => expect(screen.queryByRole("group", { name: "Off the plan: sync" })).toBeNull())
    const sent = studio.calls.find((call) => call.method === "POST")
    expect(sent?.address).toBe(STUDIO + "/api/scenes/steps/offplan/1")
    expect(JSON.parse(String(sent?.body))).toEqual({ decision: "cut" })
    expect(screen.queryByTitle("Off the plan")).toBeNull()
    expect(screen.queryByText(/to decide/)).toBeNull()
  })

  it("says a take has not been looked at yet, and can ask for it", async () => {
    const proposed: string[] = []
    const stopListening = onComposerProposal("task-1", (text) => proposed.push(text))
    render(<AgentStudioPanel item={item({}, "intro")} fill />)
    expect(await screen.findByText("The agent is looking at this take")).toBeTruthy()
    expect(screen.getByText("1 take not reviewed yet")).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Ask for a review" }))
    expect(proposed[0]).toContain('Review my take of the scene "intro"')
    expect(proposed[0]).toContain("takes/intro-01.webm")
    stopListening()
  })

  it("has Legato tell the agent of a take, unless the person turns that off", async () => {
    const stored: Record<string, string> = {}
    vi.spyOn(Storage.prototype, "getItem").mockImplementation((key) => stored[key] ?? null)
    vi.spyOn(Storage.prototype, "setItem").mockImplementation((key, value) => { stored[key] = String(value) })
    const told = () => studio.calls.filter((call) => call.method === "POST").map((call) => new Headers(call.headers as HeadersInit).get("X-Legato-Studio-Notify"))
    const record = async () => {
      fireEvent.click(await screen.findByRole("button", { name: /^(Record|New take)$/ }))
      const stopButton = await screen.findByRole("button", { name: "Stop" })
      now += 5_000
      fireEvent.click(stopButton)
    }
    render(<AgentUIItemView item={item()} fill />)
    await record()
    await waitFor(() => expect(told()).toEqual(["1"]))

    fireEvent.click(await screen.findByRole("button", { name: "Microphone and camera" }))
    fireEvent.click(screen.getByRole("checkbox", { name: /Tell the agent when a take is ready/ }))
    expect(stored["legato:studio-notify"]).toBe("off")
    await record()
    await waitFor(() => expect(told()).toEqual(["1", null]))
  })

  it("plays the part of the take the scene uses", async () => {
    studio.project = reviewed()
    render(<AgentStudioPanel item={item({}, "steps")} fill />)
    fireEvent.click(await screen.findByRole("button", { name: "Play take" }))
    await screen.findByRole("button", { name: "Pause take" })
    expect(studio.calls.some((call) => call.address === STUDIO + "/api/scenes/steps/take?v=1")).toBe(true)
    expect(HTMLMediaElement.prototype.play).toHaveBeenCalled()
    expect(screen.getByText("0:00 / 0:39")).toBeTruthy()
    // An earlier take can be put back in use.
    expect(within(screen.getByRole("list", { name: "Takes" })).getByRole("button", { name: "Use take 1, 0:52" })).toBeTruthy()
    expect(within(screen.getByRole("list", { name: "Takes" })).getByRole("button", { name: "Take 2, 0:41, in use" })).toBeTruthy()
  })

  it("renders the video when every scene is recorded, and plays it", async () => {
    // One scene still waits: nothing to render yet.
    render(<AgentStudioPanel item={item()} fill pollMs={30} />)
    expect((await screen.findByRole("button", { name: "Render" }) as HTMLButtonElement).disabled).toBe(true)
    expect((screen.getByRole("button", { name: "Preview video" }) as HTMLButtonElement).disabled).toBe(true)

    studio.project = reviewed()
    studio.answer = (method, address) => {
      if (method === "POST" && address.endsWith("/api/render")) return project(reviewed().scenes, { render: { running: true, log: ["scene steps  41.0s"] } })
    }
    await waitFor(() => expect((screen.getByRole("button", { name: "Render" }) as HTMLButtonElement).disabled).toBe(false))
    fireEvent.click(screen.getByRole("button", { name: "Render" }))
    await waitFor(() => expect(screen.getByRole("status", { name: "The video" }).textContent).toBe("Rendering… scene steps  41.0s"))
    expect(studio.calls.some((call) => call.method === "POST" && call.address === STUDIO + "/api/render")).toBe(true)

    studio.project = project(reviewed().scenes, { render: { running: false, log: [], video: "/media/video?v=9" } })
    await waitFor(() => expect((screen.getByRole("button", { name: "Preview video" }) as HTMLButtonElement).disabled).toBe(false))
    fireEvent.click(screen.getByRole("button", { name: "Preview video" }))
    expect(await screen.findByLabelText("The rendered video")).toBeTruthy()
    expect(studio.calls.some((call) => call.address === STUDIO + "/media/video?v=9")).toBe(true)
    fireEvent.click(screen.getByRole("button", { name: "Back to the scene" }))
    expect(screen.queryByLabelText("The rendered video")).toBeNull()
  })

  it("says where the rendered video is, and copies its path", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true })
    // A libretto from before it said where the file is: nothing to show.
    studio.project = project(reviewed().scenes, { render: { running: false, log: [], video: "/media/video?v=9" } })
    render(<AgentStudioPanel item={item()} fill pollMs={30} />)
    await waitFor(() => expect((screen.getByRole("button", { name: "Preview video" }) as HTMLButtonElement).disabled).toBe(false))
    expect(screen.queryByRole("button", { name: "Copy path" })).toBeNull()

    studio.project = project(reviewed().scenes, { render: { running: false, log: [], video: "/media/video?v=9", file: "/home/me/talk/storyboard.mp4" } })
    const file = await screen.findByLabelText("The video's file")
    expect(file.textContent).toBe("/home/me/talk/storyboard.mp4")
    fireEvent.click(screen.getByRole("button", { name: "Copy path" }))
    expect(writeText).toHaveBeenCalledWith("/home/me/talk/storyboard.mp4")
    expect(await screen.findByRole("button", { name: "Copied" })).toBeTruthy()

    // While the video is made again the file is not yet the new one.
    studio.project = project(reviewed().scenes, { render: { running: true, log: [], file: "/home/me/talk/storyboard.mp4" } })
    await waitFor(() => expect(screen.queryByLabelText("The video's file")).toBeNull())
  })

  it("leaves the path to be selected by hand where the browser will not copy", async () => {
    // A page not served over a safe connection has no clipboard.
    Object.defineProperty(navigator, "clipboard", { value: undefined, configurable: true })
    studio.project = project(reviewed().scenes, { render: { running: false, log: [], video: "/media/video?v=9", file: "/home/me/talk/storyboard.mp4" } })
    render(<AgentStudioPanel item={item()} fill pollMs={30} />)
    fireEvent.click(await screen.findByRole("button", { name: "Copy path" }))
    expect(await screen.findByRole("button", { name: "Could not copy" })).toBeTruthy()
    expect(screen.getByLabelText("The video's file").textContent).toBe("/home/me/talk/storyboard.mp4")
  })

  // A plan nobody has drawn yet: two scenes that say what they will show,
  // and one that has its picture.
  const draft = (overrides: Partial<StudioProject> = {}) => project([
    scene({ id: "intro", kind: "planned", planned: true, chapter: "Start", shows: "A title card", notes: "- What restack is", still: "/api/scenes/intro/still.png?v=p1" }),
    scene({ id: "demo", kind: "planned", planned: true, shows: "A terminal running restack", notes: "- One command\n- Nothing by hand", still: "/api/scenes/demo/still.png?v=p2" }),
    scene({ id: "sync", kind: "card", chapter: "More", shows: "A card about sync", notes: "- The whole stack", still: "/api/scenes/sync/still.png?v=p3" }),
  ], overrides)
  const posted = (path: string) => studio.calls.filter((call) => call.method !== "GET" && call.address === STUDIO + path)
  const told = (call: { headers?: unknown }) => new Headers(call.headers as HeadersInit).get("X-Legato-Studio-Notify")

  it("opens on the plan while there is planning to do, and takes what the person wants to cover", async () => {
    studio.project = draft()
    studio.answer = (method, address, body) => {
      if (method === "POST" && address.endsWith("/api/brief")) return draft({ brief: JSON.parse(String(body)).text })
    }
    render(<AgentUIItemView item={item()} fill />)
    expect((await screen.findByRole("tab", { name: /Plan/ })).getAttribute("aria-selected")).toBe("true")
    // How many pictures are still to be drawn is on the tab and in the footer.
    expect(screen.getByRole("tab", { name: /Plan/ }).textContent).toContain("2")
    expect(screen.getByRole("status", { name: "The plan" }).textContent).toBe("2 scenes are not drawn yet")
    // A guess at the length, from how much there is to say: 14 + 24 + 14 s.
    expect(screen.getByText("3 scenes · about 0:52")).toBeTruthy()
    expect(outline().getByRole("button", { name: /demo/ }).textContent).toContain("~0:24")

    const brief = screen.getByRole("textbox", { name: "What you want the video to cover" })
    expect((screen.getByRole("button", { name: "Propose a plan" }) as HTMLButtonElement).disabled).toBe(true)
    fireEvent.change(brief, { target: { value: "How restack works, for people who stack branches. Two minutes." } })
    fireEvent.click(screen.getByRole("button", { name: "Propose a plan" }))
    expect(await screen.findByText("How restack works, for people who stack branches. Two minutes.")).toBeTruthy()
    // The agent is told: drafting the scenes from it is the agent's work.
    expect(JSON.parse(String(posted("/api/brief")[0].body))).toEqual({ text: "How restack works, for people who stack branches. Two minutes." })
    expect(told(posted("/api/brief")[0])).toBe("1")
    // Written, it is read back, and can be written again.
    expect(screen.queryByRole("textbox", { name: "What you want the video to cover" })).toBeNull()
    fireEvent.click(screen.getByRole("button", { name: "Edit" }))
    expect((screen.getByRole("textbox", { name: "What you want the video to cover" }) as HTMLTextAreaElement).value).toContain("How restack works")
  })

  it("lets the person rewrite a scene of the plan by hand", async () => {
    studio.project = draft()
    studio.answer = (method, address, body) => {
      if (method !== "PATCH" || !address.endsWith("/api/scenes/demo")) return
      const change = JSON.parse(String(body))
      const scenes = draft().scenes
      scenes[1] = { ...scenes[1], ...change }
      return project(scenes)
    }
    render(<AgentStudioPanel item={item({}, "demo")} fill />)
    const points = await screen.findByRole("textbox", { name: "Talking points of this scene" }) as HTMLTextAreaElement
    // One to a line, without the dashes the file keeps.
    expect(points.value).toBe("One command\nNothing by hand")
    expect((screen.getByRole("textbox", { name: "What this scene shows" }) as HTMLInputElement).value).toBe("A terminal running restack")

    fireEvent.change(points, { target: { value: "One command\nNothing by hand\n  Safe to run twice " } })
    fireEvent.blur(points)
    await waitFor(() => expect(posted("/api/scenes/demo")).toHaveLength(1))
    expect(JSON.parse(String(posted("/api/scenes/demo")[0].body))).toEqual({ notes: "- One command\n- Nothing by hand\n- Safe to run twice" })
    // Nothing is drawn yet, so there is nothing for the agent to redo.
    expect(told(posted("/api/scenes/demo")[0])).toBeNull()

    const shows = screen.getByRole("textbox", { name: "What this scene shows" })
    fireEvent.change(shows, { target: { value: "A terminal: restack, then the log" } })
    fireEvent.blur(shows)
    await waitFor(() => expect(posted("/api/scenes/demo")).toHaveLength(2))
    expect(JSON.parse(String(posted("/api/scenes/demo")[1].body))).toEqual({ shows: "A terminal: restack, then the log" })
    // Leaving a field as it was sends nothing.
    fireEvent.blur(screen.getByRole("textbox", { name: "What this scene shows" }))
    expect(posted("/api/scenes/demo")).toHaveLength(2)
  })

  it("moves scenes, renames chapters and adds a scene from a line", async () => {
    studio.project = draft()
    render(<AgentStudioPanel item={item({}, "demo")} fill />)
    fireEvent.click(await screen.findByRole("button", { name: "Move this scene up" }))
    await waitFor(() => expect(posted("/api/scenes/demo/move")).toHaveLength(1))
    expect(JSON.parse(String(posted("/api/scenes/demo/move")[0].body))).toEqual({ by: -1 })
    await waitFor(() => expect((screen.getByRole("button", { name: "Move this scene down" }) as HTMLButtonElement).disabled).toBe(false))
    fireEvent.click(screen.getByRole("button", { name: "Move this scene down" }))
    await waitFor(() => expect(posted("/api/scenes/demo/move")).toHaveLength(2))

    fireEvent.click(screen.getByRole("button", { name: "Rename the chapter More" }))
    const title = screen.getByRole("textbox", { name: "The title of the chapter More" })
    fireEvent.change(title, { target: { value: "Going further" } })
    fireEvent.submit(title.closest("form")!)
    await waitFor(() => expect(posted("/api/chapters")).toHaveLength(1))
    expect(JSON.parse(String(posted("/api/chapters")[0].body))).toEqual({ from: "More", to: "Going further" })

    // A new scene goes after the one in front of the person, and is the
    // agent's to fill in.
    await waitFor(() => expect((screen.getByRole("textbox", { name: "A new scene, in a line" }) as HTMLInputElement).disabled).toBe(false))
    fireEvent.change(screen.getByRole("textbox", { name: "A new scene, in a line" }), { target: { value: "What happens on a conflict" } })
    await waitFor(() => expect((screen.getByRole("button", { name: "Add to the plan" }) as HTMLButtonElement).disabled).toBe(false))
    fireEvent.click(screen.getByRole("button", { name: "Add to the plan" }))
    await waitFor(() => expect(posted("/api/scenes")).toHaveLength(1))
    expect(JSON.parse(String(posted("/api/scenes")[0].body))).toEqual({ shows: "What happens on a conflict", after: "demo" })
    expect(told(posted("/api/scenes")[0])).toBe("1")
  })

  it("starts each view from its top", async () => {
    studio.project = draft()
    render(<AgentStudioPanel item={item()} fill />)
    await screen.findByRole("navigation", { name: "Outline" })
    // The person has read down the plan. The other view is another page, and
    // opens at its start, with its tabs in sight.
    const scroller = screen.getByRole("tablist", { name: "Studio" }).parentElement as HTMLElement
    scroller.scrollTop = 400
    openView("Record")
    expect(scroller.scrollTop).toBe(0)
  })

  it("agrees the plan, and then goes on to record", async () => {
    studio.project = draft()
    studio.answer = (method, address) => {
      if (method === "POST" && address.endsWith("/api/agree")) return draft({ comments: ["The person agreed the plan in the studio. Draw the picture of each scene that is only planned (intro, demo)."] })
    }
    render(<AgentStudioPanel item={item()} fill pollMs={20} />)
    fireEvent.click(await screen.findByRole("button", { name: "Agree the plan" }))
    await waitFor(() => expect(screen.getByRole("status", { name: "The plan" }).textContent).toBe("Agreed: the agent is drawing 2 pictures"))
    expect(told(posted("/api/agree")[0])).toBe("1")
    expect((screen.getByRole("button", { name: "Plan agreed" }) as HTMLButtonElement).disabled).toBe(true)
    // A scene that is not drawn cannot be recorded yet.
    expect((screen.getByRole("button", { name: "Record this scene" }) as HTMLButtonElement).disabled).toBe(true)

    // The agent draws them, and the plan is ready.
    studio.project = project(draft().scenes.map((s) => ({ ...s, kind: "card", planned: false })))
    fireEvent.click(await screen.findByRole("button", { name: "Start recording" }))
    expect(screen.getByRole("tab", { name: /Record/ }).getAttribute("aria-selected")).toBe("true")
    expect(strip().getByRole("button", { name: "Scene 1: intro" }).getAttribute("aria-current")).toBe("true")
    expect(screen.getByRole("button", { name: "Record" })).toBeTruthy()
  })
})
