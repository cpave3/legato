import { afterEach, describe, expect, it, vi } from "vitest"
import { browserAudio } from "../audio"

function deferred<T>() {
  let resolve!: (v: T) => void
  const promise = new Promise<T>((r) => { resolve = r })
  return { promise, resolve }
}

class Track extends EventTarget {
  stop = vi.fn()
}

function stubMicrophone() {
  const track = new Track()
  const stream = { getTracks: () => [track], getAudioTracks: () => [track] } as unknown as MediaStream
  const pending = deferred<MediaStream>()
  vi.stubGlobal("navigator", { ...navigator, mediaDevices: { getUserMedia: vi.fn(() => pending.promise) } })
  class FakeContext {
    sampleRate = 16000
    destination = {}
    createMediaStreamSource() { return { connect: () => {}, disconnect: () => {} } }
    createScriptProcessor() { return { connect: () => {}, disconnect: () => {}, onaudioprocess: null } }
    close() { return Promise.resolve() }
  }
  vi.stubGlobal("AudioContext", FakeContext)
  return { track, stream, grant: () => pending.resolve(stream) }
}

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
