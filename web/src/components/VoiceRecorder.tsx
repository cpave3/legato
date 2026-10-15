import { forwardRef, useImperativeHandle } from "react"
import { Mic, Loader2, X, Check } from "lucide-react"
import { Button } from "./ui/Button"
import { useVoiceRecorder } from "../hooks/useVoiceRecorder"

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

// VoiceRecorder is a button that dictates into a field: press to record,
// press again to transcribe.
export const VoiceRecorder = forwardRef<VoiceRecorderHandle, VoiceRecorderProps>(function VoiceRecorder({ agentId, agentKind, baseUrl, transcriptionOnly = false, onTranscription }, ref) {
  const recorder = useVoiceRecorder({ agentId, agentKind, baseUrl, transcriptionOnly, onTranscription })
  const { state, error: errorMsg } = recorder
  const startRecording = recorder.start
  const stopAndSend = recorder.stop
  const cancel = recorder.cancel

  useImperativeHandle(ref, () => ({
    start: () => { void startRecording() },
    send: () => { void stopAndSend() },
    cancel,
    isRecording: () => state === "recording",
  }), [startRecording, stopAndSend, cancel, state])

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
      onClick={cancel}
    >
      <X size={16} />
      <span>Error</span>
    </div>
  )
})
