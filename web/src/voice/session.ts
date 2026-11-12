// The voice session: a spoken conversation with the main's Reeve over
// GET /api/reeve/voice/session. Text frames are JSON; binary frames are the
// microphone, 16 kHz mono 16-bit PCM. The protocol is pinned by the shared
// fixtures in internal/server/testdata/voice-session/ (docs/ai/reeve.md §
// The voice session).

export type VoiceState = "idle" | "listening" | "thinking" | "speaking"

// A message from the main.
export interface VoiceOut {
  type: "state" | "pause" | "partial" | "heard" | "activity" | "line" | "written" | "ended"
  state?: VoiceState
  reason?: string
  text?: string
  ms?: number
  of?: number
  id?: string
  url?: string
}

export interface VoiceLine {
  id: string
  text: string
  url: string
}

// What a widget draws.
export interface VoiceView {
  state: VoiceState
  // Words so far while listening; what was sent; what the Reeve is doing.
  partial: string
  heard: string
  activity: string
  // Lines still to play, the first playing; speaking is the one playing.
  lines: VoiceLine[]
  speaking: string
  // The pause bar, 0 to 1, and the microphone's level, 0 to 1.
  pause: number
  level: number
  // Why the session went idle, and whether the Reeve answered in writing.
  reason: string
  written: boolean
  // Why the session ended, "" while it is open.
  ended: string
}

export const initialVoiceView: VoiceView = {
  state: "idle", partial: "", heard: "", activity: "", lines: [], speaking: "",
  pause: 0, level: 0, reason: "", written: false, ended: "",
}

// reduceVoice applies one message from the main to the view.
export function reduceVoice(view: VoiceView, msg: VoiceOut): VoiceView {
  switch (msg.type) {
    case "state": {
      const state = msg.state ?? "idle"
      const next: VoiceView = { ...view, state, reason: msg.reason ?? "", pause: 0 }
      if (state === "listening") {
        Object.assign(next, { partial: "", written: false })
        if (view.state !== "listening") Object.assign(next, { heard: "", activity: "" })
      }
      return next
    }
    case "pause":
      return { ...view, pause: msg.of ? Math.min(1, (msg.ms ?? 0) / msg.of) : 0 }
    case "partial":
      return { ...view, partial: msg.text ?? "" }
    case "heard":
      return { ...view, heard: msg.text ?? "", partial: "", activity: "" }
    case "activity":
      return { ...view, activity: msg.text ?? "" }
    case "line":
      if (!msg.id || !msg.url) return view
      return { ...view, lines: [...view.lines, { id: msg.id, text: msg.text ?? "", url: msg.url }] }
    case "written":
      return { ...view, written: true }
    case "ended":
      return { ...view, ended: msg.reason || "ended", state: "idle", lines: [], speaking: "" }
  }
  return view
}

// AudioPort is where the microphone comes from and lines play: the
// browser, or the Android app's shell.
export interface AudioPort {
  // canPlay: whether lines can be played here.
  readonly canPlay: boolean
  // startCapture streams the microphone as 16 kHz mono 16-bit PCM frames,
  // and calls onStopped if the stream stops by itself (the app lost the
  // microphone or the audio focus).
  startCapture(onFrame: (pcm: ArrayBuffer, level: number) => void, onStopped?: () => void): Promise<void>
  stopCapture(): void
  // play plays a line's audio (a URL on the main) and resolves when it ends
  // or is stopped.
  play(url: string): Promise<void>
  stopPlayback(): void
}

// The part of a WebSocket the client uses.
export interface SocketLike {
  binaryType: string
  readyState: number
  onopen: (() => void) | null
  onmessage: ((ev: { data: unknown }) => void) | null
  onclose: (() => void) | null
  onerror: (() => void) | null
  send(data: string | ArrayBuffer): void
  close(): void
}

export interface VoiceClientOptions {
  url: string
  device: string
  client: "web" | "android" | "desktop"
  audio: AudioPort
  onView: (view: VoiceView) => void
  // socket opens the connection; a WebSocket by default.
  socket?: (url: string) => SocketLike
  // How long the microphone stays muted after a line, so the end of the
  // Reeve's voice is not heard as the person's (300 ms).
  muteAfterMs?: number
}

// VoiceSessionClient holds one device's conversation: it says hello,
// streams the microphone while the main listens, plays the Reeve's lines
// in order with the microphone muted, and reports each.
export class VoiceSessionClient {
  view: VoiceView = initialVoiceView
  private socket: SocketLike | null = null
  private capturing = false
  private playing = false
  private mutedUntil = 0
  // After bye the page has moved on: nothing more is told to it.
  private disposed = false
  private opts: VoiceClientOptions

  constructor(opts: VoiceClientOptions) {
    this.opts = opts
  }

