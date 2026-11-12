import { useCallback, useEffect, useRef, useState } from "react"
import { useNavigate } from "react-router-dom"
import { fetchVoiceStatus, openVoice, VOICE_HASH, type VoiceStatus } from "./status"
import { fetchReeve } from "../lib/reeve"
import { getToken } from "../lib/auth"
import { openMainVoice, shellStreams, voiceAudio } from "./audio"
import { initialVoiceView, VoiceSessionClient, type AudioPort, type SocketLike, type VoiceView } from "./session"
import { VoiceSheet, type VoiceReadinessView } from "./VoiceWidgets"
import { VoiceRing } from "./VoiceRing"
import { useReeveName } from "../lib/reeveName"

function sessionUrl(baseUrl: string): string {
  let base: string
  if (!baseUrl) {
    base = `${window.location.protocol === "https:" ? "wss:" : "ws:"}//${window.location.host}`
  } else {
    const parsed = new URL(baseUrl)
    base = `${parsed.protocol === "https:" ? "wss:" : "ws:"}//${parsed.host}`
  }
  const token = getToken(baseUrl || undefined)
  return `${base}/api/reeve/voice/session${token ? `?token=${encodeURIComponent(token)}` : ""}`
}

// readinessOf is what the sheet says before it listens. A worker's page
// cannot talk to the main's Reeve: the voice is on the main.
function readinessOf(status: VoiceStatus): VoiceReadinessView {
  const main = status.instance.name
  if (status.instance.role === "worker") {
    return { ready: false, reason: "Voice is on the main", setting: "", main }
  }
  return { ready: status.ready, reason: status.reason, setting: status.setting, main }
}

function deviceName(): string {
  const ua = navigator.userAgent
  if (/Android/i.test(ua)) return "Android phone"
  if (/iPhone|iPad/i.test(ua)) return "iPhone"
  return "Browser"
}

export interface VoiceLauncherProps {
  baseUrl: string
  // Tests give their own audio and socket.
  audio?: AudioPort | null
  socket?: (url: string) => SocketLike
}

// useVoiceHash is whether the voice sheet is open, from the fragment.
function useVoiceHash(): [boolean, (open: boolean) => void] {
  const [open, setOpen] = useState(() => window.location.hash === VOICE_HASH)
  useEffect(() => {
    const onHash = () => setOpen(window.location.hash === VOICE_HASH)
    window.addEventListener("hashchange", onHash)
    return () => window.removeEventListener("hashchange", onHash)
  }, [])
  const set = useCallback((next: boolean) => {
    if (next) {
      window.location.hash = VOICE_HASH
    } else if (window.location.hash === VOICE_HASH) {
      history.replaceState(window.history.state, "", window.location.pathname + window.location.search)
    }
    setOpen(next)
  }, [])
  return [open, set]
}

