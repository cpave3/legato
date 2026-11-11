import { describe, expect, it, vi } from "vitest"
// The protocol's shared fixtures, which the Go server's tests also read.
import messagesRaw from "../../../../internal/server/testdata/voice-session/messages.json?raw"
import { initialVoiceView, reduceVoice, VoiceSessionClient, type AudioPort, type VoiceOut, type SocketLike } from "../session"

const messages = JSON.parse(messagesRaw) as { device: Record<string, unknown>[]; main: VoiceOut[] }

describe("the voice view", () => {
  it("takes every message the main sends in the shared fixture", () => {
    let view = initialVoiceView
    for (const msg of messages.main) {
      view = reduceVoice(view, msg)
    }
    expect(view.ended).toBe("bye")
  })

  it("follows a turn: listening with words and the pause bar, then what was heard and done", () => {
    let v = reduceVoice(initialVoiceView, { type: "state", state: "listening" })
    v = reduceVoice(v, { type: "partial", text: "turn the" })
    v = reduceVoice(v, { type: "pause", ms: 300, of: 1000 })
    expect(v).toMatchObject({ state: "listening", partial: "turn the", pause: 0.3 })
    v = reduceVoice(v, { type: "pause", of: 1000 })
    expect(v.pause).toBe(0)
    v = reduceVoice(v, { type: "state", state: "thinking" })
    v = reduceVoice(v, { type: "heard", text: "turn the lights off" })
    v = reduceVoice(v, { type: "activity", text: "Reading config.yaml" })
    expect(v).toMatchObject({ state: "thinking", heard: "turn the lights off", activity: "Reading config.yaml", partial: "" })
    v = reduceVoice(v, { type: "line", id: "a", text: "The lights are off.", url: "/x" })
    v = reduceVoice(v, { type: "state", state: "speaking" })
    expect(v.lines).toEqual([{ id: "a", text: "The lights are off.", url: "/x" }])
    v = reduceVoice(v, { type: "state", state: "listening" })
    expect(v.partial).toBe("")
  })

  it("says when the Reeve answered in writing, and why it went idle", () => {
    let v = reduceVoice(initialVoiceView, { type: "written" })
    expect(v.written).toBe(true)
    v = reduceVoice(v, { type: "state", state: "idle", reason: "no_speech" })
    expect(v.reason).toBe("no_speech")
    v = reduceVoice(v, { type: "state", state: "listening" })
    expect(v.written).toBe(false)
    expect(v.reason).toBe("")
  })
})

// A socket a test drives by hand.
class FakeSocket implements SocketLike {
  sent: (string | ArrayBuffer)[] = []
  binaryType = "arraybuffer"
  readyState = 0
  onopen: (() => void) | null = null
  onmessage: ((ev: { data: unknown }) => void) | null = null
  onclose: (() => void) | null = null
  onerror: (() => void) | null = null
  send(data: string | ArrayBuffer) { this.sent.push(data) }
  close() { this.readyState = 3; this.onclose?.() }
  open() { this.readyState = 1; this.onopen?.() }
  receive(msg: VoiceOut) { this.onmessage?.({ data: JSON.stringify(msg) }) }
  texts() { return this.sent.filter((s): s is string => typeof s === "string").map((s) => JSON.parse(s)) }
  frames() { return this.sent.filter((s) => typeof s !== "string").length }
}

class FakeAudio implements AudioPort {
  onFrame: ((pcm: ArrayBuffer, level: number) => void) | null = null
  onStopped: (() => void) | null = null
  starts = 0
  capturing = false
  played: string[] = []
  finish: (() => void) | null = null
  canPlay = true
  async startCapture(onFrame: (pcm: ArrayBuffer, level: number) => void, onStopped?: () => void) {
    this.onFrame = onFrame; this.onStopped = onStopped ?? null; this.capturing = true; this.starts++
  }
  stopCapture() { this.capturing = false; this.onFrame = null }
  play(url: string) {
    this.played.push(url)
    return new Promise<void>((resolve) => { this.finish = resolve })
  }
  stopPlayback() { this.finish?.(); this.finish = null }
  frame() { this.onFrame?.(new Int16Array(1600).buffer, 0.5) }
}

function connect(opts: { muteAfterMs?: number } = {}) {
  const socket = new FakeSocket()
  const audio = new FakeAudio()
  const views: string[] = []
  const client = new VoiceSessionClient({ url: "wss://main/api/reeve/voice/session?token=t", device: "Pixel", client: "web", audio, socket: () => socket, onView: (v) => views.push(v.state), muteAfterMs: opts.muteAfterMs ?? 0 })
  void client.connect()
  socket.open()
  return { socket, audio, client, views }
}

