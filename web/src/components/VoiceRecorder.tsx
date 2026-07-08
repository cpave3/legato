import { useState, useRef, useCallback } from "react"
import { apiFetch } from "../lib/api"
import { Mic, Loader2, X, Check } from "lucide-react"

interface VoiceRecorderProps {
  agentId: string
  agentKind?: string
  baseUrl: string
}

type RecordingState = "idle" | "recording" | "transcribing" | "sent" | "error"

export function VoiceRecorder({ agentId, agentKind, baseUrl }: VoiceRecorderProps) {
  const [state, setState] = useState<RecordingState>("idle")
  const [errorMsg, setErrorMsg] = useState("")
  const audioCtxRef = useRef<AudioContext | null>(null)
  const streamRef = useRef<MediaStream | null>(null)
  const processorRef = useRef<ScriptProcessorNode | null>(null)
  const sourceRef = useRef<MediaStreamAudioSourceNode | null>(null)
  const pcmBufferRef = useRef<Int16Array[]>([])

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
      streamRef.current.getTracks().forEach((t) => t.stop())
      streamRef.current = null
    }
    if (audioCtxRef.current) {
      audioCtxRef.current.close().catch(() => {})
      audioCtxRef.current = null
    }
    pcmBufferRef.current = []
  }, [])

  const startRecording = useCallback(async () => {
    try {
      const stream = await navigator.mediaDevices.getUserMedia({
        audio: {
          channelCount: 1,
          sampleRate: 16000,
          echoCancellation: true,
          noiseSuppression: true,
        },
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

      processor.onaudioprocess = (e) => {
        const input = e.inputBuffer.getChannelData(0)
        // Convert Float32 [-1, 1] → Int16 [-32768, 32767]
        const pcm = new Int16Array(input.length)
        for (let i = 0; i < input.length; i++) {
          const s = Math.max(-1, Math.min(1, input[i]))
          pcm[i] = s < 0 ? s * 0x8000 : s * 0x7fff
        }
        pcmBufferRef.current.push(pcm)
      }

      source.connect(processor)
      processor.connect(audioCtx.destination)

      setState("recording")
      setErrorMsg("")
    } catch (err) {
      setState("error")
      setErrorMsg(err instanceof Error ? err.message : "Failed to access microphone")
      cleanup()
    }
  }, [cleanup])

  const stopAndSend = useCallback(async () => {
    setState("transcribing")

    // Merge all PCM chunks into a single buffer.
    const chunks = pcmBufferRef.current
    const totalLen = chunks.reduce((sum, c) => sum + c.length, 0)
    const merged = new Int16Array(totalLen)
    let offset = 0
    for (const chunk of chunks) {
      merged.set(chunk, offset)
      offset += chunk.length
    }

    cleanup()

    // Send raw PCM bytes as a JSON array to the server.
    const bytes = new Uint8Array(merged.buffer)

    try {
      const res = await apiFetch(baseUrl, "/api/voice/transcribe", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          agent_id: agentId,
          agent_kind: agentKind || "",
          pcm: Array.from(bytes),
        }),
      })

      if (!res.ok && res.status !== 200) {
        throw new Error(`Server returned ${res.status}`)
      }

      const data = await res.json()
      if (data.error) {
        throw new Error(data.error)
      }

      setState("sent")
      setTimeout(() => setState("idle"), 2000)
    } catch (err) {
      setState("error")
      setErrorMsg(err instanceof Error ? err.message : "Transcription failed")
      setTimeout(() => setState("idle"), 3000)
    }
  }, [agentId, agentKind, baseUrl, cleanup])

  const cancel = useCallback(() => {
    cleanup()
    setState("idle")
    setErrorMsg("")
  }, [cleanup])

  if (state === "idle") {
    return (
      <button
        onClick={startRecording}
        className="rounded p-1.5 text-zinc-400 border border-zinc-700 transition-colors hover:bg-zinc-800 hover:text-zinc-200"
        title="Voice dictation"
      >
        <Mic size={16} />
      </button>
    )
  }

  if (state === "recording") {
    return (
      <div className="flex items-center gap-1.5">
        <button
          onClick={stopAndSend}
          className="flex items-center gap-1 rounded px-2 py-1.5 text-xs text-red-400 border border-red-900 transition-colors hover:bg-red-950 hover:text-red-300 animate-pulse"
          title="Click to transcribe and send"
        >
          <span className="inline-block w-2 h-2 rounded-full bg-red-500" />
          <span>Send</span>
        </button>
        <button
          onClick={cancel}
          className="rounded p-1.5 text-zinc-500 border border-zinc-700 transition-colors hover:bg-zinc-800 hover:text-zinc-300"
          title="Cancel recording"
        >
          <X size={16} />
        </button>
      </div>
    )
  }

  if (state === "transcribing") {
    return (
      <div className="flex items-center gap-1 rounded px-2 py-1.5 text-xs text-indigo-400 border border-indigo-900">
        <Loader2 size={16} className="animate-spin" />
        <span>Transcribing...</span>
      </div>
    )
  }

  if (state === "sent") {
    return (
      <div className="flex items-center gap-1 rounded px-2 py-1.5 text-xs text-emerald-400 border border-emerald-900">
        <Check size={16} />
        <span>Sent</span>
      </div>
    )
  }

  // error
  return (
    <div
      className="flex items-center gap-1 rounded px-2 py-1.5 text-xs text-red-400 border border-red-900 cursor-help"
      title={errorMsg}
      onClick={() => setState("idle")}
    >
      <X size={16} />
      <span>Error</span>
    </div>
  )
}
