import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import "@testing-library/jest-dom/vitest"
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { CAP, capped, initialModel, keyCaps, reduce, surface, tail, type VoiceEvent, type WidgetModel } from "./model"
import { DRAG_FROM, useWidgetDrag } from "./drag"

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
    if (cmd === "voice_widget_layout") return Promise.resolve({ width: 480, height: 600, anchor: "top-right" })
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

describe("a long transcript", () => {
  const words = (n: number) => Array.from({ length: n }, (_, i) => `word${i}`).join(" ")

  it("keeps only its end, after an ellipsis, from the start of a word", () => {
    expect(tail("turn the lights off", 40)).toBe("turn the lights off")
    const long = words(100)
    const t = tail(long, 60)
    expect(t.startsWith("…")).toBe(true)
    expect(long.endsWith(t.slice(1))).toBe(true)
    expect(t.length).toBeLessThanOrEqual(61)
    expect(t.slice(1)).toMatch(/^word\d+ /)
  })

  it("is cut to the latest words in every part the card shows", () => {
    const long = words(200)
    const view = capped({ ...initialModel.view, partial: long, heard: long, activity: long, ended: long })
    expect(view.partial.length).toBeLessThanOrEqual(CAP.partial + 1)
    expect(view.heard.length).toBeLessThanOrEqual(CAP.heard + 1)
    expect(view.activity.length).toBeLessThanOrEqual(CAP.activity + 1)
    expect(view.ended.length).toBeLessThanOrEqual(CAP.heard + 1)
    expect(view.partial.endsWith("word199")).toBe(true)
  })

  it("speaks the line playing, and leaves the lines to come that do not fit for later", () => {
    const lines = [
      { id: "a", text: words(80), url: "" },
      { id: "b", text: "Done.", url: "" },
      { id: "c", text: words(30), url: "" },
    ]
    const view = capped({ ...initialModel.view, state: "speaking", lines, speaking: "a" })
    expect(view.lines.map((l) => l.id)).toEqual(["a"])
    expect(view.lines[0].text.length).toBeLessThanOrEqual(CAP.lines + 1)
    const short = capped({ ...initialModel.view, state: "speaking", lines: [{ id: "a", text: "Yes.", url: "" }, { id: "b", text: "Done.", url: "" }], speaking: "a" })
    expect(short.lines.map((l) => l.text)).toEqual(["Yes.", "Done."])
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

  it("shows the end of a long dictation, not the start", async () => {
    render(<Widget />)
    await screen.findByText(/tessitura · ready/)
    emit("voice-state", { state: "listening" })
    const said = Array.from({ length: 120 }, (_, i) => `w${i}`).join(" ")
    emit("voice-caption", { kind: "partial", text: said })
    const card = screen.getByTestId("voice-card")
    expect(card).toHaveTextContent("w119")
    expect(card).toHaveTextContent("…")
    expect(card).not.toHaveTextContent(/\bw0\b/)
  })

  it("is dragged from the capsule, and a drag is not a click", async () => {
    render(<Widget />)
    await screen.findByText(/tessitura · ready/)
    const capsule = screen.getByTestId("voice-capsule")
    fireEvent.pointerDown(capsule, { pointerId: 1, button: 0, clientX: 10, clientY: 10 })
    fireEvent.pointerMove(capsule, { pointerId: 1, clientX: 40, clientY: 25 })
    await act(async () => {})
    fireEvent.pointerUp(capsule, { pointerId: 1, clientX: 40, clientY: 25 })
    fireEvent.click(capsule)
    await act(async () => {})
    const drags = invoked.filter((i) => i.cmd.startsWith("voice_widget_drag"))
    expect(drags).toEqual([
      { cmd: "voice_widget_drag_start", args: undefined },
      { cmd: "voice_widget_drag_move", args: { dx: 30, dy: 15 } },
      { cmd: "voice_widget_drag_end", args: undefined },
    ])
    expect(actions()).toEqual([])
    // The next click talks again.
    fireEvent.click(capsule)
    expect(actions()).toEqual(["talk"])
  })

  it("keeps to the most Rust says fits on the screen, aligned to its anchor's side", async () => {
    const { container } = render(<Widget />)
    await screen.findByText(/tessitura · ready/)
    const root = container.querySelector(".voice-widget") as HTMLElement
    expect(root).toHaveAttribute("data-anchor", "top-right")
    expect(root.style.getPropertyValue("--widget-max-h")).toBe("600px")
    expect(root.style.getPropertyValue("--widget-max-w")).toBe("480px")
    // Dropped at the bottom of a smaller screen.
    emit("voice-widget-layout", { width: 400, height: 380, anchor: "bottom" })
    expect(root).toHaveAttribute("data-anchor", "bottom")
    expect(root.style.getPropertyValue("--widget-max-h")).toBe("380px")
  })

  it("asks Rust for its menu on a right click", async () => {
    render(<Widget />)
    await screen.findByText(/tessitura · ready/)
    fireEvent.contextMenu(screen.getByTestId("voice-capsule"))
    expect(invoked.map((i) => i.cmd)).toContain("voice_widget_menu")
  })

  it("shows approvals as they arrive, above the capsule, with no conversation", async () => {
    render(<Widget />)
    await screen.findByText(/tessitura · ready/)
    expect(screen.queryByTestId("widget-approvals")).not.toBeInTheDocument()
    emit("voice-approvals", [{
      server: "s1",
      server_name: "tessitura",
      approval: { id: "ap-1", source: "reeve_change_set", title: "Run a command on the host", signed: true, options: [{ id: "allow", label: "Approve", kind: "allow" }], details: {} },
    }])
    expect(screen.getByTestId("widget-approvals")).toHaveTextContent("Reeve: Run a command on the host")
    expect(screen.getByTestId("voice-capsule")).toBeInTheDocument()
  })
})

describe("dragging", () => {
  beforeEach(() => { invoked.length = 0 })
  afterEach(cleanup)

  function Box() {
    const drag = useWidgetDrag()
    return (
      <div data-testid="box" {...drag}>
        <button type="button" onClick={() => invoked.push({ cmd: "clicked" })}>Talk</button>
        <div data-no-drag><button type="button" onClick={() => invoked.push({ cmd: "approved" })}>Approve</button></div>
      </div>
    )
  }
  const cmds = () => invoked.map((i) => i.cmd)

  it("is a click when the pointer moves less than the drag distance", () => {
    render(<Box />)
    const talk = screen.getByText("Talk")
    fireEvent.pointerDown(talk, { pointerId: 1, button: 0, clientX: 10, clientY: 10 })
    fireEvent.pointerMove(talk, { pointerId: 1, clientX: 10 + DRAG_FROM - 1, clientY: 10 })
    fireEvent.pointerUp(talk, { pointerId: 1 })
    fireEvent.click(talk)
    expect(cmds()).toEqual(["clicked"])
  })

  it("never starts from inside [data-no-drag]", () => {
    render(<Box />)
    const approve = screen.getByText("Approve")
    fireEvent.pointerDown(approve, { pointerId: 1, button: 0, clientX: 10, clientY: 10 })
    fireEvent.pointerMove(approve, { pointerId: 1, clientX: 80, clientY: 60 })
    fireEvent.pointerUp(approve, { pointerId: 1 })
    fireEvent.click(approve)
    expect(cmds()).toEqual(["approved"])
  })

  it("ignores the right button, which opens the menu", () => {
    render(<Box />)
    const talk = screen.getByText("Talk")
    fireEvent.pointerDown(talk, { pointerId: 1, button: 2, clientX: 10, clientY: 10 })
    fireEvent.pointerMove(talk, { pointerId: 1, clientX: 80, clientY: 60 })
    expect(cmds()).toEqual([])
  })

  it("sends one move at a time, each from where the widget took hold", async () => {
    render(<Box />)
    const box = screen.getByTestId("box")
    fireEvent.pointerDown(box, { pointerId: 1, button: 0, clientX: 10, clientY: 10 })
    fireEvent.pointerMove(box, { pointerId: 1, clientX: 30, clientY: 10 })
    // Still moving the window: this one is stale, and dropped.
    fireEvent.pointerMove(box, { pointerId: 1, clientX: 50, clientY: 10 })
    await act(async () => {})
    // The window has moved 20: the pointer is 5 past where it took hold.
    fireEvent.pointerMove(box, { pointerId: 1, clientX: 15, clientY: 12 })
    await act(async () => {})
    fireEvent.pointerUp(box, { pointerId: 1 })
    await act(async () => {})
    expect(invoked.filter((i) => i.cmd === "voice_widget_drag_move").map((i) => i.args)).toEqual([{ dx: 20, dy: 0 }, { dx: 5, dy: 2 }])
    expect(cmds().at(-1)).toBe("voice_widget_drag_end")
  })
})