  // connect opens the session and says hello; it resolves once open, and
  // rejects when the connection fails first.
  connect(): Promise<void> {
    const open = this.opts.socket ?? ((url: string) => new WebSocket(url) as unknown as SocketLike)
    const socket = open(this.opts.url)
    socket.binaryType = "arraybuffer"
    this.socket = socket
    let opened: () => void = () => {}
    let failed: (err: Error) => void = () => {}
    const ready = new Promise<void>((resolve, reject) => { opened = resolve; failed = reject })
    socket.onerror = () => failed(new Error("The voice session could not connect"))
    socket.onopen = () => {
      this.sendJSON({ type: "hello", device: this.opts.device, client: this.opts.client, play: this.opts.audio.canPlay })
      opened()
    }
    socket.onmessage = (ev) => {
      if (typeof ev.data !== "string") return
      try {
        this.receive(JSON.parse(ev.data) as VoiceOut)
      } catch {
        // Not a message of the protocol.
      }
    }
    socket.onclose = () => {
      failed(new Error("The voice session closed"))
      if (!this.view.ended) this.update({ ...this.view, ended: "closed", state: "idle" })
      this.release()
    }
    return ready
  }

  // listen starts a turn: the key, the button, or Reply.
  async listen() {
    this.sendJSON({ type: "listen" })
    this.update({ ...reduceVoice(this.view, { type: "state", state: "listening" }) })
    try {
      await this.capture()
    } catch (err) {
      this.sendJSON({ type: "cancel" })
      throw err
    }
  }

  // capture starts the microphone unless it runs.
  private async capture() {
    if (this.capturing) return
    this.capturing = true
    try {
      await this.opts.audio.startCapture((pcm, level) => this.frame(pcm, level), () => this.lostMicrophone())
    } catch (err) {
      this.capturing = false
      throw err
    }
  }

  send() { this.sendJSON({ type: "send" }) }
  cancel() { this.sendJSON({ type: "cancel" }) }

  // stop drops the lines left and stops playback; while the Reeve thinks,
  // the main interrupts it.
  stop() {
    this.sendJSON({ type: "stop" })
    this.dropLines()
  }

  // reply stops playback and listens.
  async reply() {
    this.dropLines()
    await this.listen()
  }

  bye() {
    this.sendJSON({ type: "bye" })
    this.disposed = true
    this.release()
    this.socket?.close()
  }

  private receive(msg: VoiceOut) {
    if (this.disposed) return
    const before = this.view
    const next = reduceVoice(before, msg)
    this.update(next)
    if (msg.type === "line") void this.playNext()
    // The main listens again (the follow-up): the microphone must be on,
    // though the app may have stopped it while a line played.
    if (msg.type === "state" && msg.state === "listening" && !this.capturing && !this.view.ended) {
      this.capture().catch(() => this.sendJSON({ type: "cancel" }))
    }
    if (msg.type === "ended") this.release()
  }

  // lostMicrophone: the stream stopped by itself. While a line plays the
  // app may have given the audio focus to it, and the microphone starts
  // again when the main listens; while listening the turn cannot go on.
  private lostMicrophone() {
    this.capturing = false
    if (this.view.state === "listening") {
      this.sendJSON({ type: "cancel" })
      this.update({ ...this.view, state: "idle", reason: "mic_lost", pause: 0 })
    }
  }

  private frame(pcm: ArrayBuffer, level: number) {
    this.update({ ...this.view, level })
    if (this.view.state !== "listening" && this.view.state !== "thinking") return
    if (this.playing || Date.now() < this.mutedUntil) return
    if (this.socket?.readyState === 1) this.socket.send(pcm)
  }

  private async playNext() {
    if (this.playing) return
    const line = this.view.lines[0]
    if (!line) return
    this.playing = true
    this.update({ ...this.view, speaking: line.id })
    this.sendJSON({ type: "playing", line: line.id })
    try {
      await this.opts.audio.play(line.url)
    } catch {
      // A line that cannot play is skipped, as one that ended.
    }
    this.playing = false
    this.mutedUntil = Date.now() + (this.opts.muteAfterMs ?? 300)
    this.sendJSON({ type: "played", line: line.id })
    this.update({ ...this.view, speaking: "", lines: this.view.lines.filter((l) => l.id !== line.id) })
    void this.playNext()
  }

  private dropLines() {
    this.update({ ...this.view, lines: [], speaking: "" })
    this.opts.audio.stopPlayback()
  }

  private release() {
    if (this.capturing) {
      this.capturing = false
      this.opts.audio.stopCapture()
    }
    this.opts.audio.stopPlayback()
  }

  private sendJSON(msg: Record<string, unknown>) {
    if (this.socket?.readyState === 1) this.socket.send(JSON.stringify(msg))
  }

  private update(view: VoiceView) {
    if (this.disposed) return
    this.view = view
    this.opts.onView(view)
  }
}
