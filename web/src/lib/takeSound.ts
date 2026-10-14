// The sound and the picture of a take in the studio, from the moment it is
// started to the moment it is stopped.

// TakePart is the sound of a take, or its film.
export interface TakePart {
  start(): void
  // stop ends the part and gives it as a file. A second call gives the same.
  stop(): Promise<Blob>
}

// Heard is what a take's samples say about the microphone that gave them.
export interface Heard {
  // How much sound has come, in seconds.
  seconds: number
  // False while every sample has been exactly nothing: a microphone that is
  // muted, or not there.
  loud: boolean
  // How long the microphone has now been giving exactly nothing.
  silentMs: number
  // How much of the sound is exactly nothing, in all. A sound passes through
  // nothing many times a second; only stretches too long to be that count.
  silentTotalMs: number
  // The longest stretch of exactly nothing, when it is long enough to be a
  // microphone that stopped and not a pause: a room is never that quiet.
  silence: { atMs: number; ms: number } | null
}

export interface TakeSound extends TakePart {
  heard(): Heard
}

// A stretch of nothing shorter than this is not reported.
const STOPPED_MS = 3000
// A stretch of nothing shorter than this is a sound passing through it.
const NOTHING_MS = 100
// How long the audio thread is given to hand over the last of a take.
const LAST_WORD_MS = 3000

// TAKE_SOUND is run by the browser on its audio thread. The sound of a take
// is taken from the microphone there, as samples. Every sample is counted
// from "start" to "stop", and "done" is answered only after the last has
// been handed over, so a page that is busy makes a take slow to finish and
// nothing else. A microphone that stops gives samples of exactly nothing,
// which take up their time and can be seen. A browser's own recorder, fed
// a microphone that came and went because another page had it, wrote a file
// that was whole and simply ended early, and gave no sign of why.
const TAKE_SOUND = `
class TakeSound extends AudioWorkletProcessor {
  constructor() {
    super();
    this.on = false;
    this.held = new Float32Array(4800);
    this.count = 0;
    this.port.onmessage = (e) => {
      if (e.data === "start") this.on = true;
      if (e.data === "stop") { this.on = false; this.hand(); this.port.postMessage("done"); }
    };
  }
  hand() {
    if (this.count) this.port.postMessage(this.held.slice(0, this.count));
    this.count = 0;
  }
  process(inputs) {
    if (!this.on) return true;
    // A microphone that gives nothing for a while still takes up the time:
    // the take is silent there, not shorter.
    const from = inputs[0][0];
    for (let i = 0; i < 128; i++) {
      this.held[this.count++] = from ? from[i] : 0;
      if (this.count === this.held.length) this.hand();
    }
    return true;
  }
}
registerProcessor("take-sound", TakeSound);
`

// samplesOf takes the sound of a stream as samples, from start() to stop(),
// which gives them as a WAV file of one channel.
export async function samplesOf(context: AudioContext, stream: MediaStream): Promise<TakeSound> {
  if (context.state !== "running") await context.resume()
  const code = URL.createObjectURL(new Blob([TAKE_SOUND], { type: "application/javascript" }))
  try { await context.audioWorklet.addModule(code) } finally { URL.revokeObjectURL(code) }
  const node = new AudioWorkletNode(context, "take-sound", { numberOfInputs: 1, numberOfOutputs: 1, channelCount: 1, channelCountMode: "explicit" })
  const source = context.createMediaStreamSource(stream)
  source.connect(node)
  // It writes nothing, so nothing is heard. A browser may not run a part
  // that leads nowhere.
  node.connect(context.destination)

  const rate = context.sampleRate
  const chunks: Int16Array[] = []
  let frames = 0
  let loud = false
  let silent = 0 // the samples of nothing that the sound now ends with
  let longest = { at: 0, frames: 0 }
  let nothing = 0 // the samples of nothing in stretches that have ended
  const counts = (run: number) => (run >= (NOTHING_MS * rate) / 1000 ? run : 0)
  let done = () => {}
  const ended = new Promise<void>((resolve) => { done = resolve })
  node.port.onmessage = (event: MessageEvent) => {
    if (event.data === "done") return done()
    const samples = event.data as Float32Array
    const part = new Int16Array(samples.length)
    for (let i = 0; i < samples.length; i++) {
      const v = Math.max(-1, Math.min(1, samples[i]))
      part[i] = v < 0 ? v * 0x8000 : v * 0x7fff
      if (v === 0) {
        silent++
        if (silent > longest.frames) longest = { at: frames + i + 1 - silent, frames: silent }
      } else {
        nothing += counts(silent)
        silent = 0
        loud = true
      }
    }
    chunks.push(part)
    frames += part.length
  }

  const ms = (count: number) => Math.round((count / rate) * 1000)
  let stopped: Promise<Blob> | null = null
  return {
    start: () => node.port.postMessage("start"),
    stop: () => {
      stopped ??= (async () => {
        node.port.postMessage("stop")
        // An audio thread that has died answers nothing; what came is kept.
        await Promise.race([ended, new Promise((resolve) => setTimeout(resolve, LAST_WORD_MS))])
        source.disconnect()
        node.disconnect()
        const head = new DataView(new ArrayBuffer(44))
        const text = (at: number, word: string) => { for (let i = 0; i < word.length; i++) head.setUint8(at + i, word.charCodeAt(i)) }
        text(0, "RIFF"); head.setUint32(4, 36 + frames * 2, true); text(8, "WAVE")
        text(12, "fmt "); head.setUint32(16, 16, true); head.setUint16(20, 1, true); head.setUint16(22, 1, true)
        head.setUint32(24, rate, true); head.setUint32(28, rate * 2, true); head.setUint16(32, 2, true); head.setUint16(34, 16, true)
        text(36, "data"); head.setUint32(40, frames * 2, true)
        return new Blob([head, ...chunks.map((part) => part.buffer as ArrayBuffer)], { type: "audio/wav" })
      })()
      return stopped
    },
    heard: () => ({
      seconds: frames / rate,
      loud,
      silentMs: ms(silent),
      silentTotalMs: ms(nothing + counts(silent)),
      silence: ms(longest.frames) >= STOPPED_MS ? { atMs: ms(longest.at), ms: ms(longest.frames) } : null,
    }),
  }
}

// recordingOf records a stream with the browser's recorder, from start() to
// stop(), which gives the recording. It is how the camera is recorded, and
// the sound in a browser that cannot give samples.
export function recordingOf(stream: MediaStream, kinds: string[], bits: { audioBitsPerSecond?: number; videoBitsPerSecond?: number }): TakePart {
  const recorder = new MediaRecorder(stream, { mimeType: kinds.find((kind) => MediaRecorder.isTypeSupported(kind)), ...bits })
  const parts: Blob[] = []
  let done = () => {}
  const ended = new Promise<void>((resolve) => { done = resolve })
  recorder.ondataavailable = (event) => { if (event.data.size > 0) parts.push(event.data) }
  recorder.onstop = () => done()
  return {
    // Handed over a second at a time, so a recorder that fails part way
    // still leaves what it had.
    start: () => recorder.start(1000),
    stop: async () => {
      if (recorder.state === "inactive") done() // it stopped by itself, or never began
      else recorder.stop()
      await ended
      return new Blob(parts, { type: recorder.mimeType })
    },
  }
}