describe("the voice session client", () => {
  it("says hello as the shared fixture's device does, then listens and streams frames", async () => {
    const { socket, audio, client } = connect()
    const hello = socket.texts()[0]
    expect(Object.keys(hello).sort()).toEqual(Object.keys(messages.device[0]).sort())
    expect(hello).toMatchObject({ type: "hello", device: "Pixel", client: "web", play: true })
    await client.listen()
    expect(socket.texts().at(-1)).toEqual({ type: "listen" })
    socket.receive({ type: "state", state: "listening" })
    audio.frame()
    audio.frame()
    expect(socket.frames()).toBe(2)
    expect(client.view.level).toBe(0.5)
  })

  it("sends every device message in the shared fixture's shape", async () => {
    const { socket, client } = connect()
    await client.listen()
    client.send()
    client.cancel()
    client.stop()
    client.bye()
    const types = socket.texts().map((m) => m.type)
    for (const t of ["hello", "listen", "send", "cancel", "stop", "bye"]) expect(types).toContain(t)
  })

  it("plays the Reeve's lines in order, mutes the microphone meanwhile, and says played", async () => {
    const { socket, audio, client } = connect()
    await client.listen()
    socket.receive({ type: "state", state: "thinking" })
    socket.receive({ type: "line", id: "one", text: "One.", url: "/api/reeve/voice/lines/one" })
    socket.receive({ type: "line", id: "two", text: "Two.", url: "/api/reeve/voice/lines/two" })
    socket.receive({ type: "state", state: "speaking" })
    await vi.waitFor(() => expect(audio.played).toEqual(["/api/reeve/voice/lines/one"]))
    expect(socket.texts()).toContainEqual({ type: "playing", line: "one" })
    expect(client.view.speaking).toBe("one")
    audio.frame()
    expect(socket.frames()).toBe(0)
    audio.finish?.()
    await vi.waitFor(() => expect(audio.played).toEqual(["/api/reeve/voice/lines/one", "/api/reeve/voice/lines/two"]))
    expect(socket.texts()).toContainEqual({ type: "played", line: "one" })
    audio.finish?.()
    await vi.waitFor(() => expect(socket.texts()).toContainEqual({ type: "played", line: "two" }))
    socket.receive({ type: "state", state: "listening" })
    audio.frame()
    expect(socket.frames()).toBe(1)
  })

  it("keeps the microphone muted a moment after a line", async () => {
    vi.useFakeTimers()
    try {
      const { socket, audio, client } = connect({ muteAfterMs: 300 })
      await client.listen()
      socket.receive({ type: "line", id: "one", text: "One.", url: "/one" })
      await vi.waitFor(() => expect(audio.played).toHaveLength(1))
      audio.finish?.()
      await vi.waitFor(() => expect(socket.texts()).toContainEqual({ type: "played", line: "one" }))
      socket.receive({ type: "state", state: "listening" })
      audio.frame()
      expect(socket.frames()).toBe(0)
      vi.advanceTimersByTime(301)
      audio.frame()
      expect(socket.frames()).toBe(1)
    } finally {
      vi.useRealTimers()
    }
  })

  it("stop drops the lines left and stops playback; reply does so and listens", async () => {
    const { socket, audio, client } = connect()
    await client.listen()
    socket.receive({ type: "line", id: "one", text: "One.", url: "/one" })
    socket.receive({ type: "line", id: "two", text: "Two.", url: "/two" })
    await vi.waitFor(() => expect(audio.played).toHaveLength(1))
    client.stop()
    await Promise.resolve()
    expect(audio.played).toHaveLength(1)
    expect(client.view.lines).toEqual([])
    socket.receive({ type: "line", id: "three", text: "Three.", url: "/three" })
    await vi.waitFor(() => expect(audio.played).toHaveLength(2))
    await client.reply()
    expect(client.view.lines).toEqual([])
    expect(socket.texts().filter((m) => m.type === "listen")).toHaveLength(2)
  })

  it("starts the microphone again when the app stopped it and the main listens", async () => {
    const { socket, audio, client } = connect()
    await client.listen()
    expect(audio.starts).toBe(1)
    // The app lost the audio focus to the line playing, and stopped.
    audio.capturing = false
    audio.onStopped?.()
    socket.receive({ type: "state", state: "listening" })
    await vi.waitFor(() => expect(audio.starts).toBe(2))
  })

  it("stops capturing when the session ends", async () => {
    const { socket, audio, client } = connect()
    await client.listen()
    expect(audio.capturing).toBe(true)
    socket.receive({ type: "ended", reason: "taken over by desk" })
    expect(audio.capturing).toBe(false)
    expect(client.view.ended).toBe("taken over by desk")
  })
})
