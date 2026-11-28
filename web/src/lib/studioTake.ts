import { clock, studioPace } from "./agentStudio"
import { recordingOf, samplesOf, type Heard, type TakePart, type TakeSound } from "./takeSound"

// One take in the studio, from the microphone opening to the take being
// kept or refused, apart from the panel that draws it.

// A take shorter than this cannot hold a word: it is a slip of the hand.
export const MIN_TAKE_MS = 1000
// A recording smaller than this holds no sound: it is the start of a file
// and nothing after it, which a recorder gives when its microphone gave it
// nothing.
export const MIN_TAKE_BYTES = 2000
// A microphone that has given exactly nothing for this long has likely
// stopped, and the person is told while the take can still be thrown away.
export const QUIET_TOLD_MS = 3000
// A take's sound may be this much shorter than the take before it is said
// to be: the take runs on a moment after Stop, and starts a moment after
// Record.
const SHORT_TOLD_MS = 500

export const DEAD_MICROPHONE = "The microphone is giving no sound, so the take was not started. Another page or program may have it: close that, then record again. Test, in the settings, shows whether it hears you."

// quietNote says how much of a take the microphone gave nothing for. A
// microphone that another page has gives sound in short bursts, so the whole
// of it is said, and not only the longest stretch.
export function quietNote(heard: Heard, lastedMs: number): string {
  const secs = (ms: number) => (ms / 1000).toFixed(1)
  const where = !heard.silence ? ", in short stretches"
    : heard.silence.ms >= heard.silentTotalMs ? `, from ${clock(heard.silence.atMs)} into it`
      : `, ${secs(heard.silence.ms)} s of it in one stretch from ${clock(heard.silence.atMs)} into it`
  return `The microphone gave nothing at all for ${secs(heard.silentTotalMs)} s of this ${secs(lastedMs)} s take${where}. If you were speaking then, another page or program may have had the microphone: close it, and record the take again.`
}

export interface FinishedTake {
  cancelled: boolean
  lastedMs: number
  // The size of the sound's recording.
  bytes: number
  // What the samples heard, when the sound was taken as samples.
  heard?: Heard
}

// Why a finished take is not kept, or "" when it is.
export function takeRefusal({ cancelled, lastedMs, bytes, heard }: FinishedTake): string {
  if (cancelled) return "That take was thrown away."
  if (lastedMs < MIN_TAKE_MS) return "That take was too short to hold a word, so it was not kept."
  if (bytes < MIN_TAKE_BYTES || (heard && !heard.loud)) return "Nothing was recorded: the microphone gave no sound. Test it in the settings, then record again."
  return ""
}

// What the samples say went wrong with a kept take, for the person to hear
// of before they go on to the next scene, or "".
export function takeWarning(heard: Heard | undefined, lastedMs: number): string {
  if (!heard) return "This browser could not give the take's sound as samples, so its own recorder was used. That recorder can lose the end of a take: listen to it before you go on."
  if (lastedMs - heard.seconds * 1000 > SHORT_TOLD_MS) return `Only ${heard.seconds.toFixed(1)} s of sound came from a take of ${(lastedMs / 1000).toFixed(1)} s, so some of what you said is missing. Listen to it, and record it again if it is.`
  return heard.silentTotalMs >= QUIET_TOLD_MS ? quietNote(heard, lastedMs) : ""
}

// A camera that gave nothing leaves a recording too small to be one; the
// take is then the sound alone.
export function keptPicture(filmed: Blob | null): Blob | undefined {
  return filmed && filmed.size >= MIN_TAKE_BYTES ? filmed : undefined
}

// The sound is taken from the microphone as samples, which show what the
// microphone gave. A browser's recorder hides a microphone that came and
// went, so it takes the sound only where samples cannot be had.
export async function takeSoundOf(context: AudioContext | undefined, got: MediaStream): Promise<{ sound: TakePart; samples: TakeSound | null }> {
  try {
    if (!context) throw new Error("this browser has no sound system for a page")
    const samples = await samplesOf(context, got)
    return { sound: samples, samples }
  } catch {
    return { sound: recordingOf(new MediaStream(got.getAudioTracks()), ["audio/webm;codecs=opus", "audio/webm", "audio/mp4"], { audioBitsPerSecond: 128000 }), samples: null }
  }
}

// The camera is recorded by itself, with no sound in the recording: a
// recorder given both waits for one before it writes the other.
export function takeFilmOf(got: MediaStream): TakePart | null {
  if (got.getVideoTracks().length === 0) return null
  return recordingOf(new MediaStream(got.getVideoTracks()), ["video/webm;codecs=vp9", "video/webm;codecs=vp8", "video/webm", "video/mp4"], { videoBitsPerSecond: 2500000 })
}

// A listener on the microphone through the countdown: how often it looked,
// and how often it heard anything at all.
function microphoneListener(listen: AnalyserNode | undefined) {
  const heardNow = new Float32Array(listen ? 256 : 0)
  const counts = { looks: 0, live: 0 }
  const look = () => {
    if (!listen || typeof listen.getFloatTimeDomainData !== "function") return
    listen.getFloatTimeDomainData(heardNow)
    counts.looks++
    if (heardNow.some((v) => v !== 0)) counts.live++
  }
  return { look, counts }
}

// countDown counts three, two, one: time to look up, and time to hear
// whether the microphone gives anything at all before a take is spent on
// it. A microphone that another page has gives exactly nothing, or a burst
// of sound now and then, which a room never does. It says whether the
// microphone is dead; a countdown called off ends early.
export async function countDown(listen: AnalyserNode | undefined, setCounting: (n: number) => void, cancelled: { current: boolean }): Promise<boolean> {
  const { look, counts } = microphoneListener(listen)
  for (let n = 3; n >= 1 && studioPace.countdownMs > 0; n--) {
    setCounting(n)
    for (const until = performance.now() + studioPace.countdownMs / 3; performance.now() < until && !cancelled.current;) {
      await new Promise((resolve) => setTimeout(resolve, 50))
      look()
    }
  }
  look()
  setCounting(0)
  return counts.looks > 0 && counts.live * 2 < counts.looks
}