// VoiceLauncher is the web UI's voice: a sheet over the page, opened by
// the ring button or #reeve-voice, that holds a spoken conversation with
// this server's Reeve. It checks first that the server can, and says why
// not in one line.
export function VoiceLauncher({ baseUrl, audio: givenAudio, socket }: VoiceLauncherProps) {
  const [open, setOpen] = useVoiceHash()
  const [readiness, setReadiness] = useState<VoiceReadinessView | null>(null)
  const [role, setRole] = useState("")
  const [view, setView] = useState<VoiceView>(initialVoiceView)
  const clientRef = useRef<VoiceSessionClient | null>(null)
  // Each opening of the sheet; closing it makes any start still under way
  // stale, so it records nothing.
  const openingRef = useRef(0)
  const navigate = useNavigate()

  // end ends the conversation however the sheet closed: a button, a click
  // outside, Back, or the page going.
  const end = useCallback(() => {
    openingRef.current++
    clientRef.current?.bye()
    clientRef.current = null
    setView(initialVoiceView)
    setReadiness(null)
  }, [])

  const close = useCallback(() => setOpen(false), [setOpen])

  useEffect(() => {
    if (!open) end()
  }, [open, end])

  // listen checks the server can, then starts a turn; with start false it
  // only checks.
  const listen = useCallback(async (start = true) => {
    const mine = openingRef.current
    const stale = () => mine !== openingRef.current
    let status: VoiceStatus
    try {
      status = await fetchVoiceStatus(baseUrl)
    } catch {
      if (stale()) return
      setReadiness({ ready: false, reason: "Can't reach this Legato", setting: "", main: "this Legato" })
      return
    }
    if (stale()) return
    setRole(status.instance.role)
    const next = readinessOf(status)
    const audio = givenAudio === undefined ? voiceAudio(baseUrl) : givenAudio
    if (next.ready && !audio) {
      next.ready = false
      next.reason = "No microphone here: this page needs HTTPS, or the Legato app"
    }
    setReadiness(next)
    if (!next.ready || !audio || !start) return
    try {
      let client = clientRef.current
      if (!client || client.view.ended) {
        client = new VoiceSessionClient({ url: sessionUrl(baseUrl), device: deviceName(), client: "web", audio, socket, onView: setView })
        clientRef.current = client
        await client.connect()
      }
      if (stale()) return
      await client.listen()
    } catch (err) {
      if (stale()) return
      setReadiness({ ...next, ready: false, reason: err instanceof Error ? err.message : String(err) })
    }
  }, [baseUrl, givenAudio, socket])

  // openConversation shows the Reeve's own conversation.
  const openConversation = useCallback(async () => {
    let task = ""
    try {
      task = (await fetchReeve(baseUrl)).current?.task_id ?? ""
    } catch {
      // The Agents page still opens.
    }
    close()
    navigate(task ? `/agents?task=${encodeURIComponent(task)}` : "/agents")
  }, [baseUrl, close, navigate])

  // Opening the sheet starts listening, as the key does on the desktop;
  // but a page the person has not touched yet (opened at #reeve-voice) may
  // not start the microphone's audio, so it waits for Talk.
  useEffect(() => {
    if (open && !clientRef.current) void listen(navigator.userActivation?.hasBeenActive !== false)
  }, [open, listen])

  // Escape closes the sheet, and the focus goes back where it was.
  useEffect(() => {
    if (!open) return
    const before = document.activeElement as HTMLElement | null
    const onKey = (e: KeyboardEvent) => { if (e.key === "Escape" && !e.defaultPrevented) close() }
    document.addEventListener("keydown", onKey)
    return () => {
      document.removeEventListener("keydown", onKey)
      before?.focus?.()
    }
  }, [open, close])

  // Closing the page ends the conversation.
  useEffect(() => () => end(), [end])

  if (!open) return null
  const main = readiness?.main ?? ""
  const client = clientRef.current
  return (
    <div className="fixed inset-0 z-[80] flex flex-col justify-end bg-[rgba(7,7,9,0.8)]" onClick={(e) => { if (e.target === e.currentTarget) close() }}>
      <div className="mx-auto w-full max-w-[440px]">
        {readiness === null ? (
          <VoiceSheet view={initialVoiceView} main="" onClose={close} />
        ) : (
          <VoiceSheet view={view} main={main} readiness={readiness.ready ? undefined : readiness}
            onListen={() => void listen()}
            onSend={() => client?.send()}
            onCancel={() => client?.cancel()}
            onStop={() => client?.stop()}
            onReply={() => void client?.reply()}
            onClose={close}
            onOpenConversation={() => void openConversation()}
            onOpenMain={role === "worker" && shellStreams() ? () => { openMainVoice(); close() } : undefined}
            onOpenSettings={role !== "worker" && readiness.setting ? () => { close(); navigate(`/settings#setting-${readiness.setting}`) } : undefined}
          />
        )}
      </div>
    </div>
  )
}

// VoiceButton is the ring button that opens the voice sheet; it shows only
// where the page can hold a conversation.
export function VoiceButton({ className }: { className?: string }) {
  const reeve = useReeveName()
  if (voiceAudio("") === null) return null
  return (
    <button type="button" onClick={(e) => { e.stopPropagation(); openVoice() }} className={className} aria-label={`Talk to ${reeve.mid}`} title={`Talk to ${reeve.mid}`}>
      <VoiceRing mode="idle" size={22} label={`Talk to ${reeve.mid}`} />
    </button>
  )
}
