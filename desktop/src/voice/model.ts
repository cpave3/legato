import { initialVoiceView, reduceVoice, type VoiceView } from "@web-voice/session"
import { idleLine } from "@web-voice/copy"

// What the voice widget draws, from the app's voice-* events
// (src-tauri/src/voiceclient.rs, docs/ai/desktop.md § Talking to the
// Reeve). The window holds no token: Rust talks to the main and tells it.

export interface Readiness {
  ready: boolean
  reason: string
  setting: string
  main: string
}

// idle: the capsule, ready; connecting: the capsule while the main is asked;
// not_ready: the amber capsule; talking: the card; ended: the card saying
// how the conversation ended, until it is closed.
export type Phase = "idle" | "connecting" | "not_ready" | "talking" | "ended"

export interface WidgetModel {
  phase: Phase
  view: VoiceView
  readiness: Readiness
  // The bound key as the person reads it (Meta+Alt+Space), "" when none.
  keyText: string
}

export const initialModel: WidgetModel = {
  phase: "idle",
  view: initialVoiceView,
  readiness: { ready: true, reason: "", setting: "", main: "" },
  keyText: "",
}

export type VoiceEvent =
  | { name: "voice-state"; payload: { state: string; reason?: string | null; setting?: string | null } }
  | { name: "voice-caption"; payload: { kind: "partial" | "heard" | "activity" | "written"; text: string } }
  | { name: "voice-line"; payload: { id: string; text: string; status: "queued" | "playing" | "played" | "dropped" } }
  | { name: "voice-pause"; payload: { ms: number; of: number } }
  | { name: "voice-level"; payload: { level: number } }
  | { name: "voice-key-changed"; payload: { key?: string | null } }

export const EVENT_NAMES = ["voice-state", "voice-caption", "voice-line", "voice-pause", "voice-level", "voice-key-changed"] as const

// The main's own reasons for going idle, which idleLine says for a person.
const IDLE_REASONS = new Set(["no_speech", "nothing_heard", "no_answer", "failed", "cancelled"])

// reduce applies one of the app's events to the widget.
export function reduce(model: WidgetModel, event: VoiceEvent): WidgetModel {
  const view = model.view
  switch (event.name) {
    case "voice-state": {
      const { state, reason, setting } = event.payload
      switch (state) {
        case "connecting":
          return { ...model, phase: "connecting", view: initialVoiceView, readiness: { ...model.readiness, main: reason || model.readiness.main } }
        case "not_ready":
          return { ...model, phase: "not_ready", readiness: { ...model.readiness, ready: false, reason: reason ?? "", setting: setting ?? "" } }
        case "listening":
        case "thinking":
        case "speaking":
          return { ...model, phase: "talking", readiness: { ...model.readiness, ready: true, reason: "", setting: "" }, view: reduceVoice(view, { type: "state", state }) }
        case "idle": {
          const why = reason ?? ""
          // The person ended it, or the Reeve said its piece: nothing to say.
          if ((why === "bye" || why === "cancelled" || why === "done") && !view.written) return { ...model, phase: "idle", view: initialVoiceView }
          const known = IDLE_REASONS.has(why)
          const ended: VoiceView = { ...view, state: "idle", pause: 0, lines: [], speaking: "", reason: known ? why : "", ended: known || !why ? "" : why }
          return { ...model, phase: "ended", view: ended }
        }
      }
      return model
    }
    case "voice-caption": {
      const { kind, text } = event.payload
      if (kind === "written") return { ...model, view: reduceVoice(view, { type: "written" }) }
      return { ...model, view: reduceVoice(view, { type: kind, text }) }
    }
    case "voice-line": {
      const { id, text, status } = event.payload
      switch (status) {
        case "queued":
          return { ...model, view: { ...view, lines: [...view.lines, { id, text, url: "" }] } }
        case "playing":
          return { ...model, view: { ...view, speaking: id } }
        default:
          return { ...model, view: { ...view, lines: view.lines.filter((l) => l.id !== id), speaking: view.speaking === id ? "" : view.speaking } }
      }
    }
    case "voice-pause":
      return { ...model, view: reduceVoice(view, { type: "pause", ms: event.payload.ms, of: event.payload.of }) }
    case "voice-level":
      return { ...model, view: { ...view, level: event.payload.level } }
    case "voice-key-changed":
      return { ...model, keyText: event.payload.key ?? "" }
  }
  return model
}

// What the window shows: the capsule at rest, the card in a conversation
// and after one that ended with something to say.
export function surface(model: WidgetModel): "capsule" | "card" {
  if (model.phase === "talking") return "card"
  if (model.phase === "ended" && (idleLine(model.view) || model.view.written)) return "card"
  return "capsule"
}

// The key as the capsule's key caps: Meta+Alt+Space is three, ⌃⇧Space one.
export function keyCaps(text: string): string[] {
  if (!text) return []
  return text.includes("+") ? text.split("+") : [text]
}
