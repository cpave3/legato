import { afterEach, describe, expect, it, vi } from "vitest"
import "@testing-library/jest-dom/vitest"
import { act, cleanup, render, screen } from "@testing-library/react"

// The app's events, as Rust emits them.
const handlers = new Map<string, (e: { payload: unknown }) => void>()

vi.mock("@tauri-apps/api/event", () => ({
  listen: (name: string, handler: (e: { payload: unknown }) => void) => {
    handlers.set(name, handler)
    return Promise.resolve(() => handlers.delete(name))
  },
}))
vi.mock("@tauri-apps/api/core", () => ({
  invoke: (cmd: string) => {
    if (cmd === "voice_widget_settings") return Promise.resolve({ shown: true, anchor: "bottom-right", dropped: false, screens: [] })
    return Promise.resolve(null)
  },
}))

const { VoiceSection } = await import("./VoiceSection")

afterEach(cleanup)

describe("Show the Legato widget", () => {
  it("follows the widget when the tray, the × or its menu hides or shows it", async () => {
    render(<VoiceSection />)
    const box = await screen.findByRole("checkbox", { name: "Show the Legato widget" })
    expect(box).toBeChecked()
    // set_widget's event, whichever way the widget was hidden.
    act(() => handlers.get("voice-widget-changed")?.({ payload: { shown: false, anchor: "bottom-right", dropped: false, screens: [] } }))
    expect(box).not.toBeChecked()
    act(() => handlers.get("voice-widget-changed")?.({ payload: { shown: true, anchor: "bottom-right", dropped: false, screens: [] } }))
    expect(box).toBeChecked()
  })
})
