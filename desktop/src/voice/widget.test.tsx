import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import "@testing-library/jest-dom/vitest"
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { initialModel, keyCaps, reduce, surface, type VoiceEvent, type WidgetModel } from "./model"

// The app's events, as Rust emits them, and what the page asks of Rust.
const handlers = new Map<string, (e: { payload: unknown }) => void>()
const invoked: { cmd: string; args?: unknown }[] = []
let status = { ready: true, reason: "", setting: "", main: "tessitura" }

vi.mock("@tauri-apps/api/event", () => ({
  listen: (name: string, handler: (e: { payload: unknown }) => void) => {
    handlers.set(name, handler)
    return Promise.resolve(() => handlers.delete(name))
  },
}))
vi.mock("@tauri-apps/api/core", () => ({
  invoke: (cmd: string, args?: unknown) => {
    invoked.push({ cmd, args })
    if (cmd === "voice_status") return Promise.resolve(status)
    if (cmd === "voice_key_info") return Promise.resolve({ key: "Meta+Alt+Space" })
    return Promise.resolve(null)
  },
}))

const { Widget } = await import("./Widget")

function emit(name: string, payload: unknown) {
  act(() => handlers.get(name)?.({ payload }))
}

const run = (...events: VoiceEvent[]): WidgetModel => events.reduce(reduce, initialModel)
const state = (s: string, reason?: string, setting?: string): VoiceEvent => ({ name: "voice-state", payload: { state: s, reason, setting } })

describe("what the widget draws", () => {
  it("is the capsule at rest and the card in a conversation", () => {
    expect(surface(initialModel)).toBe("capsule")
    expect(surface(run(state("connecting", "tessitura")))).toBe("capsule")
    for (const s of ["listening", "thinking", "speaking"]) expect(surface(run(state(s)))).toBe("card")
    expect(surface(run(state("not_ready", "Whisper isn't answering on tessitura", "voice.whisper_url")))).toBe("capsule")
  })

  it("says how a conversation ended, and nothing when the person ended it", () => {
    const heardNothing = run(state("listening"), state("idle", "no_speech"))
    expect(heardNothing.phase).toBe("ended")
    expect(surface(heardNothing)).toBe("card")
    expect(heardNothing.view.reason).toBe("no_speech")

    const takenOver = run(state("listening"), state("idle", "taken over by Pixel 9"))
    expect(takenOver.view.ended).toBe("taken over by Pixel 9")
    expect(surface(takenOver)).toBe("card")

    expect(surface(run(state("listening"), state("idle", "bye")))).toBe("capsule")
    expect(surface(run(state("speaking"), state("idle", "cancelled")))).toBe("capsule")
    // Answered in writing.
    expect(surface(run(state("thinking"), { name: "voice-caption", payload: { kind: "written", text: "" } }, state("idle", "done")))).toBe("card")
    expect(surface(run(state("speaking"), state("idle", "done")))).toBe("capsule")
  })

  it("keeps the card after the person stops the Reeve, ready to talk again", () => {
    // The app keeps the conversation and says idle with no reason.
    const stopped = run(state("speaking"), state("thinking"), state("idle"))
    expect(stopped.phase).toBe("talking")
    expect(stopped.view.state).toBe("idle")
    expect(surface(stopped)).toBe("card")
    expect(surface(run(state("speaking"), state("idle"), state("listening")))).toBe("card")
  })

  it("keeps the lines in order, the one playing lit", () => {
    const line = (id: string, status: "queued" | "playing" | "played" | "dropped"): VoiceEvent => ({ name: "voice-line", payload: { id, text: `Line ${id}.`, status } })
    let m = run(state("speaking"), line("a", "queued"), line("b", "queued"), line("a", "playing"))
    expect(m.view.lines.map((l) => l.id)).toEqual(["a", "b"])
    expect(m.view.speaking).toBe("a")
    m = reduce(m, line("a", "played"))
    expect(m.view.lines.map((l) => l.id)).toEqual(["b"])
    expect(m.view.speaking).toBe("")
  })

  it("follows the words, the pause bar and the level", () => {
    const m = run(
      state("listening"),
      { name: "voice-caption", payload: { kind: "partial", text: "turn the" } },
      { name: "voice-pause", payload: { ms: 500, of: 1000 } },
      { name: "voice-level", payload: { level: 0.4 } },
    )
    expect(m.view.partial).toBe("turn the")
    expect(m.view.pause).toBe(0.5)
    expect(m.view.level).toBe(0.4)
    const t = reduce(reduce(m, state("thinking")), { name: "voice-caption", payload: { kind: "heard", text: "turn the lights off" } })
    expect(t.view.heard).toBe("turn the lights off")
  })

  it("reads the key as key caps", () => {
    expect(keyCaps("Meta+Alt+Space")).toEqual(["Meta", "Alt", "Space"])
    expect(keyCaps("⌃⇧Space")).toEqual(["⌃⇧Space"])
    expect(keyCaps("")).toEqual([])
  })
})

describe("the widget page", () => {
  beforeEach(() => {
    handlers.clear()
    invoked.length = 0
    status = { ready: true, reason: "", setting: "", main: "tessitura" }
  })
  afterEach(cleanup)

  const actions = () => invoked.filter((i) => i.cmd === "voice_action").map((i) => (i.args as { action: string }).action)

  it("shows the ready capsule with the main and the key, and a click talks", async () => {
    render(<Widget />)
    expect(await screen.findByText(/tessitura · ready/)).toBeInTheDocument()
    expect(screen.getByLabelText("Key: Meta, Alt, Space")).toBeInTheDocument()
    fireEvent.click(screen.getByTestId("voice-capsule"))
    expect(actions()).toEqual(["talk"])
  })

  it("shows why it is not ready, and Open settings opens the main's setting", async () => {
    status = { ready: false, reason: "The Reeve's voice is off on tessitura", setting: "reeve.voice.enabled", main: "tessitura" }
    render(<Widget />)
    expect(await screen.findByText("The Reeve's voice is off on tessitura")).toBeInTheDocument()
    fireEvent.click(screen.getByText("Open settings"))
    expect(invoked).toContainEqual({ cmd: "voice_open_settings", args: { setting: "reeve.voice.enabled" } })
  })

  it("draws the card as the app's events come, with the bound key to send", async () => {
    render(<Widget />)
    await screen.findByText(/tessitura · ready/)
    emit("voice-state", { state: "listening" })
    expect(screen.getByTestId("voice-card")).toHaveAttribute("data-state", "listening")
    expect(screen.getByText("Meta+Alt+Space to send")).toBeInTheDocument()
    fireEvent.click(screen.getByText("Send now"))
    emit("voice-state", { state: "speaking" })
    emit("voice-line", { id: "a", text: "The lights are off.", status: "queued" })
    emit("voice-line", { id: "a", text: "The lights are off.", status: "playing" })
    expect(screen.getByTestId("voice-caption")).toHaveTextContent("The lights are off.")
    fireEvent.click(screen.getByText("Reply"))
    fireEvent.click(screen.getByText("Stop"))
    expect(actions()).toEqual(["send", "reply", "stop"])
    emit("voice-state", { state: "idle", reason: "bye" })
    expect(screen.getByTestId("voice-capsule")).toBeInTheDocument()
  })

  it("asks Rust for its menu on a right click", async () => {
    render(<Widget />)
    await screen.findByText(/tessitura · ready/)
    fireEvent.contextMenu(screen.getByTestId("voice-capsule"))
    expect(invoked.map((i) => i.cmd)).toContain("voice_widget_menu")
  })
})
