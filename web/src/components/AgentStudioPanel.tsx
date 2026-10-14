import { Fragment, useCallback, useEffect, useMemo, useRef, useState } from "react"
import { ArrowDown, ArrowUp, ChevronLeft, ChevronRight, Circle, MessageCircleQuestion, Pause, Play, Settings2, Sparkles, Square, Trash2, X } from "lucide-react"
import "@fontsource/geist-sans/400.css"
import "@fontsource/geist-sans/500.css"
import "@fontsource/geist-sans/600.css"
import "@fontsource/geist-mono/400.css"
import { useServer } from "../hooks/useServer"
import { proposeComposerText, type AgentUIItem } from "../lib/agentUi"
import {
  StudioError, addScene, agreePlan, chapterHeadings, chooseTake, clock, commentOnScene, decideOffPlan, lengthOf, moveScene, planLength, renameChapter, rewriteScene, writeBrief, fetchStudioFile, fetchStudioProject, focusedScene, intoTake, leftTheScript, lineUpRequest,
  openOffPlan, planChanges, pointsHit, questionAboutScene, recordedCount, removeScene, reviewRequest, sceneSignature, startRender, studioPace, takeBackComment, talkingPoints,
  timeline, transcriptRuns, uploadTake, type StudioProject, type StudioScene,
} from "../lib/agentStudio"
import { recordingOf, samplesOf, type Heard, type TakePart, type TakeSound } from "../lib/takeSound"

interface AgentStudioPanelProps {
  item: AgentUIItem
  // fill: take the container's height (the side panel).
  fill?: boolean
  // How often the storyboard is read again, in milliseconds. The agent edits
  // its file while the panel is open, and that is how the panel comes to
  // show it. A test asks more often.
  pollMs?: number
  // How long a test keeps the microphone and the camera open, in
  // milliseconds. A test asks for less.
  testHoldMs?: number
}

// A studio that cannot open is asked for less often: each asking runs
// libretto on the server.
const FAILED_POLLS = 3
// A take shorter than this cannot hold a word: it is a slip of the hand.
const MIN_TAKE_MS = 1000
// A recording smaller than this holds no sound: it is the start of a file
// and nothing after it, which a recorder gives when its microphone gave it
// nothing.
const MIN_TAKE_BYTES = 2000
// How long a take runs on after Stop is pressed. The sound of the last word
// is still on its way from the microphone when the person presses.
const STOP_TAIL_MS = 350
// A microphone that has given exactly nothing for this long has likely
// stopped, and the person is told while the take can still be thrown away.
const QUIET_TOLD_MS = 3000
// A take's sound may be this much shorter than the take before it is said
// to be: the take runs on a moment after Stop, and starts a moment after
// Record.
const SHORT_TOLD_MS = 500
const NO_CAMERA = "none"

const STATUS: Record<StudioScene["status"], { dot: string; label: string }> = {
  recorded: { dot: "bg-lw-live", label: "Recorded" },
  waiting: { dot: "border border-lw-faint", label: "Not recorded yet" },
  voice: { dot: "bg-lw-accent", label: "A voice says this unless you record it" },
  silent: { dot: "bg-lw-line-strong", label: "Nothing is said here" },
}

function asStudioError(err: unknown): StudioError {
  if (err instanceof StudioError) return err
  return new StudioError("studio_failed", err instanceof Error ? err.message : String(err))
}

// why says what a refusal of the microphone or the camera most likely means.
function why(err: unknown): string {
  const name = err instanceof Error ? err.name : ""
  if (name === "NotAllowedError") return "the browser or the system did not allow it"
  if (name === "NotFoundError") return "none was found"
  if (name === "NotReadableError" || name === "AbortError") return "another program may be using it"
  if (name === "OverconstrainedError") return "the one chosen is not there now"
  return err instanceof Error ? err.message : String(err)
}

// quietNote says how much of a take the microphone gave nothing for. A
// microphone that another page has gives sound in short bursts, so the whole
// of it is said, and not only the longest stretch.
function quietNote(heard: Heard, lastedMs: number): string {
  const secs = (ms: number) => (ms / 1000).toFixed(1)
  const where = !heard.silence ? ", in short stretches"
    : heard.silence.ms >= heard.silentTotalMs ? `, from ${clock(heard.silence.atMs)} into it`
      : `, ${secs(heard.silence.ms)} s of it in one stretch from ${clock(heard.silence.atMs)} into it`
  return `The microphone gave nothing at all for ${secs(heard.silentTotalMs)} s of this ${secs(lastedMs)} s take${where}. If you were speaking then, another page or program may have had the microphone: close it, and record the take again.`
}

function remembered(key: string): string {
  try { return localStorage.getItem(key) ?? "" } catch { return "" }
}

function remember(key: string, value: string) {
  try { localStorage.setItem(key, value) } catch { /* a private window keeps nothing */ }
}

