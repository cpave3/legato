// What a voice recording gives the box it records for: its state, how long
// and how loud it has been, and the controls. useVoiceRecorder makes one.
export type RecordingState = "idle" | "recording" | "transcribing" | "sent" | "error"

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
