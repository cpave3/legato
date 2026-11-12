import { afterEach, describe, expect, it, vi } from "vitest"
import { browserAudio, Resampler } from "../audio"

function deferred<T>() {
  let resolve!: (v: T) => void
  const promise = new Promise<T>((r) => { resolve = r })
  return { promise, resolve }
}

class Track extends EventTarget {
  stop = vi.fn()
}

function stubMicrophone({ state = "running", sourceFails = false }: { state?: string; sourceFails?: boolean } = {}) {
  const track = new Track()
  const stream = { getTracks: () => [track], getAudioTracks: () => [track] } as unknown as MediaStream
  const pending = deferred<MediaStream>()
  vi.stubGlobal("navigator", { ...navigator, mediaDevices: { getUserMedia: vi.fn(() => pending.promise) } })
  const contexts: FakeContext[] = []
  class FakeContext {
    sampleRate = 16000
    destination = {}
    state = state
    closed = false
    resume = vi.fn(async () => { this.state = "running" })
    constructor() { contexts.push(this) }
    createMediaStreamSource() {
      if (sourceFails) throw new Error("NotSupportedError")
      return { connect: () => {}, disconnect: () => {} }
    }
    createScriptProcessor() { return { connect: () => {}, disconnect: () => {}, onaudioprocess: null } }
    close() { this.closed = true; return Promise.resolve() }
  }
  vi.stubGlobal("AudioContext", FakeContext)
  return { track, stream, contexts, grant: () => pending.resolve(stream) }
}

function sine(freq: number, rate: number, seconds: number): Float32Array {
  const out = new Float32Array(Math.round(rate * seconds))
  for (let i = 0; i < out.length; i++) out[i] = 0.5 * Math.sin((2 * Math.PI * freq * i) / rate)
  return out
}

function rms(samples: Float32Array, skip = 0): number {
  let sum = 0
  for (let i = skip; i < samples.length; i++) sum += samples[i] * samples[i]
  return Math.sqrt(sum / (samples.length - skip))
}

function resample(from: number, input: Float32Array, chunk: number): Float32Array {
  const r = new Resampler(from, 16000)
  const parts: Float32Array[] = []
  for (let at = 0; at < input.length; at += chunk) parts.push(r.push(input.subarray(at, at + chunk)))
  const out = new Float32Array(parts.reduce((n, p) => n + p.length, 0))
  let at = 0
  for (const p of parts) { out.set(p, at); at += p.length }
  return out
}

describe("Resampler", () => {
  it("keeps speech-band sound at 16 kHz", () => {
    const out = resample(48000, sine(1000, 48000, 1), 2048)
    expect(Math.abs(out.length - 16000)).toBeLessThanOrEqual(1)
    expect(rms(out, 160)).toBeGreaterThan(0.33)
  })

  it("filters out sound above 8 kHz instead of folding it into speech", () => {
    // Taking every third sample would turn 12 kHz into a 4 kHz tone at full level.
    const out = resample(48000, sine(12000, 48000, 1), 2048)
    expect(rms(out, 160)).toBeLessThan(0.05)
  })

  it("resamples a rate that is not a multiple of 16 kHz, and chunking changes nothing", () => {
    const input = sine(440, 44100, 1)
    const a = resample(44100, input, 2048)
    const b = resample(44100, input, 777)
    expect(Math.abs(a.length - 16000)).toBeLessThanOrEqual(1)
    expect(b.length).toBe(a.length)
    for (let i = 0; i < a.length; i++) expect(b[i]).toBeCloseTo(a[i], 6)
  })
})

describe("the browser's audio", () => {
  afterEach(() => vi.unstubAllGlobals())

  it("does not play a line whose download was stopped", async () => {
    const download = deferred<Response>()
    vi.stubGlobal("fetch", vi.fn(() => download.promise))
    vi.stubGlobal("URL", Object.assign(URL, { createObjectURL: () => "blob:x", revokeObjectURL: () => {} }))
    const play = vi.spyOn(HTMLMediaElement.prototype, "play")
    const audio = browserAudio("")
    const done = audio.play("/api/reeve/voice/lines/a")
    audio.stopPlayback()
    download.resolve(new Response(new Blob(["ID3"]), { status: 200 }))
    await done
    expect(play).not.toHaveBeenCalled()
    play.mockRestore()
  })

  it("releases a microphone granted after capture was stopped", async () => {
    const mic = stubMicrophone()
    const audio = browserAudio("")
    const started = audio.startCapture(() => {}).catch(() => {})
    audio.stopCapture()
    mic.grant()
    await started
    expect(mic.track.stop).toHaveBeenCalled()
  })

  it("releases the microphone when setting up the audio fails", async () => {
    const mic = stubMicrophone({ sourceFails: true })
    const audio = browserAudio("")
    const started = audio.startCapture(() => {})
    mic.grant()
    await expect(started).rejects.toThrow()
    expect(mic.track.stop).toHaveBeenCalled()
    expect(mic.contexts.every((c) => c.closed)).toBe(true)
  })

  it("resumes an audio context the browser started suspended", async () => {
    const mic = stubMicrophone({ state: "suspended" })
    const audio = browserAudio("")
    const started = audio.startCapture(() => {})
    mic.grant()
    await started
    expect(mic.contexts[0].resume).toHaveBeenCalled()
  })

  it("says so when the microphone's track ends", async () => {
    const mic = stubMicrophone()
    const audio = browserAudio("")
    const stopped = vi.fn()
    const started = audio.startCapture(() => {}, stopped)
    mic.grant()
    await started
    mic.track.dispatchEvent(new Event("ended"))
    expect(stopped).toHaveBeenCalled()
  })
})
