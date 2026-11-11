import { apiFetch } from "../lib/api"
import type { AudioPort } from "./session"

// The voice session's audio is 16 kHz mono 16-bit PCM, in frames of about
// 100 ms.
const RATE = 16000
const FRAME = 1600

// level is a frame's loudness, 0 to 1, for the ring.
export function level(pcm: Int16Array): number {
  if (!pcm.length) return 0
  let sum = 0
  for (const s of pcm) sum += s * s
  const rms = Math.sqrt(sum / pcm.length)
  // -60 dBFS and below is silence, full scale is 1.
  const db = 20 * Math.log10(Math.max(rms, 1) / 32768)
  return Math.max(0, Math.min(1, (db + 60) / 60))
}

function toPcm16(samples: Float32Array): Int16Array {
  const pcm = new Int16Array(samples.length)
  for (let i = 0; i < samples.length; i++) {
    const s = Math.max(-1, Math.min(1, samples[i]))
    pcm[i] = s < 0 ? s * 0x8000 : s * 0x7fff
  }
  return pcm
}

// Framer cuts a stream of samples into frames of FRAME.
export class Framer {
  private buf = new Int16Array(0)
  private onFrame: (pcm: ArrayBuffer, level: number) => void
  constructor(onFrame: (pcm: ArrayBuffer, level: number) => void) { this.onFrame = onFrame }
  push(samples: Int16Array) {
    const all = new Int16Array(this.buf.length + samples.length)
    all.set(this.buf)
    all.set(samples, this.buf.length)
    let at = 0
    for (; at + FRAME <= all.length; at += FRAME) {
      const frame = all.slice(at, at + FRAME)
      this.onFrame(frame.buffer, level(frame))
    }
    this.buf = all.slice(at)
  }
}

// LinePlayer plays the Reeve's lines with an <audio> element, fetching
// each with the server's token: a page cannot add a header to <audio src>.
// stop also cancels a line still downloading.
class LinePlayer {
  private audio: HTMLAudioElement | null = null
  private done: (() => void) | null = null
  private download: AbortController | null = null
  private generation = 0
  private baseUrl: string
  constructor(baseUrl: string) { this.baseUrl = baseUrl }

  async play(url: string): Promise<void> {
    this.stop()
    const mine = ++this.generation
    const download = new AbortController()
    this.download = download
    let src = ""
    try {
      const res = await apiFetch(this.baseUrl, url, { signal: download.signal })
      if (!res.ok) throw new Error(`line: ${res.status}`)
      const blob = await res.blob()
      if (mine !== this.generation) return
      src = URL.createObjectURL(blob)
    } catch (err) {
      if (mine !== this.generation) return
      throw err
    } finally {
      if (this.download === download) this.download = null
    }
    const audio = new Audio(src)
    this.audio = audio
    try {
      await new Promise<void>((resolve) => {
        this.done = resolve
        audio.onended = () => resolve()
        audio.onerror = () => resolve()
        audio.play().catch(() => resolve())
      })
    } finally {
      URL.revokeObjectURL(src)
      if (this.audio === audio) this.audio = null
      this.done = null
    }
  }

  stop() {
    this.generation++
    this.download?.abort()
    this.download = null
    this.audio?.pause()
    this.done?.()
  }
}

// browserAudio is the page's own microphone (getUserMedia, which needs
// HTTPS) and an <audio> element.
export function browserAudio(baseUrl: string): AudioPort {
  const player = new LinePlayer(baseUrl)
  let stop: (() => void) | null = null
  // Each start's own; stopping before the microphone is granted cancels it.
  let starting = 0
  return {
    canPlay: true,
    async startCapture(onFrame, onStopped) {
      const mine = ++starting
      const stream = await navigator.mediaDevices.getUserMedia({ audio: { channelCount: 1, echoCancellation: true, noiseSuppression: true } })
      if (mine !== starting) {
        stream.getTracks().forEach((t) => t.stop())
        throw new Error("stopped before the microphone was ready")
      }
      // A context at 16 kHz resamples for us where the browser can.
      const ctx = new AudioContext({ sampleRate: RATE })
      const source = ctx.createMediaStreamSource(stream)
      const processor = ctx.createScriptProcessor(2048, 1, 1)
      const framer = new Framer(onFrame)
      const ratio = ctx.sampleRate / RATE
      processor.onaudioprocess = (ev) => {
        const input = ev.inputBuffer.getChannelData(0)
        if (ratio === 1) {
          framer.push(toPcm16(input))
          return
        }
        // A context that kept the device's rate: take every ratio-th sample.
        const out = new Float32Array(Math.floor(input.length / ratio))
        for (let i = 0; i < out.length; i++) out[i] = input[Math.floor(i * ratio)]
        framer.push(toPcm16(out))
      }
      source.connect(processor)
      processor.connect(ctx.destination)
      const release = () => {
        processor.disconnect()
        source.disconnect()
        stream.getTracks().forEach((t) => t.stop())
        ctx.close().catch(() => {})
      }
      stop = release
      // The device went away, or the browser took the microphone back.
      for (const track of stream.getTracks()) {
        track.addEventListener("ended", () => {
          if (stop !== release) return
          release()
          stop = null
          onStopped?.()
        })
      }
    },
    stopCapture() {
      starting++
      stop?.()
      stop = null
    },
    play: (url) => player.play(url),
    stopPlayback: () => player.stop(),
  }
}

