import { useEffect, useLayoutEffect, useRef, useState } from "react"
import { invoke } from "@tauri-apps/api/core"
import { listen } from "@tauri-apps/api/event"
import { VoiceCapsule, VoiceCard } from "@web-voice/VoiceWidgets"
import { capped, EVENT_NAMES, initialModel, keyCaps, reduce, surface, type VoiceEvent, type WidgetModel } from "./model"
import { useWidgetDrag } from "./drag"

// The voice window's page: the capsule at rest, the card in a
// conversation. Rust tells it what to draw (voice-* events) and does what
// its buttons ask (voice_action); it holds no token.

type Status = { ready: boolean; reason: string; setting: string; main: string }
type KeyInfo = { key?: string | null }

// How often the capsule asks whether the main can listen, while at rest.
export const STATUS_EVERY = 30_000
// How long a card that says how a conversation ended stays.
export const ENDED_FOR = 6_000

const act = (action: string) => () => { invoke("voice_action", { action }).catch(() => {}) }

export function Widget() {
  const [model, setModel] = useState<WidgetModel>(initialModel)
  const apply = (event: VoiceEvent) => setModel((m) => reduce(m, event))

  useEffect(() => {
    const offs = EVENT_NAMES.map((name) => listen(name, (e) => apply({ name, payload: e.payload } as VoiceEvent)))
    const status = () => {
      invoke<Status>("voice_status")
        .then((s) => setModel((m) => (m.phase === "idle" || m.phase === "not_ready"
          ? { ...m, phase: s.ready ? "idle" : "not_ready", readiness: s }
          : { ...m, readiness: { ...m.readiness, main: s.main || m.readiness.main } })))
        .catch(() => {})
    }
    status()
    const timer = setInterval(status, STATUS_EVERY)
    const offTargets = listen("voice-targets-changed", status)
    invoke<KeyInfo>("voice_key_info").then((k) => setModel((m) => ({ ...m, keyText: k.key ?? "" }))).catch(() => {})
    return () => {
      clearInterval(timer)
      for (const off of [...offs, offTargets]) off.then((f) => f())
    }
  }, [])

  // A card that says how the conversation ended goes after a while.
  useEffect(() => {
    if (model.phase !== "ended") return
    const timer = setTimeout(() => setModel((m) => (m.phase === "ended" ? { ...m, phase: "idle" } : m)), ENDED_FOR)
    return () => clearTimeout(timer)
  }, [model.phase])

  // The window is sized to what it draws, so no transparent margin takes clicks.
  const box = useRef<HTMLDivElement>(null)
  useLayoutEffect(() => {
    const el = box.current
    if (!el || typeof ResizeObserver === "undefined") return
    const tell = () => {
      const r = el.getBoundingClientRect()
      invoke("voice_widget_size", { width: Math.ceil(r.width), height: Math.ceil(r.height) }).catch(() => {})
    }
    const observer = new ResizeObserver(tell)
    observer.observe(el)
    tell()
    return () => observer.disconnect()
  }, [])

  const menu = (e: React.MouseEvent) => {
    e.preventDefault()
    invoke("voice_widget_menu").catch(() => {})
  }

  // Dragging the widget places it; a click still talks.
  const drag = useWidgetDrag()

  const close = () => setModel((m) => ({ ...m, phase: "idle" }))
  const name = "The Reeve"
  return (
    <div ref={box} className="voice-widget" onContextMenu={menu} {...drag}>
      {surface(model) === "card" ? (
        <VoiceCard
          view={capped(model.view)}
          main={model.readiness.main}
          keyLabel={model.keyText || undefined}
          onListen={act("talk")}
          onSend={act("send")}
          onCancel={act("cancel")}
          onStop={act("stop")}
          onReply={act("reply")}
          onClose={model.phase === "ended" ? close : act("end")}
        />
      ) : (
        <VoiceCapsule
          name={name}
          readiness={model.phase === "not_ready" ? model.readiness : { ...model.readiness, ready: true }}
          keys={keyCaps(model.keyText)}
          onListen={act("talk")}
          onOpenSettings={() => { invoke("voice_open_settings", { setting: model.readiness.setting }).catch(() => {}) }}
        />
      )}
    </div>
  )
}
