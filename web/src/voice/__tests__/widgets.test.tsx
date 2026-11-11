import { describe, expect, it, vi } from "vitest"
import "@testing-library/jest-dom/vitest"
import { fireEvent, render, screen } from "@testing-library/react"
import { VoiceRing } from "../VoiceRing"
import { dialTicks } from "../ring"
import { VoiceCapsule, VoiceCard, VoiceSheet } from "../VoiceWidgets"
import { initialVoiceView, type VoiceView } from "../session"

const view = (v: Partial<VoiceView>): VoiceView => ({ ...initialVoiceView, ...v })
const ready = { ready: true, reason: "", setting: "", main: "tessitura" }

function reducedMotion(on: boolean) {
  window.matchMedia = vi.fn().mockImplementation((query: string) => ({
    matches: on && query.includes("reduce"), media: query, onchange: null,
    addEventListener: () => {}, removeEventListener: () => {}, addListener: () => {}, removeListener: () => {}, dispatchEvent: () => false,
  }))
}

describe("the voice ring", () => {
  it("has sixty ticks in every mode", () => {
    for (const mode of ["idle", "listening", "thinking", "speaking", "blocked"] as const) {
      expect(dialTicks(mode, 88)).toHaveLength(60)
    }
  })

  it("follows the microphone's level while listening", () => {
    const length = (level: number) => dialTicks("listening", 88, level).reduce((n, t) => n + Math.hypot(t.x2 - t.x1, t.y2 - t.y1), 0)
    expect(length(0.9)).toBeGreaterThan(length(0.1))
  })

  it("draws each state, with the thinking arc only while thinking", () => {
    reducedMotion(false)
    const { rerender } = render(<VoiceRing mode="thinking" />)
    expect(screen.getByTestId("voice-arc")).toBeInTheDocument()
    rerender(<VoiceRing mode="blocked" />)
    expect(screen.getByRole("img", { name: /not ready/ })).toHaveAttribute("data-mode", "blocked")
    expect(screen.queryByTestId("voice-arc")).toBeNull()
  })

  it("keeps still when the person asks for reduced motion", () => {
    reducedMotion(true)
    render(<VoiceRing mode="speaking" />)
    expect(screen.getByRole("img")).toHaveAttribute("data-motion", "reduced")
    reducedMotion(false)
  })
})

describe("the capsule", () => {
  it("at rest names the Reeve, the main and the key, and talks when pressed", () => {
    const onListen = vi.fn()
    render(<VoiceCapsule name="Reeve" readiness={ready} keys={["Meta", "Alt", "Space"]} onListen={onListen} />)
    expect(screen.getByText("tessitura · ready")).toBeInTheDocument()
    expect(screen.getByLabelText("Key: Meta, Alt, Space")).toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Talk to Reeve" }))
    expect(onListen).toHaveBeenCalled()
  })

  it("not ready says why in one line, names the setting, and opens settings", () => {
    const onOpenSettings = vi.fn()
    render(<VoiceCapsule name="Reeve" readiness={{ ready: false, reason: "The Reeve's voice is off on tessitura", setting: "reeve.voice.enabled", main: "tessitura" }} onOpenSettings={onOpenSettings} />)
    expect(screen.getByText("The Reeve's voice is off on tessitura")).toBeInTheDocument()
    expect(screen.getByText("not recording · reeve.voice.enabled")).toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Open settings" }))
    expect(onOpenSettings).toHaveBeenCalled()
  })
})

describe("the card", () => {
  it("listening shows the words so far, the pause bar and the key to send", () => {
    const onSend = vi.fn()
    render(<VoiceCard view={view({ state: "listening", partial: "Can you check", pause: 0.38 })} main="main" keyLabel="Meta+Alt+Space" onSend={onSend} onCancel={() => {}} />)
    expect(screen.getByText("LISTENING")).toBeInTheDocument()
    expect(screen.getByText("Can you check")).toBeInTheDocument()
    expect(screen.getByTestId("voice-pause")).toHaveStyle({ width: "38%" })
    expect(screen.getByText("Meta+Alt+Space to send")).toBeInTheDocument()
    expect(screen.queryByText(/esc to cancel/)).toBeNull()
    fireEvent.click(screen.getByRole("button", { name: "Send now" }))
    expect(onSend).toHaveBeenCalled()
  })

  it("thinking shows what was sent and what the Reeve is doing", () => {
    render(<VoiceCard view={view({ state: "thinking", heard: "Why did it fail?", activity: "reading the workflow log" })} main="main" onStop={() => {}} onOpenConversation={() => {}} />)
    expect(screen.getByText("“Why did it fail?”")).toBeInTheDocument()
    expect(screen.getByText("reading the workflow log…")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Stop" })).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Open conversation" })).toBeInTheDocument()
  })

  it("speaking lights the line being said and dims those to come", () => {
    render(<VoiceCard view={view({ state: "speaking", speaking: "a", lines: [{ id: "a", text: "It failed.", url: "" }, { id: "b", text: "Nothing is lost.", url: "" }] })} main="main" onStop={() => {}} onReply={() => {}} />)
    expect(screen.getByText("It failed.")).toHaveClass("text-[#E6E5EB]")
    expect(screen.getByText(/Nothing is lost/)).toHaveClass("text-[#6E6D79]")
    expect(screen.getByRole("button", { name: "Reply" })).toBeInTheDocument()
  })

  it("says when the Reeve answered in writing", () => {
    render(<VoiceCard view={view({ state: "idle", written: true })} main="main" onOpenConversation={() => {}} />)
    expect(screen.getByText("Answered in writing")).toBeInTheDocument()
  })
})

describe("the phone sheet", () => {
  it("draws the ring at 176 px and each state's buttons", () => {
    const { rerender } = render(<VoiceSheet view={view({ state: "listening" })} main="main" onCancel={() => {}} onSend={() => {}} />)
    expect(screen.getByRole("img")).toHaveAttribute("width", "176")
    expect(screen.getByRole("button", { name: "Send now" })).toBeInTheDocument()
    rerender(<VoiceSheet view={view({ state: "speaking", lines: [{ id: "a", text: "Done.", url: "" }] })} main="main" onStop={() => {}} onReply={() => {}} />)
    expect(screen.getByRole("button", { name: "Reply" })).toBeInTheDocument()
  })

  it("not ready says why and offers the settings", () => {
    render(<VoiceSheet view={initialVoiceView} main="main" readiness={{ ready: false, reason: "Whisper isn't answering on main", setting: "voice.whisper_url", main: "main" }} onOpenSettings={() => {}} onClose={() => {}} />)
    expect(screen.getByTestId("voice-sheet")).toHaveAttribute("data-state", "blocked")
    expect(screen.getByText("Whisper isn't answering on main")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Open settings" })).toBeInTheDocument()
  })
})
