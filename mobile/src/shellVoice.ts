import { useCallback, useEffect, useRef } from "react"
import { requestRecordingPermissionsAsync, setAudioModeAsync, useAudioRecorder, type RecordingOptions } from "expo-audio"
import * as FileSystem from "expo-file-system/legacy"

// What the shell tells the page about a recording. The page turns the audio
// into the samples Legato's server transcribes.
export type ShellVoiceEvent =
  | { type: "started" }
  | { type: "level"; level: number }
  | { type: "audio"; data: string; mime: string }
  | { type: "cancelled" }
  | { type: "error"; message: string }

// Speech, small: one channel at the rate the transcriber wants.
const options: RecordingOptions = {
  extension: ".m4a",
  sampleRate: 16000,
  numberOfChannels: 1,
  bitRate: 64000,
  isMeteringEnabled: true,
  android: { outputFormat: "mpeg4", audioEncoder: "aac" },
  ios: { outputFormat: "aac ", audioQuality: 64 },
  web: { mimeType: "audio/webm" },
}

const LEVEL_EVERY_MS = 150

// useShellVoice records the microphone for the page. A page served over plain
// HTTP has no microphone of its own, so the app records in its place: the
// page asks to start, stop or cancel, and hears back through `tell`.
export function useShellVoice(tell: (event: ShellVoiceEvent) => void) {
  const recorder = useAudioRecorder(options)
  const recording = useRef(false)
  const meter = useRef<ReturnType<typeof setInterval> | undefined>(undefined)

  const stopMeter = () => {
    clearInterval(meter.current)
    meter.current = undefined
  }

  const finish = useCallback(async () => {
    stopMeter()
    recording.current = false
    await recorder.stop()
    await setAudioModeAsync({ allowsRecording: false }).catch(() => {})
    return recorder.uri
  }, [recorder])

  const start = useCallback(async () => {
    if (recording.current) return
    const permission = await requestRecordingPermissionsAsync()
    if (!permission.granted) {
      tell({ type: "error", message: "Legato may not use the microphone. Allow it in the app's settings." })
      return
    }
    await setAudioModeAsync({ allowsRecording: true, playsInSilentMode: true })
    await recorder.prepareToRecordAsync()
    recorder.record()
    recording.current = true
    tell({ type: "started" })
    meter.current = setInterval(() => {
      // Metering is in decibels below full scale; speech sits well above -50.
      const db = recorder.getStatus().metering
      if (typeof db === "number") tell({ type: "level", level: Math.max(0, Math.min(1, (db + 50) / 50)) })
    }, LEVEL_EVERY_MS)
  }, [recorder, tell])

  const stop = useCallback(async () => {
    if (!recording.current) return
    const uri = await finish()
    if (!uri) { tell({ type: "error", message: "The recording was not saved." }); return }
    const data = await FileSystem.readAsStringAsync(uri, { encoding: FileSystem.EncodingType.Base64 })
    FileSystem.deleteAsync(uri, { idempotent: true }).catch(() => {})
    tell({ type: "audio", data, mime: "audio/mp4" })
  }, [finish, tell])

  const cancel = useCallback(async () => {
    if (!recording.current) return
    const uri = await finish()
    if (uri) FileSystem.deleteAsync(uri, { idempotent: true }).catch(() => {})
    tell({ type: "cancelled" })
  }, [finish, tell])

  // A recording does not outlive the page that asked for it.
  useEffect(() => () => { stopMeter(); if (recording.current) { recording.current = false; void recorder.stop().catch(() => {}) } }, [recorder])

  return useCallback((action: unknown) => {
    const run = action === "start" ? start : action === "stop" ? stop : action === "cancel" ? cancel : undefined
    run?.().catch((reason: unknown) => {
      stopMeter()
      recording.current = false
      tell({ type: "error", message: reason instanceof Error ? reason.message : String(reason) })
    })
  }, [start, stop, cancel, tell])
}
