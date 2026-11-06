import { act, renderHook, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { useVoiceRecorder } from "../useVoiceRecorder"

// What the Legato app tells the page about a recording it makes.
const appSays = (detail: Record<string, unknown>) =>
  act(() => { window.dispatchEvent(new CustomEvent("legato:shell-voice", { detail })) })

describe("useVoiceRecorder", () => {
  const posted: unknown[] = []
  let mediaDevices: PropertyDescriptor | undefined

  beforeEach(() => {
    posted.length = 0
    mediaDevices = Object.getOwnPropertyDescriptor(navigator, "mediaDevices")
    // A page served over plain HTTP to a phone has no microphone of its own.
    Object.defineProperty(navigator, "mediaDevices", { configurable: true, value: undefined })
    vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(JSON.stringify({ text: "which tickets have designs" }), { status: 200 }))
  })

  afterEach(() => {
    if (mediaDevices) Object.defineProperty(navigator, "mediaDevices", mediaDevices)
    else delete (navigator as { mediaDevices?: unknown }).mediaDevices
    delete (window as { legatoShell?: unknown }).legatoShell
    delete (window as { ReactNativeWebView?: unknown }).ReactNativeWebView
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  function inTheApp() {
    Object.assign(window, {
      legatoShell: { voice: 1, key: "page-key" },
      ReactNativeWebView: { postMessage: (data: string) => posted.push(JSON.parse(data)) },
    })
    // The app records AAC; the page decodes it at the transcriber's rate.
    vi.stubGlobal("OfflineAudioContext", class {
      decodeAudioData() {
        return Promise.resolve({ numberOfChannels: 1, getChannelData: () => new Float32Array([0, 0.5, -0.5, 1]) })
      }
    })
  }

  it("says why there is no microphone on a page that has none", async () => {
    const { result } = renderHook(() => useVoiceRecorder({ baseUrl: "" }))

    await act(() => result.current.start())

    expect(result.current.state).toBe("error")
    expect(result.current.error).toMatch(/HTTPS/)
  })

  it("has the Legato app record, and transcribes what it recorded", async () => {
    inTheApp()
    const onTranscription = vi.fn()
    const { result } = renderHook(() => useVoiceRecorder({ baseUrl: "", transcriptionOnly: true, onTranscription }))

    await act(() => result.current.start())
    expect(posted).toEqual([{ legatoShell: "voice", key: "page-key", action: "start" }])
    appSays({ type: "started" })
    expect(result.current.state).toBe("recording")
    appSays({ type: "level", level: 0.6 })
    expect(result.current.levels).toEqual([0.6])

    await act(() => result.current.stop())
    expect(posted).toContainEqual({ legatoShell: "voice", key: "page-key", action: "stop" })
    expect(result.current.state).toBe("transcribing")
    appSays({ type: "audio", data: btoa("aac"), mime: "audio/mp4" })

    await waitFor(() => expect(onTranscription).toHaveBeenCalledWith("which tickets have designs"))
    const body = JSON.parse(String(vi.mocked(fetch).mock.calls[0][1]?.body))
    expect(body.transcription_only).toBe(true)
    // 0, 0.5, -0.5 and 1 as little-endian 16-bit samples.
    expect(body.pcm).toEqual([0, 0, 0xff, 0x3f, 0x00, 0xc0, 0xff, 0x7f])
  })

  // The desktop app's AppImage carries WebKitGTK without GStreamer's
  // plugins, and there decodeAudioData never settles. The app's recording is
  // already the transcriber's 16 kHz mono 16-bit samples in a WAV, so the
  // page reads them as they are.
  it("reads the desktop app's WAV without decoding it", async () => {
    inTheApp()
    vi.stubGlobal("OfflineAudioContext", class {
      decodeAudioData() { return new Promise(() => {}) }
    })
    const onTranscription = vi.fn()
    const { result } = renderHook(() => useVoiceRecorder({ baseUrl: "", transcriptionOnly: true, onTranscription }))

    await act(() => result.current.start())
    appSays({ type: "started" })
    await act(() => result.current.stop())
    const samples = [0, 0x3fff, -0x4000, 0x7fff]
    const wav = new DataView(new ArrayBuffer(44 + samples.length * 2))
    const text = (at: number, s: string) => [...s].forEach((c, i) => wav.setUint8(at + i, c.charCodeAt(0)))
    text(0, "RIFF"); wav.setUint32(4, 36 + samples.length * 2, true); text(8, "WAVE")
    text(12, "fmt "); wav.setUint32(16, 16, true); wav.setUint16(20, 1, true); wav.setUint16(22, 1, true)
    wav.setUint32(24, 16000, true); wav.setUint32(28, 32000, true); wav.setUint16(32, 2, true); wav.setUint16(34, 16, true)
    text(36, "data"); wav.setUint32(40, samples.length * 2, true)
    samples.forEach((s, i) => wav.setInt16(44 + i * 2, s, true))
    appSays({ type: "audio", data: btoa(String.fromCharCode(...new Uint8Array(wav.buffer))), mime: "audio/wav" })

    await waitFor(() => expect(onTranscription).toHaveBeenCalledWith("which tickets have designs"))
    const body = JSON.parse(String(vi.mocked(fetch).mock.calls[0][1]?.body))
    expect(body.pcm).toEqual([0, 0, 0xff, 0x3f, 0x00, 0xc0, 0xff, 0x7f])
  })

  it("asks the app to throw a recording away", async () => {
    inTheApp()
    const { result } = renderHook(() => useVoiceRecorder({ baseUrl: "" }))

    await act(() => result.current.start())
    appSays({ type: "started" })
    act(() => result.current.cancel())

    expect(posted).toContainEqual({ legatoShell: "voice", key: "page-key", action: "cancel" })
    expect(result.current.state).toBe("idle")
  })

  it("shows what went wrong in the app", async () => {
    inTheApp()
    const { result } = renderHook(() => useVoiceRecorder({ baseUrl: "" }))

    await act(() => result.current.start())
    appSays({ type: "error", message: "Legato may not use the microphone." })

    expect(result.current.state).toBe("error")
    expect(result.current.error).toBe("Legato may not use the microphone.")
  })
})
