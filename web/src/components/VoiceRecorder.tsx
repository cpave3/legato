import { useState, useRef, useCallback, forwardRef, useImperativeHandle } from "react"
import { apiFetch } from "../lib/api"
import { Mic, Loader2, X, Check } from "lucide-react"
import { Button } from "./ui/Button"

interface VoiceRecorderProps {
  agentId?: string
  agentKind?: string
  baseUrl: string
  transcriptionOnly?: boolean
  onTranscription?: (text: string) => void
}

export interface VoiceRecorderHandle {
  start: () => void
  send: () => void
  cancel: () => void
  isRecording: () => boolean
}

type RecordingState = "idle" | "recording" | "transcribing" | "sent" | "error"

export const VoiceRecorder = forwardRef<VoiceRecorderHandle, VoiceRecorderProps>(function VoiceRecorder({ agentId, agentKind, baseUrl, transcriptionOnly = false, onTranscription }, ref) {
  const [state, setState] = useState<RecordingState>("idle")
  const [errorMsg, setErrorMsg] = useState("")
  const audioCtxRef = useRef<AudioContext | null>(null)
  const streamRef = useRef<MediaStream | null>(null)
  const processorRef = useRef<ScriptProcessorNode | null>(null)
  const sourceRef = useRef<MediaStreamAudioSourceNode | null>(null)
  const pcmBufferRef = useRef<Int16Array[]>([])
  const stateRef = useRef<RecordingState>("idle")
  stateRef.current = state

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
    if (stateRef.current !== "idle") return
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
    if (stateRef.current !== "recording") return
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
          agent_id: agentId || "",
          agent_kind: agentKind || "",
          transcription_only: transcriptionOnly,
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

      onTranscription?.(data.text)
      setState("sent")
      setTimeout(() => setState("idle"), 2000)
    } catch (err) {
      setState("error")
      setErrorMsg(err instanceof Error ? err.message : "Transcription failed")
      setTimeout(() => setState("idle"), 3000)
    }
  }, [agentId, agentKind, baseUrl, cleanup, onTranscription, transcriptionOnly])

  const cancel = useCallback(() => {
    cleanup()
    setState("idle")
    setErrorMsg("")
  }, [cleanup])

  useImperativeHandle(ref, () => ({
    start: () => startRecording(),
    send: () => stopAndSend(),
    cancel,
    isRecording: () => stateRef.current === "recording",
  }), [startRecording, stopAndSend, cancel])

  if (state === "idle") {
    return (
      <Button
        variant="outline"
        size="icon"
        aria-label="Voice dictation"
        onClick={startRecording}
        className="self-center"
        title="Voice dictation (V)"
      >
        <Mic size={16} />
      </Button>
    )
  }

  if (state === "recording") {
    return (
      <div className="flex self-center items-center gap-1.5">
        <Button
          variant="destructive"
          size="sm"
          onClick={stopAndSend}
          className="animate-pulse"
          title={transcriptionOnly ? "Click to add transcription" : "Click to transcribe and send (V)"}
        >
          <span className="size-2 rounded-full bg-red-200" />
          <span>{transcriptionOnly ? "Add" : "Send"}</span>
        </Button>
        <Button
          variant="outline"
          size="icon-sm"
          aria-label="Cancel recording"
          onClick={cancel}
          title="Cancel recording (Esc)"
        >
          <X size={16} />
        </Button>
      </div>
    )
  }

  if (state === "transcribing") {
    return (
      <div className="flex self-center items-center gap-1 rounded border border-indigo-900 px-2 py-1.5 text-xs text-indigo-400">
        <Loader2 size={16} className="animate-spin" />
        <span>Transcribing...</span>
      </div>
    )
  }

  if (state === "sent") {
    return (
      <div className="flex self-center items-center gap-1 rounded border border-emerald-900 px-2 py-1.5 text-xs text-emerald-400">
        <Check size={16} />
        <span>{transcriptionOnly ? "Added" : "Sent"}</span>
      </div>
    )
  }

  // error
  return (
    <div
      className="flex self-center cursor-help items-center gap-1 rounded border border-red-900 px-2 py-1.5 text-xs text-red-400"
      title={errorMsg}
      onClick={() => setState("idle")}
    >
      <X size={16} />
      <span>Error</span>
    </div>
  )
})