// The Android app's shell streams its microphone to the page (AudioRecord,
// 16 kHz mono PCM16) when it offers voiceStream; see docs/ai/web-ui.md §
// The Legato app's microphone.
interface ShellObject { voiceStream?: number; key?: string }
type ShellBridge = { postMessage: (data: string) => void }
type ShellStreamEvent =
  | { type: "stream-started" }
  | { type: "frame"; data: string }
  | { type: "stream-stopped" }
  | { type: "error"; message: string }

function shell(): { key: string; post: (msg: object) => void } | null {
  const object = (window as { legatoShell?: ShellObject }).legatoShell
  const bridge = (window as { ReactNativeWebView?: ShellBridge }).ReactNativeWebView
  if (object?.voiceStream !== 1 || typeof object.key !== "string" || !bridge) return null
  const key = object.key
  return { key, post: (msg) => bridge.postMessage(JSON.stringify({ ...msg, key })) }
}

// shellStreams reports whether the app the page runs in streams its
// microphone.
export function shellStreams(): boolean {
  return shell() !== null
}

// openMainVoice asks the app to open the main's page with the voice sheet:
// a worker's page cannot talk to the main's Reeve.
export function openMainVoice(): boolean {
  const s = shell()
  if (!s) return false
  s.post({ legatoShell: "open-voice" })
  return true
}

function decode(base64: string): Int16Array {
  const bytes = Uint8Array.from(atob(base64), (c) => c.charCodeAt(0))
  return new Int16Array(bytes.buffer, 0, Math.floor(bytes.length / 2))
}

// shellAudio is the app's microphone, with lines played by the page.
export function shellAudio(baseUrl: string): AudioPort {
  const player = new LinePlayer(baseUrl)
  let listener: ((ev: Event) => void) | null = null
  const s = shell()
  return {
    canPlay: true,
    startCapture(onFrame, onStopped) {
      if (!s) return Promise.reject(new Error("this app does not stream its microphone"))
      return new Promise<void>((resolve, reject) => {
        listener = (ev: Event) => {
          const detail = (ev as CustomEvent<ShellStreamEvent>).detail
          switch (detail?.type) {
            case "stream-started": resolve(); break
            case "frame": {
              const pcm = decode(detail.data)
              onFrame(pcm.slice().buffer, level(pcm))
              resolve()
              break
            }
            case "error": reject(new Error(detail.message)); break
            case "stream-stopped":
              if (listener) window.removeEventListener("legato:shell-voice", listener)
              listener = null
              onStopped?.()
              break
          }
        }
        window.addEventListener("legato:shell-voice", listener)
        s.post({ legatoShell: "voice-stream", action: "start" })
      })
    },
    stopCapture() {
      if (listener) window.removeEventListener("legato:shell-voice", listener)
      listener = null
      s?.post({ legatoShell: "voice-stream", action: "stop" })
    },
    play: (url) => player.play(url),
    stopPlayback: () => player.stop(),
  }
}

// voiceAudio is the audio a page can hold a conversation with: the app's
// stream, else the browser's microphone; null when it has neither.
export function voiceAudio(baseUrl: string): AudioPort | null {
  if (shellStreams()) return shellAudio(baseUrl)
  if (typeof navigator !== "undefined" && typeof navigator.mediaDevices?.getUserMedia === "function" && window.isSecureContext) return browserAudio(baseUrl)
  return null
}
