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
  }, [cleanup])

  const stop = useCallback(async () => {
    if (stateRef.current !== "recording") return
    setState("transcribing")

    const chunks = pcmBufferRef.current
    const merged = new Int16Array(chunks.reduce((sum, chunk) => sum + chunk.length, 0))
    let offset = 0
    for (const chunk of chunks) {
      merged.set(chunk, offset)
      offset += chunk.length
    }
    cleanup()

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
  }, [agentId, agentKind, baseUrl, cleanup, later, onTranscription, transcriptionOnly])

  const cancel = useCallback(() => {
    cleanup()
    setState("idle")
    setError("")
  }, [cleanup])

  return { state, error, seconds, levels, start, stop, cancel }
}