// AgentStudioPanel is the studio beside the conversation: the plan of a video
// the person will present, and the place they record it. It is a tool the
// person and the agent share, not something the agent made to be read.
//
// The plan is the agent's to keep: it edits the storyboard file as the two
// of them talk, and the panel shows the file as it is now, marking what
// changed. The person does the two things only they can: say what they
// think of the plan, and say the words. Both of the panel's requests to the
// agent go in the person's message box, for them to add to and send.
export function AgentStudioPanel({ item, fill = false, pollMs = 2000, testHoldMs = 20000 }: AgentStudioPanelProps) {
  const { baseUrl } = useServer()
  const [project, setProject] = useState<StudioProject | null>(null)
  const [failure, setFailure] = useState<StudioError | null>(null)
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [phase, setPhase] = useState<"idle" | "opening" | "counting" | "recording" | "stopping" | "saving" | "changing">("idle")
  const [note, setNote] = useState("")
  const [seconds, setSeconds] = useState(0)
  const [level, setLevel] = useState(0)
  // What the agent changed since the person last looked: per scene, and in
  // a sentence.
  const [marks, setMarks] = useState<Record<string, "new" | "changed">>({})
  const [changed, setChanged] = useState("")
  const [settings, setSettings] = useState(false)
  // Whether Legato tells the agent what the person did here: a take to
  // review, a comment to act on. On unless they turned it off.
  const [notify, setNotify] = useState(() => remembered("legato:studio-notify") !== "off")
  // The scene's take, played back: fetched when first asked for. heardMs is
  // where it has got to in the recording, and -1 until it has played.
  const audio = useRef<HTMLAudioElement>(null)
  const heardTake = useRef<{ url: string; address: string } | null>(null)
  const [loadingTake, setLoadingTake] = useState(false)
  const [playing, setPlaying] = useState(false)
  const [heardMs, setHeardMs] = useState(-1)
  // The video as it was last rendered, when the person is watching it.
  const [film, setFilm] = useState<string | null>(null)
  const [watching, setWatching] = useState(false)
  const [loadingFilm, setLoadingFilm] = useState(false)
  // Whether the video's path was just copied, or could not be.
  const [copied, setCopied] = useState<"" | "done" | "failed">("")
  // Which of the two views is shown: the plan, or where it is recorded.
  // Chosen when the storyboard is first read, and the person's from then on.
  const [view, setView] = useState<"plan" | "record" | null>(null)
  // What the person is typing into the plan and has not yet saved: the
  // brief, the scene in front of them, a new scene, a chapter's title.
  // The part of the panel that scrolls. A view opens at its top: the other
  // view is another page, not a place further down this one.
  const scroller = useRef<HTMLDivElement>(null)
  useEffect(() => { if (scroller.current) scroller.current.scrollTop = 0 }, [view])
  const [briefDraft, setBriefDraft] = useState<string | null>(null)
  const [drafts, setDrafts] = useState<{ id: string; points?: string; shows?: string }>({ id: "" })
  const [adding, setAdding] = useState("")
  const [renaming, setRenaming] = useState<{ from: string; to: string } | null>(null)
  const [comment, setComment] = useState("")
  const [removing, setRemoving] = useState(false)
  const [devices, setDevices] = useState<MediaDeviceInfo[]>([])
  const [mic, setMic] = useState(() => remembered("legato:studio-mic"))
  const [cam, setCam] = useState(() => remembered("legato:studio-cam"))
  const [using, setUsing] = useState("")
  const [seen, setSeen] = useState(false)
  const busy = useRef(false)
  const known = useRef<Map<string, string> | null>(null)
  // The scene in front of the person, which a change does not need to mark:
  // they see it change.
  const viewing = useRef<string | null>(null)
  const stream = useRef<MediaStream | null>(null)
  // The take being recorded: its sound, and its film when the camera is on.
  const recorder = useRef<{ finish: () => void } | null>(null)
  const meter = useRef<{ context: AudioContext; analyser: AnalyserNode; timer: ReturnType<typeof setInterval> } | null>(null)
  // The count before a take starts: 3, 2, 1, and 0 when nothing is counting.
  const [counting, setCounting] = useState(0)
  // How long the microphone has now given nothing, while a take is recorded.
  const [quiet, setQuiet] = useState(0)
  // Set when the person cancels the take being recorded: what the recorder
  // hands over is then thrown away.
  const cancelled = useRef(false)
  const self = useRef<HTMLVideoElement>(null)
  const title = item.title || "Studio"

  // take shows the storyboard as it is now, and works out what the agent
  // changed since the last look.
  // own is true for a change the person made here themselves: it is shown,
  // and is not news to them.
  const take = useCallback((next: StudioProject, own = false) => {
    const before = known.current
    known.current = new Map(next.scenes.map((scene) => [scene.id, sceneSignature(scene)]))
    setProject(next)
    setFailure(null)
    // The panel opens on the plan while there is planning to do, and where
    // the recording is done after. It does not change views under the person
    // because the storyboard did.
    if (!before) setView((chosen) => chosen ?? (next.scenes.some((scene) => scene.planned) || recordedCount(next).recorded === 0 ? "plan" : "record"))
    if (!before || own) return // the first look, or their own change: nothing to tell them
    const diff = planChanges(before, next.scenes)
    if (diff.summary) setChanged(diff.summary)
    if (Object.keys(diff.marks).length > 0 || diff.removed.length > 0) {
      setMarks((current) => {
        const merged = { ...current, ...diff.marks }
        for (const id of diff.removed) delete merged[id]
        if (viewing.current) delete merged[viewing.current]
        return merged
      })
    }
  }, [])

  // The storyboard is read again every couple of seconds, except while a
  // take is being recorded or kept: an answer from before the take would
  // undo what the take's own answer showed.
  useEffect(() => {
    let gone = false
    let timer: ReturnType<typeof setTimeout>
    const load = async () => {
      let failed = false
      if (!busy.current) {
        try {
          const next = await fetchStudioProject(baseUrl, item.id)
          if (!gone && !busy.current) take(next)
        } catch (err) {
          failed = true
          if (!gone) setFailure(asStudioError(err))
        }
      }
      if (!gone) timer = setTimeout(load, failed ? pollMs * FAILED_POLLS : pollMs)
    }
    void load()
    return () => { gone = true; clearTimeout(timer) }
  }, [baseUrl, item.id, pollMs, take])

  // What lets a tested microphone go again.
  const tested = useRef<ReturnType<typeof setTimeout> | null>(null)

  const close = useCallback(() => {
    if (tested.current) { clearTimeout(tested.current); tested.current = null }
    if (meter.current) { clearInterval(meter.current.timer); void meter.current.context.close().catch(() => {}); meter.current = null }
    stream.current?.getTracks().forEach((track) => track.stop())
    stream.current = null
    setLevel(0)
    setUsing("")
    setSeen(false)
  }, [])

  // The microphone is let go when the panel is closed.
  useEffect(() => close, [close])

  const listDevices = useCallback(async () => {
    try { setDevices(await navigator.mediaDevices.enumerateDevices()) } catch { /* no list, the defaults still work */ }
  }, [])
  useEffect(() => { void listDevices() }, [listDevices])

  // The agent can put a scene in front of the person by showing the studio
  // again with that scene named. A take in progress is not interrupted.
  const asked = useMemo(() => focusedScene(item.content), [item.content])
  useEffect(() => {
    if (asked && !busy.current) setSelectedId(asked)
  }, [asked, item.revision])

  const scenes = useMemo(() => project?.scenes ?? [], [project])
  // The plan is laid out by chapter: a scene that starts one is headed by it.
  const headings = useMemo(() => chapterHeadings(scenes), [scenes])
  const index = Math.max(0, scenes.findIndex((s) => s.id === selectedId) >= 0
    ? scenes.findIndex((s) => s.id === selectedId)
    : scenes.findIndex((s) => s.status === "waiting"))
  const scene: StudioScene | null = scenes[index] ?? null
  const sceneId = scene?.id ?? null

  // A scene the person is looking at is no longer news.
  useEffect(() => {
    viewing.current = sceneId
    if (!sceneId) return
    setMarks((current) => {
      if (!(sceneId in current)) return current
      const rest = { ...current }
      delete rest[sceneId]
      return rest
    })
    setComment("")
    setRemoving(false)
    // Another scene has another take.
    audio.current?.pause()
    setHeardMs(-1)
    setWatching(false)
  }, [sceneId])

  // What was fetched to be played is given back when the panel goes.
  useEffect(() => () => { if (heardTake.current) URL.revokeObjectURL(heardTake.current.address) }, [])
  useEffect(() => () => { if (film) URL.revokeObjectURL(film) }, [film])

  function go(to: StudioScene | undefined) {
    if (to && phase !== "recording") setSelectedId(to.id)
  }

  // open gets the microphone, and the camera when one is wanted. The camera
  // is what usually fails, because a screen recorder left running has it; a
  // take needs only the sound, so it goes on without.
  async function open(): Promise<MediaStream> {
    close()
    const sound = { echoCancellation: false, ...(mic ? { deviceId: { exact: mic } } : {}) }
    const picture = cam === NO_CAMERA ? false : { width: { ideal: 720 }, height: { ideal: 720 }, ...(cam ? { deviceId: { exact: cam } } : {}) }
    let got: MediaStream
    try {
      got = await navigator.mediaDevices.getUserMedia({ audio: sound, video: picture })
    } catch (err) {
      if (!picture) throw err
      got = await navigator.mediaDevices.getUserMedia({ audio: sound, video: false })
      setNote(`The camera could not be opened (${why(err)}), so this take is sound alone. Close the program that has it, or choose "No camera" in the settings.`)
    }
    stream.current = got
    setUsing(got.getTracks().map((track) => track.label).filter(Boolean).join(" and "))
    setSeen(got.getVideoTracks().length > 0)
    if (self.current) {
      self.current.srcObject = got.getVideoTracks().length > 0 ? got : null
      void self.current.play()?.catch(() => {})
    }
    // The meter shows that the microphone hears something before a take is
    // spent finding out.
    if (typeof AudioContext !== "undefined") {
      const context = new AudioContext()
      const analyser = context.createAnalyser()
      analyser.fftSize = 512
      context.createMediaStreamSource(got).connect(analyser)
      const samples = new Uint8Array(analyser.fftSize)
      const timer = setInterval(() => {
        analyser.getByteTimeDomainData(samples)
        let peak = 0
        for (const v of samples) peak = Math.max(peak, Math.abs(v - 128))
        setLevel(Math.min(100, peak * 1.6))
      }, 100)
      meter.current = { context, analyser, timer }
    }
    void listDevices() // the browser names its devices once it is allowed
    return got
  }

  async function test() {
    if (phase !== "idle") return
    setPhase("opening")
    setNote("")
    try {
      await open()
      // A browser gives every page the same feed from a microphone, and one
      // that has been open a long while runs late: a page that left its
      // microphone open cost every take recorded in another page its last
      // words. So nothing here holds a microphone it is not recording.
      tested.current = setTimeout(close, testHoldMs)
    } catch (err) {
      setNote(`The microphone could not be opened: ${why(err)}.`)
    }
    setPhase("idle")
  }

  async function start() {
    if (phase !== "idle" || !scene) return
    const recorded = scene.id
    // The panel opens on the first scene that waits. Once this one has a
    // take that would be another scene, and the person is still on this one.
    setSelectedId(recorded)
    setPhase("opening")
    setNote("")
    // The microphone is opened for this take and let go after it. A feed
    // from a microphone that has been open a long while runs late: the take
    // begins with sound from before it, and loses the end of what was said.
    let got: MediaStream
    try {
      got = await open()
    } catch (err) {
      setNote(`The microphone could not be opened: ${why(err)}. The microphone is chosen in the settings.`)
      setPhase("idle")
      return
    }
    // The sound is taken from the microphone as samples, which show what
    // the microphone gave. A browser's recorder hides a microphone that came
    // and went, so it takes the sound only where samples cannot be had.
    let sound: TakePart
    let samples: TakeSound | null = null
    try {
      if (!meter.current) throw new Error("this browser has no sound system for a page")
      sound = samples = await samplesOf(meter.current.context, got)
    } catch {
      sound = recordingOf(new MediaStream(got.getAudioTracks()), ["audio/webm;codecs=opus", "audio/webm", "audio/mp4"], { audioBitsPerSecond: 128000 })
    }
    // The camera is recorded by itself, with no sound in the recording: a
    // recorder given both waits for one before it writes the other.
    const film = got.getVideoTracks().length > 0
      ? recordingOf(new MediaStream(got.getVideoTracks()), ["video/webm;codecs=vp9", "video/webm;codecs=vp8", "video/webm", "video/mp4"], { videoBitsPerSecond: 2500000 })
      : null
    // Three, two, one: time to look up, and time to hear whether the
    // microphone gives anything at all before a take is spent on it. A
    // microphone that another page has gives exactly nothing, or a burst of
    // sound now and then, which a room never does.
    cancelled.current = false
    setPhase("counting")
    const listen = meter.current?.analyser
    const heardNow = new Float32Array(listen ? 256 : 0)
    let looks = 0
    let live = 0
    const look = () => {
      if (!listen || typeof listen.getFloatTimeDomainData !== "function") return
      listen.getFloatTimeDomainData(heardNow)
      looks++
      if (heardNow.some((v) => v !== 0)) live++
    }
    for (let n = 3; n >= 1 && studioPace.countdownMs > 0; n--) {
      setCounting(n)
      for (const until = performance.now() + studioPace.countdownMs / 3; performance.now() < until && !cancelled.current;) {
        await new Promise((resolve) => setTimeout(resolve, 50))
        look()
      }
    }
    look()
    setCounting(0)
    const dead = looks > 0 && live * 2 < looks
    if (cancelled.current || dead) {
      close()
      setNote(dead && !cancelled.current ? "The microphone is giving no sound, so the take was not started. Another page or program may have it: close that, then record again. Test, in the settings, shows whether it hears you." : "")
      setPhase("idle")
      return
    }
    const began = Date.now()
    const tick = setInterval(() => {
      setSeconds(Math.floor((Date.now() - began) / 1000))
      setQuiet(samples ? samples.heard().silentMs : 0)
    }, 250)
    let finishing = false
    const finish = async () => {
      if (finishing) return
      finishing = true
      const lasted = Date.now() - began
      const [words, filmed] = await Promise.all([sound.stop(), film ? film.stop() : null])
      clearInterval(tick)
      recorder.current = null
      setQuiet(0)
      close()
      const heard = samples?.heard()
      const refused = cancelled.current ? "That take was thrown away. Nothing was kept."
        : lasted < MIN_TAKE_MS ? "That take was too short to hold a word, so it was not kept."
          : words.size < MIN_TAKE_BYTES || (heard && !heard.loud) ? "Nothing was recorded: the microphone gave no sound, so nothing was kept. Test it in the settings, then record again."
            : ""
      if (refused) {
        setNote(refused)
        busy.current = false
        setPhase("idle")
        return
      }
      setPhase("saving")
      // What the samples say went wrong, for the person to hear of before
      // they go on to the next scene.
      const told = !heard ? "This browser could not give the take's sound as samples, so its own recorder was used. That recorder can lose the end of a take: listen to it before you go on."
        : lasted - heard.seconds * 1000 > SHORT_TOLD_MS ? `Only ${heard.seconds.toFixed(1)} s of sound came from a take of ${(lasted / 1000).toFixed(1)} s, so some of what you said is missing. Listen to it, and record it again if it is.`
          : heard.silentTotalMs >= QUIET_TOLD_MS ? quietNote(heard, lasted)
            : ""
      // A camera that gave nothing leaves a recording too small to be one;
      // the take is then the sound alone, and the studio says so.
      const picture = filmed && filmed.size >= MIN_TAKE_BYTES ? filmed : undefined
      uploadTake(baseUrl, item.id, recorded, words, picture, notify)
        .then((next) => { take(next); if (told) setNote(told) })
        .catch((err) => setNote(asStudioError(err).message))
        .finally(() => { busy.current = false; setPhase("idle") })
    }
    busy.current = true
    recorder.current = { finish: () => void finish() }
    setSeconds(0)
    setQuiet(0)
    sound.start()
    film?.start()
    setPhase("recording")
  }

  function stop() {
    const taking = recorder.current
    if (!taking || phase !== "recording") return
    setPhase("stopping")
    setTimeout(taking.finish, STOP_TAIL_MS)
  }

  // cancel ends a take that went wrong and throws it away: it is not sent to
  // the studio, so nothing is kept and nothing is transcribed, and the scene
  // is as it was. There is no wait for the last word of a take nobody wants.
  function cancel() {
    cancelled.current = true // a take that is still counting down sees this, and does not start
    recorder.current?.finish()
  }

  function use(src: string) {
    if (!scene || phase !== "idle") return
    busy.current = true
    setPhase("saving")
    chooseTake(baseUrl, item.id, scene.id, src)
      .then(take)
      .catch((err) => setNote(asStudioError(err).message))
      .finally(() => { busy.current = false; setPhase("idle") })
  }

  // change sends the studio a change the person made to the plan and shows
  // what the storyboard is after it.
  function change(work: Promise<StudioProject>, done?: () => void) {
    if (phase !== "idle") return
    busy.current = true
    setPhase("changing")
    setNote("")
    work
      .then((next) => { take(next, true); done?.() })
      .catch((err) => setNote(asStudioError(err).message))
      .finally(() => { busy.current = false; setPhase("idle") })
  }

  // play plays the part of the take the scene uses, or pauses it. The
  // recording is fetched the first time.
  async function play() {
    const current = scene?.take
    const player = audio.current
    if (!current || !player) return
    if (playing) { player.pause(); return }
    if (heardTake.current?.url !== current.url) {
      setLoadingTake(true)
      try {
        const address = await fetchStudioFile(baseUrl, item.id, current.url)
        if (heardTake.current) URL.revokeObjectURL(heardTake.current.address)
        heardTake.current = { url: current.url, address }
        player.src = address
        setHeardMs(-1)
      } catch (err) {
        setNote(asStudioError(err).message)
        return
      } finally {
        setLoadingTake(false)
      }
    }
    const from = current.startMs ?? 0
    if (heardMs < from || (current.endMs && heardMs >= current.endMs - 50)) player.currentTime = from / 1000
    setPlaying(true)
    void player.play()?.catch(() => setPlaying(false))
  }

  // heardSoFar follows the take as it plays: it jumps what the scene leaves
  // out, and stops where the scene's part ends.
  function heardSoFar() {
    const current = scene?.take
    const player = audio.current
    if (!current || !player) return
    let ms = player.currentTime * 1000
    for (const [from, to] of current.skip ?? []) {
      if (ms >= from && ms < to) { player.currentTime = to / 1000; ms = to }
    }
    if (current.endMs && ms >= current.endMs) player.pause()
    setHeardMs(ms)
  }

  // watch plays the video as it was last rendered, where the scene is drawn.
  async function watch() {
    if (!project?.render.video) return
    setLoadingFilm(true)
    try {
      setFilm(await fetchStudioFile(baseUrl, item.id, project.render.video))
      setWatching(true)
    } catch (err) {
      setNote(asStudioError(err).message)
    } finally {
      setLoadingFilm(false)
    }
  }

  // draftOf is what a field of the scene's plan shows: what the person is
  // typing, or what the storyboard says.
  function draftOf(which: "points" | "shows"): string {
    const typed = drafts.id === scene?.id ? drafts[which] : undefined
    if (typed !== undefined) return typed
    return which === "points" ? talkingPoints(scene?.notes).join("\n") : scene?.shows ?? ""
  }

  // saveDraft writes what the person typed into the storyboard, when they
  // leave the field and it says something else than before.
  function saveDraft(which: "points" | "shows") {
    if (!scene || drafts.id !== scene.id) return
    const typed = drafts[which]
    if (typed === undefined) return
    const forget = () => setDrafts((now) => ({ ...now, [which]: undefined }))
    const was = which === "points" ? talkingPoints(scene.notes).join("\n") : scene.shows ?? ""
    if (typed.trim() === was.trim()) { forget(); return }
    const lines = typed.split("\n").map((line) => line.trim().replace(/^[-*•]\s*/, "")).filter(Boolean)
    change(rewriteScene(baseUrl, item.id, scene, which === "points" ? { notes: lines.map((line) => `- ${line}`).join("\n") } : { shows: typed.trim() }, notify), forget)
  }

  // startRecording leaves the plan for the first scene that waits to be said.
  function startRecording() {
    const next = scenes.find((s) => s.status === "waiting" && !s.planned)
    if (next) setSelectedId(next.id)
    setView("record")
  }

  function pick(which: "mic" | "cam", value: string) {
    remember(`legato:studio-${which}`, value)
    if (which === "mic") setMic(value)
    else setCam(value)
    // The next take or test opens what was chosen.
    close()
  }

  const root = `bg-lw-ground font-lw text-[13px] leading-5 text-lw-text ${fill ? "flex min-h-0 flex-1 flex-col" : ""}`

  if (!project) {
    return (
      <div className={`${root} p-4`}>
        {failure ? <StudioUnavailable failure={failure} /> : <p role="status" className="text-lw-text-2">Opening the studio…</p>}
      </div>
    )
  }

  const count = recordedCount(project)
  // The take is still being recorded while it finishes.
  const recording = phase === "recording" || phase === "stopping"
  // From the count of three to the last word: the person is presenting.
  const presenting = recording || phase === "counting"
  const points = scene ? talkingPoints(scene.notes) : []
  const hit = scene ? pointsHit(scene) : []
  const runs = scene ? transcriptRuns(scene) : []
  const toDecide = scenes.reduce((sum, s) => sum + openOffPlan(s).length, 0)
  // The scenes whose pictures are still to be drawn, and whether the person
  // has said the plan is settled.
  const drawing = scenes.filter((s) => s.planned).length
  const isAgreed = (project.comments ?? []).some((text) => text.startsWith("The person agreed the plan"))
  const whole = planLength(project)
  const shown = view ?? "record"
  const unreviewed = scenes.filter((s) => s.status === "recorded" && !s.review).length
  const blocks = timeline(project)
  const chapter = headings.slice(0, index + 1).reverse().find(Boolean) ?? ""
  const mics = devices.filter((d) => d.kind === "audioinput" && d.deviceId && d.deviceId !== "default" && d.deviceId !== "communications")
  const cams = devices.filter((d) => d.kind === "videoinput" && d.deviceId)
  const status = phase === "saving" ? "Keeping the take and writing down what you said. The first time, this downloads a model."
    : phase === "opening" ? "Waiting for the browser to hand over the microphone. Allow it if it asks."
      : note || (recording ? "Recording. Leave a breath before your first word and after your last." : "")
  const canRender = count.recorded === count.toRecord && project.problems.length === 0 && !project.render.running && phase === "idle"
  const part = scene?.take ? Math.max(1, intoTake(scene.take, scene.take.endMs || scene.take.durationMs || 0)) : 1
  const playedMs = heardMs < 0 ? 0 : Math.min(part, intoTake(scene?.take, heardMs))

  const label = "text-[11px] font-semibold uppercase tracking-[1px] text-lw-faint"
  const square = "flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-lw-raised text-lw-text-2 transition-colors hover:text-lw-text disabled:cursor-not-allowed disabled:opacity-30"
  const videoFile = project.render.file
  // copyPath puts the video's path on the clipboard. A page that is not
  // served over a safe connection has no clipboard: the path can then be
  // selected by hand, which one click on it does.
  const copyPath = async (path: string) => {
    try {
      await navigator.clipboard.writeText(path)
      setCopied("done")
    } catch {
      setCopied("failed")
    }
    setTimeout(() => setCopied(""), 2000)
  }
  const secondary = "h-8 shrink-0 rounded-md border border-lw-line-strong bg-lw-raised px-3.5 text-[13px] text-lw-text transition-colors hover:border-lw-faint disabled:cursor-not-allowed disabled:opacity-40"
  const primary = "h-8 shrink-0 rounded-md bg-lw-accent px-3.5 text-[13px] font-medium text-white transition-opacity hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-40"
  const quietButton = "h-8 shrink-0 rounded-md px-2 text-[13px] text-lw-text-2 transition-colors hover:text-lw-text disabled:cursor-not-allowed disabled:opacity-40"
  const field = "min-w-0 flex-1 rounded-md border border-lw-line-strong bg-lw-surface px-2 py-1.5 text-lw-text"
  const input = "w-full min-w-0 rounded-md border border-lw-line-strong bg-lw-surface px-2.5 py-1.5 text-[13px] text-lw-text placeholder:text-lw-faint focus:border-lw-accent-ring focus:outline-none"

  // What the person wants changed in a scene, for the agent to read in the
  // storyboard: comments, and taking the scene out. Both views have it.
  const forAgent = (scene: StudioScene) => (
    <div className="mt-4">
      <h3 className={label}>For the agent</h3>
      {(scene.comments ?? []).length > 0 && (
        <ul aria-label="Comments for the agent" className="mt-1.5 space-y-1">
          {(scene.comments ?? []).map((text, n) => (
            <li key={`${n}:${text}`} className="flex items-start gap-2 rounded-md border border-lw-line bg-lw-surface px-2.5 py-1.5 text-[13px]">
              <span className="min-w-0 flex-1 whitespace-pre-wrap">{text}</span>
              <button type="button" aria-label="Take this comment back" title="Take this comment back" disabled={phase !== "idle"} onClick={() => change(takeBackComment(baseUrl, item.id, scene.id, n + 1))} className="shrink-0 rounded p-1 text-lw-faint hover:text-lw-text disabled:opacity-40">
                <X size={13} />
              </button>
            </li>
          ))}
        </ul>
      )}
      <form
        className="mt-1.5 flex items-end gap-2"
        onSubmit={(event) => {
          event.preventDefault()
          if (comment.trim()) change(commentOnScene(baseUrl, item.id, scene.id, comment.trim(), notify), () => setComment(""))
        }}
      >
        <textarea
          aria-label="A comment for the agent on this scene"
          value={comment}
          onChange={(event) => setComment(event.target.value)}
          onKeyDown={(event) => { if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) event.currentTarget.form?.requestSubmit() }}
          rows={2}
          placeholder="What should change in this scene?"
          className="min-w-0 flex-1 resize-y rounded-md border border-lw-line-strong bg-lw-surface px-2.5 py-1.5 text-[13px] text-lw-text placeholder:text-lw-faint focus:border-lw-accent-ring focus:outline-none"
        />
        <button type="submit" disabled={!comment.trim() || phase !== "idle"} className={secondary}>Add comment</button>
      </form>
      {removing ? (
        <div className="mt-2 flex flex-wrap items-center gap-2 rounded-lg border border-lw-danger-line bg-lw-error-soft px-3 py-2 text-xs">
          <span className="min-w-0 flex-1">Remove {scene.id} from the video? Its takes are kept.</span>
          <button type="button" onClick={() => change(removeScene(baseUrl, item.id, scene.id, notify), () => setSelectedId(null))} className="h-8 rounded-md bg-lw-record px-3 font-medium text-white hover:opacity-90">Remove {scene.id}</button>
          <button type="button" onClick={() => setRemoving(false)} className={secondary}>Keep it</button>
        </div>
      ) : (
        <button type="button" disabled={phase !== "idle" || scenes.length < 2} onClick={() => setRemoving(true)} className="mt-2 flex h-8 items-center gap-1.5 text-xs text-lw-faint hover:text-lw-danger disabled:cursor-not-allowed disabled:opacity-40" title={scenes.length < 2 ? "A storyboard needs one scene" : "Takes this scene out of the video, and tells the agent"}>
          <Trash2 size={13} />
          Remove this scene
        </button>
      )}
    </div>
  )

  return (
    <div className={root}>
      <div ref={scroller} className={fill ? "min-h-0 flex-1 overflow-y-auto" : ""}>
        {failure && (
          <p role="status" className="border-b border-lw-line bg-lw-needs-soft px-4 py-2 text-xs text-lw-needs">
            The studio stopped answering, so what is shown may be out of date. {failure.message}
          </p>
        )}
        {changed && (
          <p role="status" className="flex items-start gap-2 border-b border-lw-line bg-lw-selected px-4 py-2 text-xs text-lw-text">
            <Sparkles size={13} className="mt-0.5 shrink-0 text-lw-link" aria-hidden="true" />
            <span className="min-w-0 flex-1">The plan changed: {changed}</span>
            <button type="button" onClick={() => setChanged("")} className="shrink-0 text-lw-link hover:text-lw-text">Dismiss</button>
          </p>
        )}

        <div role="tablist" aria-label="Studio" className="flex border-b border-lw-line px-4 text-[13px]">
          {(["plan", "record"] as const).map((name, n) => (
            <button
              key={name}
              type="button"
              role="tab"
              aria-selected={shown === name}
              disabled={presenting}
              onClick={() => setView(name)}
              className={`-mb-px border-b-2 px-3 py-2.5 transition-colors disabled:cursor-not-allowed ${shown === name ? "border-lw-accent font-semibold text-lw-text" : "border-transparent text-lw-text-2 hover:text-lw-text"}`}
            >
              <span className="mr-1.5 text-lw-faint">{n + 1}</span>{name === "plan" ? "Plan" : "Record"}
              {name === "plan" && drawing > 0 && <span className="ml-1.5 rounded-lg bg-lw-idle-soft px-1.5 font-lw-mono text-[10px] text-lw-text-2">{drawing}</span>}
              {name === "record" && toDecide > 0 && <span className="ml-1.5 rounded-lg bg-lw-needs-soft px-1.5 font-lw-mono text-[10px] text-lw-needs">{toDecide}</span>}
            </button>
          ))}
        </div>

        {shown === "record" && (
        <>
        {/* The whole video at a glance: a block for each scene, as wide as
            the scene is long. */}
        <nav aria-label="Timeline" className="border-b border-lw-line px-4 pb-2.5 pt-3">
          <ol className="flex h-[26px] gap-[3px]">
            {blocks.map((block, n) => {
              const current = scene?.id === block.id
              const look = current ? "bg-lw-accent text-white"
                : block.decide ? "border border-dashed border-lw-needs bg-lw-needs-soft"
                  : block.status === "recorded" ? "bg-lw-live-soft"
                    : block.status === "waiting" ? "border border-lw-line-strong bg-lw-raised"
                      : "bg-lw-idle-soft"
              return (
                <li key={block.id} className="min-w-[6px]" style={{ flex: `${block.weight} 1 0` }}>
                  <button
                    type="button"
                    disabled={presenting}
                    onClick={() => go(scenes[n])}
                    aria-label={`Scene ${n + 1}: ${block.id}`}
                    aria-current={current ? "true" : undefined}
                    title={`${block.id} · ${block.decide ? "something to decide" : STATUS[block.status].label}`}
                    className={`flex h-full w-full items-center justify-center rounded-[3px] text-[11px] font-semibold transition-opacity hover:opacity-80 disabled:cursor-not-allowed ${look}`}
                  >
                    {current ? n + 1 : ""}
                  </button>
                </li>
              )
            })}
          </ol>
          <div className="mt-2 flex justify-between text-xs leading-4 text-lw-text-2">
            <span>0:00</span>
            <span>
              <span className="text-lw-live" aria-hidden="true">●</span> {count.recorded} recorded
              {count.toRecord > count.recorded && <> · {count.toRecord - count.recorded} to record</>}
              {toDecide > 0 && <> · <span className="text-lw-needs" aria-hidden="true">◌</span> {toDecide} to decide</>}
            </span>
            <span>{project.durationMs ? clock(project.durationMs) : ""}</span>
          </div>
        </nav>

        {scene && (
          <section aria-label={`Scene ${scene.id}`} className="px-4 pb-4">
            <div className="flex items-center gap-2.5 pb-2.5 pt-3.5">
              <button type="button" aria-label="Previous scene" disabled={index <= 0 || presenting} onClick={() => go(scenes[index - 1])} className={square}><ChevronLeft size={16} /></button>
              <div className="min-w-0 flex-1">
                <div className="truncate text-base font-semibold leading-6">{scene.id}</div>
                <div className="truncate text-xs leading-4 text-lw-text-2">
                  Scene {index + 1} of {scenes.length}{chapter && ` · ${chapter}`} · {scene.kind} · {scene.status === "recorded" && scene.durationMs ? clock(scene.durationMs) : STATUS[scene.status].label}
                </div>
              </div>
              <button type="button" aria-label="Microphone and camera" aria-expanded={settings} disabled={presenting} onClick={() => setSettings((shown) => !shown)} className={square} title="Microphone and camera">
                <Settings2 size={15} />
              </button>
              <button type="button" aria-label="Next scene" disabled={index >= scenes.length - 1 || presenting} onClick={() => go(scenes[index + 1])} className={square}><ChevronRight size={16} /></button>
            </div>

            {settings && (
              <div role="group" aria-label="Microphone and camera" className="mb-3 space-y-2 rounded-lg border border-lw-line bg-lw-surface p-3 text-xs text-lw-text-2">
                <label className="flex items-center gap-2">
                  <span className="w-20 shrink-0">Microphone</span>
                  <select value={mic} disabled={phase !== "idle"} onChange={(event) => pick("mic", event.target.value)} className={field}>
                    <option value="">Default microphone</option>
                    {mics.map((d, n) => <option key={d.deviceId} value={d.deviceId}>{d.label || `Microphone ${n + 1}`}</option>)}
                  </select>
                </label>
                <label className="flex items-center gap-2">
                  <span className="w-20 shrink-0">Camera</span>
                  <select value={cam} disabled={phase !== "idle"} onChange={(event) => pick("cam", event.target.value)} className={field}>
                    <option value="">Default camera</option>
                    {cams.map((d, n) => <option key={d.deviceId} value={d.deviceId}>{d.label || `Camera ${n + 1}`}</option>)}
                    <option value={NO_CAMERA}>No camera</option>
                  </select>
                </label>
                <div className="flex items-center gap-2">
                  <button type="button" disabled={phase !== "idle"} onClick={test} className={secondary} title="Opens the microphone and the camera without recording, to see that they are the right ones">
                    Test
                  </button>
                  <span className="min-w-0 flex-1 truncate">{using ? `Using ${using}` : "Your choice is remembered. Test opens them without recording."}</span>
                </div>
                <label className="flex items-start gap-2 border-t border-lw-line pt-2">
                  <input
                    type="checkbox"
                    checked={notify}
                    onChange={(event) => { setNotify(event.target.checked); remember("legato:studio-notify", event.target.checked ? "on" : "off") }}
                    className="mt-0.5 accent-lw-accent"
                  />
                  <span>
                    <span className="text-lw-text">Tell the agent when a take is ready</span>
                    <span className="block">It reviews each take as you go: marks what you covered, and asks about what left the plan.</span>
                  </span>
                </label>
              </div>
            )}

            <div className="relative aspect-video w-full overflow-hidden rounded-lg border border-lw-line bg-lw-rail">
              {watching && film
                ? <video src={film} controls autoPlay aria-label="The rendered video" className="h-full w-full bg-black" />
                : <StudioStill baseUrl={baseUrl} itemId={item.id} path={scene.still} alt={`The scene ${scene.id}`} />}
              {counting > 0 && (
                <p role="timer" aria-label="The take starts in" className="absolute inset-0 grid place-items-center bg-black/60 text-8xl font-semibold tabular-nums text-white">{counting}</p>
              )}
              {/* Over the picture, where a person who is presenting is looking. */}
              {recording && quiet >= QUIET_TOLD_MS && (
                <p role="alert" className="absolute inset-x-0 top-0 bg-lw-record px-3 py-2 text-center text-sm font-semibold text-white">
                  The microphone is giving no sound, and has not for {Math.floor(quiet / 1000)} s. If you are speaking, cancel the take: another page or program may have the microphone.
                </p>
              )}
              {/* Where the face is drawn in the video: 232 of 1920 wide, 44 from the corner. */}
              <video
                ref={self}
                muted
                playsInline
                aria-label="Your camera"
                className={`absolute bottom-[4.07%] right-[2.29%] aspect-square w-[12.08%] -scale-x-100 rounded-full border-2 border-lw-accent bg-black object-cover ${seen ? "" : "hidden"} ${recording ? "" : "opacity-40"}`}
              />
              {!presenting && !watching && (
                <div className="absolute inset-x-0 bottom-0 flex items-center gap-2.5 bg-lw-rail/95 px-3 py-2">
                  {scene.take && (
                    <>
                      <button type="button" aria-label={playing ? "Pause take" : "Play take"} disabled={loadingTake} onClick={() => void play()} className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-lw-text text-lw-ground disabled:opacity-50">
                        {playing ? <Pause size={12} fill="currentColor" /> : <Play size={12} fill="currentColor" />}
                      </button>
                      <div role="progressbar" aria-label="Where the take is" aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round((playedMs / part) * 100)} className="flex h-1 min-w-0 flex-1 rounded-sm bg-lw-line-strong">
                        <div className="rounded-sm bg-lw-accent" style={{ width: `${Math.min(100, (playedMs / part) * 100)}%` }} />
                      </div>
                      <span className="font-lw-mono text-[11px] text-lw-text-2">{clock(playedMs)} / {clock(part)}</span>
                    </>
                  )}
                  {!scene.take && <span className="min-w-0 flex-1" />}
                  <button
                    type="button"
                    onClick={() => proposeComposerText(item.task_id, questionAboutScene(title, project, scene))}
                    className="flex h-6 shrink-0 items-center gap-1.5 rounded-md border border-lw-line-strong px-2 text-xs text-lw-text hover:border-lw-faint"
                    title="Puts this scene in your message box, for you to say what you think of it"
                  >
                    <MessageCircleQuestion size={12} />
                    Ask about this scene
                  </button>
                </div>
              )}
              {watching && (
                <button type="button" onClick={() => setWatching(false)} className="absolute right-2 top-2 rounded-md bg-lw-rail/90 px-2 py-1 text-xs text-lw-text hover:bg-lw-rail">Back to the scene</button>
              )}
              <audio ref={audio} onTimeUpdate={heardSoFar} onPause={() => setPlaying(false)} onEnded={() => setPlaying(false)} className="hidden" />
            </div>

            {/* Before a take, what to say is the thing to look at. */}
            {scene.status !== "recorded" && (
              <div className="mt-3.5">
                {points.length > 0 ? (
                  <>
                    <h3 className={label}>Talking points</h3>
                    <ul aria-label="Talking points" className="mt-1.5 space-y-1.5">
                      {points.map((point) => (
                        <li key={point} className="flex gap-2.5 text-[17px] leading-snug">
                          <span className="mt-[0.55em] h-1.5 w-1.5 shrink-0 rounded-full bg-lw-needs" aria-hidden="true" />
                          <span className="min-w-0">{point}</span>
                        </li>
                      ))}
                    </ul>
                  </>
                ) : scene.says ? (
                  <>
                    <h3 className={label}>A voice would say</h3>
                    <p className="mt-1.5 whitespace-pre-wrap text-sm leading-[23px] text-lw-text-2">{scene.says}</p>
                  </>
                ) : (
                  <p className="text-lw-text-2">This scene has no talking points yet. Say what the picture needs, or ask the agent for some.</p>
                )}
              </div>
            )}

            <div className="mt-3.5 flex flex-wrap items-center gap-1.5">
              {!presenting && scene.takes.length > 0 && (
                <>
                  <span className="mr-1 text-xs text-lw-text-2">Takes</span>
                  <ul aria-label="Takes" className="flex flex-wrap gap-1.5">
                    {scene.takes.map((t, n) => (
                      <li key={t.src}>
                        <button
                          type="button"
                          disabled={t.current || phase !== "idle"}
                          onClick={() => use(t.src)}
                          aria-label={`${t.current ? "Take" : "Use take"} ${n + 1}${t.durationMs ? `, ${clock(t.durationMs)}` : ""}${t.current ? ", in use" : ""}`}
                          title={t.current ? `${t.src}: the take this scene uses` : `${t.src}: use this take`}
                          className={`h-6 rounded-md border px-2.5 text-xs ${t.current ? "border-lw-accent bg-lw-selected text-lw-text" : "border-lw-line-strong bg-lw-raised text-lw-text-2 hover:text-lw-text disabled:opacity-40"}`}
                        >
                          {n + 1}{t.durationMs ? ` · ${clock(t.durationMs)}` : ""}{t.current ? " ★ using" : ""}
                        </button>
                      </li>
                    ))}
                  </ul>
                  <span className="min-w-0 flex-1" />
                </>
              )}
              <button
                type="button"
                disabled={phase === "opening" || phase === "counting" || phase === "saving" || phase === "stopping" || phase === "changing"}
                onClick={recording ? stop : start}
                className={presenting || scene.status !== "recorded"
                  ? `flex h-10 min-w-36 items-center justify-center gap-2 rounded-md px-4 text-sm font-semibold transition-colors disabled:cursor-not-allowed disabled:opacity-50 ${recording ? "border border-lw-record bg-lw-raised text-lw-danger" : "bg-lw-record text-white hover:opacity-90"}`
                  : "flex h-6 items-center gap-1.5 rounded-md border border-lw-danger-line px-2.5 text-xs text-lw-danger transition-colors hover:border-lw-danger disabled:cursor-not-allowed disabled:opacity-50"}
              >
                {recording ? <Square size={13} /> : <Circle size={presenting || scene.status !== "recorded" ? 13 : 8} fill="currentColor" className={presenting || scene.status !== "recorded" ? "" : "text-lw-record"} />}
                {phase === "stopping" ? "Finishing…" : phase === "counting" ? "Get ready…" : recording ? "Stop" : scene.status === "recorded" ? "New take" : "Record"}
              </button>
              {presenting && (
                <button type="button" onClick={cancel} aria-label="Cancel take" title="Stops and throws this take away, so you can try again" className={`${secondary} flex h-10 items-center gap-1.5`}>
                  <X size={14} />
                  Cancel
                </button>
              )}
              {(recording || level > 0) && (
                <>
                  <span className="ml-1 font-lw-mono text-sm tabular-nums" aria-label="Time recorded">{clock(seconds * 1000)}</span>
                  <span className="h-1.5 w-24 overflow-hidden rounded-sm bg-lw-line-strong" title="Your microphone: it moves when it hears you">
                    <span className="block h-full bg-lw-live" style={{ width: `${level}%` }} />
                  </span>
                </>
              )}
            </div>
            <p role="status" className={`mt-2 min-h-4 text-xs leading-4 ${recording && !note ? "text-lw-text-2" : "text-lw-needs"}`}>{status}</p>

            {/* What the agent asks about the take: the person decides. */}
            {openOffPlan(scene).map(({ place, part: stretch }) => (
              <div key={place} role="group" aria-label={`Off the plan${stretch.about ? `: ${stretch.about}` : ""}`} className="mt-2 flex flex-col gap-2 rounded-lg border border-lw-needs/40 bg-lw-needs-soft/60 px-3.5 py-3">
                <div className="flex items-baseline gap-2">
                  <span className="font-semibold text-lw-needs">Your take went off-plan</span>
                  <span className="text-xs text-lw-text-2">{clock(intoTake(scene.take, stretch.from))} to {clock(intoTake(scene.take, stretch.to))}</span>
                </div>
                <p className="text-[13px] leading-[19px] text-lw-text">{stretch.note}</p>
                <div className="flex flex-wrap gap-2">
                  <button type="button" disabled={phase !== "idle"} onClick={() => change(decideOffPlan(baseUrl, item.id, scene.id, place, "add", notify))} className="h-8 rounded-md bg-lw-needs px-3 text-[13px] font-semibold text-lw-ground disabled:opacity-40" title="Asks the agent for a scene of its own for this, after this one">Add scene</button>
                  <button type="button" disabled={phase !== "idle"} onClick={() => change(decideOffPlan(baseUrl, item.id, scene.id, place, "keep"))} className="h-8 rounded-md border border-lw-needs/40 px-3 text-[13px] text-lw-text disabled:opacity-40">Keep in this scene</button>
                  <button type="button" disabled={phase !== "idle"} onClick={() => change(decideOffPlan(baseUrl, item.id, scene.id, place, "cut"))} className={quietButton} title="Leaves this stretch out of the take">Cut it</button>
                </div>
              </div>
            ))}

            {scene.status === "recorded" && (
              <>
                <div className="mt-4 flex flex-col gap-2">
                  <div className="flex items-baseline justify-between gap-3">
                    <h3 className={label}>Transcript</h3>
                    {scene.review && points.length > 0
                      ? <span className="text-xs text-lw-text-2"><span className={hit.length === points.length ? "text-lw-live" : "text-lw-needs"}>{hit.length} / {points.length}</span> talking points hit</span>
                      : !scene.review && (
                        <span className="flex items-baseline gap-2 text-xs text-lw-text-2">
                          {notify ? "The agent is looking at this take" : "The agent has not looked at this take"}
                          <button type="button" onClick={() => proposeComposerText(item.task_id, reviewRequest(title, project, scene))} className="text-lw-link hover:text-lw-text" title="Puts a request in your message box">Ask for a review</button>
                        </span>
                      )}
                  </div>
                  {runs.length > 0 ? (
                    <p className="text-sm leading-[23px] text-lw-text">
                      {runs.map((run, n) => (
                        <Fragment key={n}>
                          {run.at !== undefined && <span className="font-lw-mono text-[11px] text-lw-faint">{clock(run.at)} </span>}
                          <span
                            title={run.offPlan ? "Off the plan" : run.point ? `Covers: ${points[run.point - 1] ?? `talking point ${run.point}`}` : undefined}
                            className={`${run.offPlan ? "bg-lw-needs-soft text-lw-needs" : run.point && run.point <= points.length ? "border-b-2 border-lw-live" : ""} ${playing && heardMs >= run.startMs && heardMs < run.endMs ? "text-white" : ""}`}
                          >
                            {run.text}
                          </span>{" "}
                        </Fragment>
                      ))}
                    </p>
                  ) : (
                    <p className="whitespace-pre-wrap text-sm leading-[23px] text-lw-text">{scene.heard || scene.says || "The take has no transcript yet."}</p>
                  )}
                  {leftTheScript(scene) && (
                    <p className="text-xs text-lw-done">You went your own way here. That is fine: the scene is changed to follow what you said.</p>
                  )}
                </div>

                {points.length > 0 && (
                  <div className="mt-3.5 flex flex-col gap-1">
                    <h3 className={label}>Talking points</h3>
                    <ul aria-label="Talking points" className="flex flex-col gap-1">
                      {points.map((point, n) => {
                        const covered = hit.includes(n + 1)
                        return (
                          <li key={point} title={!scene.review ? undefined : covered ? "Covered in your take" : "Not covered in your take"} className="flex gap-2 text-[13px]">
                            <span className={`w-3 shrink-0 text-center ${covered ? "text-lw-live" : "text-lw-faint"}`} aria-hidden="true">{covered ? "✓" : scene.review ? "○" : "·"}</span>
                            <span className={scene.review && !covered ? "text-lw-text-2" : ""}>{point}</span>
                          </li>
                        )
                      })}
                    </ul>
                  </div>
                )}
              </>
            )}

            {forAgent(scene)}
          </section>
        )}
        </>
        )}

        {shown === "plan" && (
          <section aria-label="The plan">
            {/* What the video is to cover, in the person's words. The agent
                drafts the scenes from it. */}
            <div className="border-b border-lw-line px-4 py-3">
              <h3 className={label}>What this video covers</h3>
              {briefDraft !== null || !project.brief ? (
                <form
                  className="mt-1.5 flex flex-col gap-2"
                  onSubmit={(event) => {
                    event.preventDefault()
                    const text = (briefDraft ?? "").trim()
                    if (text) change(writeBrief(baseUrl, item.id, text, notify), () => setBriefDraft(null))
                  }}
                >
                  <textarea
                    aria-label="What you want the video to cover"
                    value={briefDraft ?? ""}
                    onChange={(event) => setBriefDraft(event.target.value)}
                    rows={3}
                    placeholder="What do you want to cover, who is it for, and how long should it be? The agent drafts the scenes from this."
                    className={`${input} resize-y`}
                  />
                  <div className="flex items-center gap-2">
                    <button type="submit" disabled={!(briefDraft ?? "").trim() || phase !== "idle"} className={project.scenes.some((s) => !s.planned) ? secondary : primary}>{project.brief ? "Save, and plan again" : "Propose a plan"}</button>
                    {project.brief && <button type="button" onClick={() => setBriefDraft(null)} className={quietButton}>Cancel</button>}
                    <span className="min-w-0 flex-1 text-xs text-lw-text-2">{notify ? "The agent is told, and drafts the scenes." : "Kept in the storyboard for the agent to read."}</span>
                  </div>
                </form>
              ) : (
                <div className="mt-1 flex items-start gap-2">
                  <p className="min-w-0 flex-1 whitespace-pre-wrap text-[13px]">{project.brief}</p>
                  <button type="button" onClick={() => setBriefDraft(project.brief ?? "")} className="shrink-0 text-xs text-lw-link hover:text-lw-text">Edit</button>
                </div>
              )}
            </div>

            <nav aria-label="Outline" className="px-2 py-2">
              <ol>
                {scenes.map((s, n) => {
                  const heading = headings[n]
                  const current = scene?.id === s.id
                  const length = lengthOf(s)
                  const row = (
                    <button
                      type="button"
                      disabled={presenting}
                      onClick={() => go(s)}
                      aria-current={current ? "true" : undefined}
                      title={s.planned ? "Planned: its picture is not drawn yet" : STATUS[s.status].label}
                      className={`flex min-h-8 w-full items-center gap-2.5 rounded-md px-2 py-1.5 text-left text-sm transition-colors disabled:cursor-not-allowed ${current ? "font-semibold" : "hover:bg-lw-raised"}`}
                    >
                      <span className={`h-2 w-2 shrink-0 rounded-full ${STATUS[s.status].dot}`} aria-hidden="true" />
                      <span className="min-w-0 flex-1 truncate">{s.id}</span>
                      {marks[s.id] && <span className="shrink-0 rounded bg-lw-selected px-1.5 font-lw-mono text-[10px] font-normal uppercase text-lw-link">{marks[s.id]}</span>}
                      {openOffPlan(s).length > 0 && <span className="shrink-0 rounded bg-lw-needs-soft px-1.5 font-lw-mono text-[10px] font-normal uppercase text-lw-needs">decide</span>}
                      {leftTheScript(s) && <span className="shrink-0 text-[11px] font-normal text-lw-done">your own way</span>}
                      {(s.comments ?? []).length > 0 && <span className="shrink-0 text-[11px] font-normal text-lw-needs">{s.comments?.length === 1 ? "1 comment" : `${s.comments?.length} comments`}</span>}
                      <span className="shrink-0 text-xs font-normal text-lw-faint">{s.kind}</span>
                      <span className="w-10 shrink-0 text-right font-lw-mono text-xs font-normal text-lw-text-2" title={length.sure ? undefined : "A guess, from how much there is to say"}>{length.sure ? "" : "~"}{clock(length.ms)}</span>
                    </button>
                  )
                  return (
                    <li key={s.id}>
                      {heading && (renaming?.from === heading ? (
                        <form
                          className="mt-3 flex gap-2 px-2"
                          onSubmit={(event) => {
                            event.preventDefault()
                            const to = renaming.to.trim()
                            if (to && to !== heading) change(renameChapter(baseUrl, item.id, heading, to), () => setRenaming(null))
                            else setRenaming(null)
                          }}
                        >
                          <input autoFocus aria-label={`The title of the chapter ${heading}`} value={renaming.to} onChange={(event) => setRenaming({ from: heading, to: event.target.value })} onBlur={(event) => event.currentTarget.form?.requestSubmit()} className={`${input} h-7 py-0 text-xs`} />
                        </form>
                      ) : (
                        <button type="button" disabled={presenting} onClick={() => setRenaming({ from: heading, to: heading })} aria-label={`Rename the chapter ${heading}`} title="Rename this chapter" className={`${label} mt-3 block w-full px-2 text-left hover:text-lw-text-2`}>{heading}</button>
                      ))}
                      {!current ? row : (
                        <div className="my-1 flex flex-col overflow-hidden rounded-lg border border-lw-accent-ring bg-lw-selected">
                          {row}
                          <div className="grid grid-cols-[minmax(0,2fr)_minmax(0,3fr)] gap-3 px-2 pb-2.5">
                            <div className="aspect-video self-start overflow-hidden rounded-md border border-lw-line bg-lw-rail">
                              <StudioStill baseUrl={baseUrl} itemId={item.id} path={s.still} alt={`The scene ${s.id}`} />
                            </div>
                            <div className="flex min-w-0 flex-col gap-1.5">
                              <label className="flex flex-col gap-1">
                                <span className="text-xs text-lw-text-2">Shows{s.planned && <span className="ml-1.5 rounded bg-lw-idle-soft px-1.5 font-lw-mono text-[10px] uppercase">not drawn yet</span>}</span>
                                <input
                                  aria-label="What this scene shows"
                                  value={draftOf("shows")}
                                  onChange={(event) => setDrafts({ ...(drafts.id === s.id ? drafts : {}), id: s.id, shows: event.target.value })}
                                  onBlur={() => saveDraft("shows")}
                                  onKeyDown={(event) => { if (event.key === "Enter") event.currentTarget.blur() }}
                                  placeholder="What the picture will be, in a line"
                                  className={input}
                                />
                              </label>
                              <label className="flex flex-col gap-1">
                                <span className="text-xs text-lw-text-2">Talking points, one to a line</span>
                                <textarea
                                  aria-label="Talking points of this scene"
                                  value={draftOf("points")}
                                  onChange={(event) => setDrafts({ ...(drafts.id === s.id ? drafts : {}), id: s.id, points: event.target.value })}
                                  onBlur={() => saveDraft("points")}
                                  rows={Math.max(3, draftOf("points").split("\n").length)}
                                  placeholder="What to get across here"
                                  className={`${input} resize-y leading-[19px]`}
                                />
                              </label>
                            </div>
                          </div>
                          <div className="flex items-center gap-1.5 border-t border-lw-line bg-lw-raised px-2 py-2">
                            <button type="button" aria-label="Move this scene up" title="Move up" disabled={phase !== "idle" || n === 0} onClick={() => change(moveScene(baseUrl, item.id, s.id, -1))} className={square}><ArrowUp size={14} /></button>
                            <button type="button" aria-label="Move this scene down" title="Move down" disabled={phase !== "idle" || n === scenes.length - 1} onClick={() => change(moveScene(baseUrl, item.id, s.id, 1))} className={square}><ArrowDown size={14} /></button>
                            <span className="min-w-0 flex-1" />
                            <button type="button" onClick={() => proposeComposerText(item.task_id, questionAboutScene(title, project, s))} className={quietButton} title="Puts this scene in your message box, for you to say what you think of it">Ask</button>
                            <button type="button" disabled={s.planned} onClick={() => setView("record")} className={secondary} title={s.planned ? "Its picture is drawn once the plan is agreed" : "Opens this scene where it is recorded"}>{s.status === "recorded" ? "Open its take" : "Record this scene"}</button>
                          </div>
                          <div className="px-2 pb-2.5">{forAgent(s)}</div>
                        </div>
                      )}
                    </li>
                  )
                })}
              </ol>
              <form
                className="mt-3 flex items-center gap-2 px-2"
                onSubmit={(event) => {
                  event.preventDefault()
                  if (adding.trim()) change(addScene(baseUrl, item.id, adding.trim(), scene?.id, notify), () => setAdding(""))
                }}
              >
                <input aria-label="A new scene, in a line" value={adding} onChange={(event) => setAdding(event.target.value)} placeholder={scene ? `Add a scene after ${scene.id}, about…` : "Add a scene about…"} className={input} />
                <button type="submit" disabled={!adding.trim() || phase !== "idle"} className={secondary}>Add to the plan</button>
              </form>
            </nav>
          </section>
        )}

        <section aria-label="Notes" className="px-4 pb-4">
          {shown === "record" && (
            <div className="flex justify-end">
              <button
                type="button"
                disabled={count.recorded === 0 || phase !== "idle"}
                onClick={() => proposeComposerText(item.task_id, lineUpRequest(title, project))}
                className={quietButton}
                title="Puts a request in your message box: the agent reads what you said and edits the video to follow it"
              >
                Make the video from my takes
              </button>
            </div>
          )}
          <StudioList tone="needs" heading="For the agent" label="For the agent" items={project.comments ?? []} />
          <StudioList tone="needs" heading="The last take" items={project.notice ? [project.notice] : []} />
          <StudioList tone="error" heading="The storyboard cannot be rendered yet" items={project.problems} />
          <StudioList tone="needs" heading="Will render, but probably not as meant" items={project.warnings} />
          <StudioList tone="error" heading="The video could not be rendered" items={project.render.error ? [project.render.error] : []} />
        </section>
      </div>

      {shown === "record" && videoFile && !project.render.running && (
        // The panel plays the video; to send it to someone the person needs
        // the file, and nothing else on the page says where it is.
        <div className="flex shrink-0 items-center gap-2.5 border-t border-lw-line bg-lw-surface px-4 py-2 text-xs leading-4 text-lw-text-2">
          <span className="shrink-0">Video file</span>
          <span aria-label="The video's file" title={videoFile} className="flex min-w-0 flex-1 select-all font-lw-mono text-lw-text">
            <span className="min-w-0 truncate">{videoFile.slice(0, videoFile.lastIndexOf("/") + 1).replace(/\/$/, "")}</span>
            <span className="shrink-0">{videoFile.slice(videoFile.lastIndexOf("/"))}</span>
          </span>
          <button type="button" onClick={() => void copyPath(videoFile)} className="shrink-0 rounded-md border border-lw-line-strong bg-lw-raised px-2.5 py-1 text-xs text-lw-text transition-colors hover:border-lw-faint">
            {copied === "done" ? "Copied" : copied === "failed" ? "Could not copy" : "Copy path"}
          </button>
        </div>
      )}

      <div className="flex shrink-0 items-center gap-2.5 border-t border-lw-line bg-lw-surface px-4 py-3">
        {shown === "plan" ? (
          <>
            <div className="min-w-0 flex-1">
              <div className="truncate">{scenes.length === 1 ? "1 scene" : `${scenes.length} scenes`} · {whole.sure ? "" : "about "}{clock(whole.ms)}</div>
              <div role="status" aria-label="The plan" className="truncate text-xs leading-4 text-lw-text-2">
                {drawing === 0 ? "Every scene has its picture"
                  : isAgreed ? `Agreed: the agent is drawing ${drawing === 1 ? "1 picture" : `${drawing} pictures`}`
                    : `${drawing === 1 ? "1 scene is" : `${drawing} scenes are`} not drawn yet`}
              </div>
            </div>
            {drawing > 0
              ? <button type="button" disabled={phase !== "idle" || isAgreed} onClick={() => change(agreePlan(baseUrl, item.id, notify))} className={primary} title="Says the plan is settled: the agent draws each scene's picture">{isAgreed ? "Plan agreed" : "Agree the plan"}</button>
              : <button type="button" onClick={startRecording} className={primary}>{count.recorded > 0 ? "Go on recording" : "Start recording"}</button>}
          </>
        ) : (
          <>
            <div className="min-w-0 flex-1">
              <div className="truncate">
                {count.toRecord > 0 ? `${count.recorded} of ${count.toRecord} recorded` : "Nothing here waits to be recorded"}
                {project.durationMs ? ` · ${clock(project.durationMs)}` : ""}
              </div>
              <div role="status" aria-label="The video" className="truncate text-xs leading-4 text-lw-text-2">
                {project.render.running ? `Rendering… ${project.render.log[project.render.log.length - 1] ?? ""}`
                  : toDecide > 0 ? `${toDecide} to decide`
                    : unreviewed > 0 ? `${unreviewed === 1 ? "1 take" : `${unreviewed} takes`} not reviewed yet`
                      : project.render.video ? (project.render.stale ? "The video is older than the storyboard" : "The video is up to date")
                        : count.toRecord > 0 && count.recorded === count.toRecord ? "All scenes recorded" : ""}
              </div>
            </div>
            <button type="button" disabled={!project.render.video || loadingFilm} onClick={() => void watch()} className={secondary} title="Plays the video as it was last rendered">
              {loadingFilm ? "Loading…" : "Preview video"}
            </button>
            <button type="button" disabled={!canRender} onClick={() => change(startRender(baseUrl, item.id))} className={primary} title={count.recorded < count.toRecord ? "Every scene has to be recorded first" : "Renders the video from the storyboard as it is"}>
              {project.render.running ? "Rendering…" : "Render"}
            </button>
          </>
        )}
      </div>
    </div>
  )
}

