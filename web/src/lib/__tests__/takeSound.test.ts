import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { recordingOf, samplesOf } from "../takeSound"

// The browser's audio thread, played by the test: what the page asked of it,
// and a way to hand the page samples as the thread would.
class FakeSoundNode {
  static made: FakeSoundNode[] = []
  asked: string[] = []
  // What is still on its way to the page when it asks for the take to stop.
  late: Float32Array[] = []
  // A thread that has died answers nothing.
  dead = false
  port = {
    onmessage: null as ((event: { data: unknown }) => void) | null,
    postMessage: (message: string) => {
      this.asked.push(message)
      if (message !== "stop" || this.dead) return
      // The answer comes later, as from another thread, after what was on
      // its way.
      setTimeout(() => {
        for (const samples of this.late) this.port.onmessage?.({ data: samples })
        this.port.onmessage?.({ data: "done" })
      }, 0)
    },
  }
  constructor() { FakeSoundNode.made.push(this) }
  give(samples: Float32Array) { this.port.onmessage?.({ data: samples }) }
  connect = vi.fn()
  disconnect = vi.fn()
}

function fakeContext(sampleRate = 1000) {
  const source = { connect: vi.fn(), disconnect: vi.fn() }
  return {
    state: "running", sampleRate, destination: {}, resume: vi.fn().mockResolvedValue(undefined),
    audioWorklet: { addModule: vi.fn().mockResolvedValue(undefined) },
    createMediaStreamSource: vi.fn(() => source),
  }
}

const tone = (frames: number, value = 0.5) => new Float32Array(frames).fill(value)

async function wavOf(blob: Blob) {
  const view = new DataView(await blob.arrayBuffer())
  const text = (at: number) => String.fromCharCode(view.getUint8(at), view.getUint8(at + 1), view.getUint8(at + 2), view.getUint8(at + 3))
  return {
    kind: text(0) + text(8), channels: view.getUint16(22, true), rate: view.getUint32(24, true), bits: view.getUint16(34, true),
    frames: view.getUint32(40, true) / 2, size: view.byteLength, sample: (n: number) => view.getInt16(44 + n * 2, true),
  }
}

beforeEach(() => {
  FakeSoundNode.made = []
  vi.stubGlobal("AudioWorkletNode", FakeSoundNode)
  URL.createObjectURL = vi.fn(() => "blob:take-sound")
  URL.revokeObjectURL = vi.fn()
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

describe("samplesOf", () => {
  it("gives the sound of a take as a WAV file of every sample, to the last one", async () => {
    const context = fakeContext(1000)
    const sound = await samplesOf(context as unknown as AudioContext, {} as MediaStream)
    const node = FakeSoundNode.made[0]
    expect(context.audioWorklet.addModule).toHaveBeenCalledWith("blob:take-sound")

    sound.start()
    expect(node.asked).toEqual(["start"])
    node.give(tone(1000, 0.5))
    node.give(tone(500, -1))
    // The person's last words are still on their way from the audio thread
    // when the take is stopped. They are waited for.
    node.late = [tone(250, 0.25)]
    const wav = await wavOf(await sound.stop())

    expect(wav).toMatchObject({ kind: "RIFFWAVE", channels: 1, rate: 1000, bits: 16, frames: 1750, size: 44 + 1750 * 2 })
    expect([wav.sample(0), wav.sample(1000), wav.sample(1749)]).toEqual([16383, -32768, 8191])
    expect(sound.heard()).toMatchObject({ seconds: 1.75, loud: true, silence: null })
  })

  it("says how long the microphone gave nothing at all, and from when", async () => {
    const sound = await samplesOf(fakeContext(1000) as unknown as AudioContext, {} as MediaStream)
    const node = FakeSoundNode.made[0]
    sound.start()
    node.give(tone(2000))
    // A room is never this quiet: samples of exactly nothing mean the
    // microphone stopped.
    node.give(tone(1500, 0))
    expect(sound.heard().silentMs).toBe(1500)
    node.give(tone(2500, 0))
    node.give(tone(1000))
    node.give(tone(700, 0))
    await sound.stop()

    // All of it is counted, and the longest stretch is named.
    expect(sound.heard()).toMatchObject({ seconds: 7.7, loud: true, silentMs: 700, silentTotalMs: 4700, silence: { atMs: 2000, ms: 4000 } })
  })

  it("does not count the instants of nothing that any sound passes through", async () => {
    const sound = await samplesOf(fakeContext(1000) as unknown as AudioContext, {} as MediaStream)
    const node = FakeSoundNode.made[0]
    sound.start()
    // A wave crosses nothing many times a second.
    const wave = new Float32Array(3000)
    for (let i = 0; i < wave.length; i++) wave[i] = i % 10 < 3 ? 0 : 0.4
    node.give(wave)
    await sound.stop()
    expect(sound.heard()).toMatchObject({ silentTotalMs: 0, silence: null })
  })

  it("knows a microphone that gave no sound at all", async () => {
    const sound = await samplesOf(fakeContext(1000) as unknown as AudioContext, {} as MediaStream)
    sound.start()
    FakeSoundNode.made[0].give(tone(3000, 0))
    await sound.stop()
    expect(sound.heard().loud).toBe(false)
  })

  it("keeps what came when the audio thread no longer answers", async () => {
    vi.useFakeTimers()
    const sound = await samplesOf(fakeContext(1000) as unknown as AudioContext, {} as MediaStream)
    const node = FakeSoundNode.made[0]
    sound.start()
    node.give(tone(1000))
    node.dead = true
    const stopped = sound.stop()
    await vi.advanceTimersByTimeAsync(5000)
    expect((await wavOf(await stopped)).frames).toBe(1000)
  })

  it("wakes a sound system the browser put to sleep", async () => {
    const context = { ...fakeContext(), state: "suspended" }
    await samplesOf(context as unknown as AudioContext, {} as MediaStream)
    expect(context.resume).toHaveBeenCalled()
  })
})

describe("recordingOf", () => {
  it("records with the browser's recorder, and gives what it handed over", async () => {
    class FakeRecorder {
      static isTypeSupported = (kind: string) => kind === "video/webm"
      state = "inactive"
      mimeType: string
      ondataavailable: ((event: { data: Blob }) => void) | null = null
      onstop: (() => void) | null = null
      started: number[] = []
      constructor(_stream: unknown, options: { mimeType?: string }) { this.mimeType = options.mimeType ?? "" }
      start(slice: number) { this.state = "recording"; this.started.push(slice) }
      stop() {
        this.state = "inactive"
        this.ondataavailable?.({ data: new Blob(["picture"], { type: this.mimeType }) })
        this.onstop?.()
      }
    }
    vi.stubGlobal("MediaRecorder", FakeRecorder)
    const film = recordingOf({} as MediaStream, ["video/webm;codecs=vp9", "video/webm"], { videoBitsPerSecond: 2500000 })
    film.start()
    const blob = await film.stop()
    expect(blob.type).toBe("video/webm")
    expect(blob.size).toBe(7)
    // Stopping twice is stopping once.
    expect((await film.stop()).size).toBe(7)
  })
})
