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
// not_ready: the amber capsule; talking: the card (also idle in a
// conversation kept after Stop); ended: the card saying
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
          // No reason: the person stopped the Reeve and the app keeps the
          // conversation; the card stays, to talk again or close.
          if (!why) return { ...model, phase: "talking", view: { ...reduceVoice(view, { type: "state", state: "idle" }), lines: [], speaking: "" } }
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

// How much of each part of a conversation the card shows, in characters:
// enough for a few lines, so a long dictation never grows the widget past
// the screen. The conversation keeps the whole text; the widget only the
// latest words.
export const CAP = { partial: 180, heard: 140, activity: 60, lines: 180 }

// tail is text's last max characters, from the start of a word, after an
// ellipsis; text itself when it fits.
export function tail(text: string, max: number): string {
  if (text.length <= max) return text
  let end = text.slice(-max)
  const space = end.search(/\s/)
  if (space > 0 && space < max / 3) end = end.slice(space)
  return `…${end.trimStart()}`
}

// capped is the view as the widget draws it: each part cut to its latest
// words, and of the Reeve's lines the one playing (cut to its end) and as
// many to come as fit after it.
export function capped(view: VoiceView): VoiceView {
  const lit = Math.max(0, view.lines.findIndex((l) => l.id === view.speaking))
  const lines: VoiceView["lines"] = []
  let room = CAP.lines
  for (const [i, line] of view.lines.slice(lit).entries()) {
    if (i === 0) {
      const text = tail(line.text, CAP.lines)
      lines.push({ ...line, text })
      room -= text.length
    } else if (line.text.length + 1 <= room) {
      lines.push(line)
      room -= line.text.length + 1
    } else {
      break
    }
  }
  return {
    ...view,
    partial: tail(view.partial, CAP.partial),
    heard: tail(view.heard, CAP.heard),
    activity: tail(view.activity, CAP.activity),
    ended: tail(view.ended, CAP.heard),
    lines,
  }
}