// StudioUnavailable says why there is no studio. When libretto is missing or
// too old the person can do something about it, and is told what.
function StudioUnavailable({ failure }: { failure: StudioError }) {
  const theirs = failure.code === "libretto_missing" || failure.code === "libretto_old" || failure.code === "legato_old"
  return (
    <div role="alert" className="rounded-lg border border-lw-needs/40 bg-lw-needs-soft/60 px-4 py-3 text-sm">
      <h3 className="font-semibold text-lw-needs">{theirs ? "The studio cannot open on this machine" : "The studio is not answering"}</h3>
      <p className="mt-1 text-lw-text">{failure.message}</p>
      <p className="mt-2 text-xs text-lw-text-2">
        {theirs ? "Nothing is lost: the storyboard is a file, and the studio opens here as soon as this is put right." : "It is tried again every few seconds."}
      </p>
    </div>
  )
}

function StudioList({ tone, heading, items, label }: { tone: "needs" | "error"; heading: string; items: string[]; label?: string }) {
  if (items.length === 0) return null
  const colours = tone === "error" ? "border-lw-danger-line bg-lw-error-soft text-lw-danger" : "border-lw-needs/40 bg-lw-needs-soft/60 text-lw-needs"
  return (
    <div className={`mt-4 rounded-lg border px-3 py-2 text-xs ${colours}`}>
      <h3 className="font-semibold">{heading}</h3>
      <ul aria-label={label} className="mt-1 list-disc space-y-1 pl-4 text-lw-text">
        {items.map((text) => <li key={text}>{text}</li>)}
      </ul>
    </div>
  )
}

