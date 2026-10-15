import { useCallback, useEffect, useRef, useState } from "react"
import { apiFetch } from "../lib/api"

export type RecordingState = "idle" | "recording" | "transcribing" | "sent" | "error"

export interface VoiceRecorderOptions {
  agentId?: string
  agentKind?: string
  baseUrl: string
  // Only return the text; never send it to the agent.
  transcriptionOnly?: boolean
  onTranscription?: (text: string) => void
}

export interface VoiceRecorder {
  state: RecordingState
  error: string
  // Seconds recorded so far.
  seconds: number
  // How loud the microphone was, 0 to 1, newest last: the bars a recording draws.
  levels: number[]
  start: () => Promise<void>
  stop: () => Promise<void>
  cancel: () => void
}

const LEVELS = 24

// The Legato app records for a page it shows: a page served over plain HTTP
// has no microphone of its own. The app says so before the page loads, with
// a key the page sends back with each request.
interface ShellVoice { key: string; post: (action: "start" | "stop" | "cancel") => void }

function shellVoice(): ShellVoice | null {
  const shell = (window as { legatoShell?: { voice?: number; key?: string } }).legatoShell
  const bridge = (window as { ReactNativeWebView?: { postMessage: (data: string) => void } }).ReactNativeWebView
  if (!shell?.voice || typeof shell.key !== "string" || !bridge) return null
  const key = shell.key
  return { key, post: (action) => bridge.postMessage(JSON.stringify({ legatoShell: "voice", key, action })) }
}

// What the app tells the page about its recording.
type ShellVoiceEvent =
  | { type: "started" }
  | { type: "level"; level: number }
  | { type: "audio"; data: string; mime: string }
  | { type: "cancelled" }
  | { type: "error"; message: string }

function toPcm16(samples: Float32Array): Int16Array {
  const pcm = new Int16Array(samples.length)
  for (let i = 0; i < samples.length; i++) {
    const sample = Math.max(-1, Math.min(1, samples[i]))
    pcm[i] = sample < 0 ? sample * 0x8000 : sample * 0x7fff
  }
  return pcm
}

// The app's recording is compressed; decoding it in a context at 16 kHz gives
// the samples the transcriber takes.
async function decodeRecording(base64: string): Promise<Int16Array> {
  const bytes = Uint8Array.from(atob(base64), (char) => char.charCodeAt(0))
  const audio = await new OfflineAudioContext(1, 1, 16000).decodeAudioData(bytes.buffer)
  return toPcm16(audio.getChannelData(0))
}