// StudioStill draws a scene's picture. Its address changes when the picture
// does, so one address is fetched once. The picture before stays until the
// new one is ready, so a change by the agent does not flash.
function StudioStill({ baseUrl, itemId, path, alt }: { baseUrl: string; itemId: string; path: string; alt: string }) {
  const [shown, setShown] = useState<{ path: string; address: string | null; failed: boolean }>({ path: "", address: null, failed: false })
  const made = useRef<string | null>(null)
  useEffect(() => {
    let gone = false
    fetchStudioFile(baseUrl, itemId, path)
      .then((address) => {
        if (gone) { URL.revokeObjectURL(address); return }
        if (made.current) URL.revokeObjectURL(made.current)
        made.current = address
        setShown({ path, address, failed: false })
      })
      .catch(() => { if (!gone) setShown({ path, address: null, failed: true }) })
    return () => { gone = true }
  }, [baseUrl, itemId, path])
  useEffect(() => () => { if (made.current) URL.revokeObjectURL(made.current) }, [])
  if (shown.address) return <img src={shown.address} alt={alt} className={`h-full w-full ${shown.path === path ? "" : "opacity-60"}`} />
  return (
    <div role="status" className="grid h-full w-full place-items-center p-6 text-center text-sm text-lw-text-2">
      {shown.failed ? "This scene cannot be drawn until the storyboard's problems are fixed." : "Drawing the scene…"}
    </div>
  )
}