// useVoiceRecorder records the microphone as 16 kHz samples and has Legato's
// server transcribe them. Whoever uses it draws the recording.
export function useVoiceRecorder({ agentId, agentKind, baseUrl, transcriptionOnly = false, onTranscription }: VoiceRecorderOptions): VoiceRecorder {
  const [state, setState] = useState<RecordingState>("idle")
  const [error, setError] = useState("")
  const [seconds, setSeconds] = useState(0)
  const [levels, setLevels] = useState<number[]>([])
  const audioCtxRef = useRef<AudioContext | null>(null)
  const streamRef = useRef<MediaStream | null>(null)
  const processorRef = useRef<ScriptProcessorNode | null>(null)
  const sourceRef = useRef<MediaStreamAudioSourceNode | null>(null)
  const pcmBufferRef = useRef<Int16Array[]>([])
  const stateRef = useRef<RecordingState>("idle")
  const timersRef = useRef<number[]>([])
  // Set while the Legato app records for the page.
  const shellRef = useRef<ShellVoice | null>(null)
  stateRef.current = state

  const later = useCallback((action: () => void, ms: number) => {
    timersRef.current.push(window.setTimeout(action, ms))
  }, [])

  const cleanup = useCallback(() => {
    if (processorRef.current) {
      processorRef.current.disconnect()
      processorRef.current.onaudioprocess = null
      processorRef.current = null
    }
    if (sourceRef.current) {
      sourceRef.current.disconnect()
      sourceRef.current = null
    }
    if (streamRef.current) {
      streamRef.current.getTracks().forEach((track) => track.stop())
      streamRef.current = null
    }
    if (audioCtxRef.current) {
      audioCtxRef.current.close().catch(() => {})
      audioCtxRef.current = null
    }
    pcmBufferRef.current = []
  }, [])

  useEffect(() => () => {
    cleanup()
    timersRef.current.forEach((timer) => window.clearTimeout(timer))
  }, [cleanup])

  // The clock of a recording.
  useEffect(() => {
    if (state !== "recording") return
    const started = Date.now()
    const timer = window.setInterval(() => setSeconds(Math.floor((Date.now() - started) / 1000)), 250)
    return () => window.clearInterval(timer)
  }, [state])

  const start = useCallback(async () => {
    if (stateRef.current !== "idle") return
    const shell = shellVoice()
    if (shell) {
      // The app says when it has started; until then nothing is recorded.
      shellRef.current = shell
      shell.post("start")
      return
    }
    if (!navigator.mediaDevices?.getUserMedia) {
      setState("error")
      setError("This page has no microphone: browsers allow one only over HTTPS. Use the Legato app, or turn on web.tls.")
      later(() => setState("idle"), 5000)
      return
    }
    try {
      const stream = await navigator.mediaDevices.getUserMedia({
        audio: { channelCount: 1, sampleRate: 16000, echoCancellation: true, noiseSuppression: true },
      })
      streamRef.current = stream

      const audioCtx = new AudioContext({ sampleRate: 16000 })
      audioCtxRef.current = audioCtx
      const source = audioCtx.createMediaStreamSource(stream)
      sourceRef.current = source

      // ScriptProcessorNode is deprecated but works everywhere.
      // Buffer size 4096 → ~250ms chunks at 16kHz.
      const processor = audioCtx.createScriptProcessor(4096, 1, 1)
      processorRef.current = processor
      pcmBufferRef.current = []

      processor.onaudioprocess = (event) => {
        const input = event.inputBuffer.getChannelData(0)
        // Convert Float32 [-1, 1] → Int16 [-32768, 32767]
        const pcm = new Int16Array(input.length)
        let sum = 0
        for (let i = 0; i < input.length; i++) {
          const sample = Math.max(-1, Math.min(1, input[i]))
          pcm[i] = sample < 0 ? sample * 0x8000 : sample * 0x7fff
          sum += sample * sample
        }
        pcmBufferRef.current.push(pcm)
        // Speech is quiet next to full scale; scale it up to be seen.
        const level = Math.min(1, Math.sqrt(sum / input.length) * 6)
        setLevels((current) => [...current.slice(-(LEVELS - 1)), level])
      }

      source.connect(processor)
      processor.connect(audioCtx.destination)

      setSeconds(0)
      setLevels([])
      setState("recording")
      setError("")
    } catch (reason) {
      setState("error")
      setError(reason instanceof Error ? reason.message : "Failed to access microphone")
      cleanup()
    }
  }, [cleanup, later])

  // Sends the samples to Legato's server and hands back what it heard.
  const transcribe = useCallback(async (merged: Int16Array) => {
    try {
      const response = await apiFetch(baseUrl, "/api/voice/transcribe", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          agent_id: agentId || "",
          agent_kind: agentKind || "",
          transcription_only: transcriptionOnly,
          pcm: Array.from(new Uint8Array(merged.buffer)),
        }),
      })
      if (!response.ok) throw new Error(`Server returned ${response.status}`)
      const data = await response.json()
      if (data.error) throw new Error(data.error)

      onTranscription?.(data.text)
      setState("sent")
      later(() => setState("idle"), 2000)
    } catch (reason) {
      setState("error")
      setError(reason instanceof Error ? reason.message : "Transcription failed")
      later(() => setState("idle"), 3000)
    }
  }, [agentId, agentKind, baseUrl, later, onTranscription, transcriptionOnly])

  // What the app says about the recording it makes for this page.
  useEffect(() => {
    const hear = (event: Event) => {
      if (!shellRef.current) return
      const message = (event as CustomEvent<ShellVoiceEvent>).detail
      switch (message?.type) {
        case "started":
          setSeconds(0)
          setLevels([])
          setError("")
          setState("recording")
          break
        case "level":
          setLevels((current) => [...current.slice(-(LEVELS - 1)), message.level])
          break
        case "audio":
          shellRef.current = null
          decodeRecording(message.data).then(transcribe, (reason) => {
            setState("error")
            setError(reason instanceof Error ? `Could not read the recording: ${reason.message}` : "Could not read the recording")
            later(() => setState("idle"), 3000)
          })
          break
        case "cancelled":
          shellRef.current = null
          setState("idle")
          break
        case "error":
          shellRef.current = null
          setState("error")
          setError(message.message)
          later(() => setState("idle"), 5000)
          break
      }
    }
    window.addEventListener("legato:shell-voice", hear)
    return () => window.removeEventListener("legato:shell-voice", hear)
  }, [later, transcribe])

  const stop = useCallback(async () => {
    if (stateRef.current !== "recording") return
    setState("transcribing")
    if (shellRef.current) {
      shellRef.current.post("stop")
      return
    }

    const chunks = pcmBufferRef.current
    const merged = new Int16Array(chunks.reduce((sum, chunk) => sum + chunk.length, 0))
    let offset = 0
    for (const chunk of chunks) {
      merged.set(chunk, offset)
      offset += chunk.length
    }
    cleanup()
    await transcribe(merged)
  }, [cleanup, transcribe])

  const cancel = useCallback(() => {
    shellRef.current?.post("cancel")
    shellRef.current = null
    cleanup()
    setState("idle")
    setError("")
  }, [cleanup])

  return { state, error, seconds, levels, start, stop, cancel }
}
