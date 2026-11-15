/// <reference types="node" />
import { afterEach, describe, expect, it, vi } from "vitest"
import "@testing-library/jest-dom/vitest"
import { fireEvent, render, screen } from "@testing-library/react"
import { VoiceRing } from "../VoiceRing"
import { ReeveNameContext } from "../../lib/reeveName"
import { dialTicks } from "../ring"
import { VoiceCapsule, VoiceCard, VoiceSheet } from "../VoiceWidgets"
import { initialVoiceView, type VoiceView } from "../session"
import { applyAppTheme, setAppTheme } from "../../lib/appTheme"
import { readFileSync } from "node:fs"
const voiceCSS = readFileSync("src/voice/voice.css", "utf8")

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

  it("names this server's Reeve in its label", () => {
    render(<ReeveNameContext.Provider value="Tessitura"><VoiceRing mode="listening" /></ReeveNameContext.Provider>)
    expect(screen.getByRole("img", { name: "Tessitura's voice: listening" })).toBeInTheDocument()
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
    expect(screen.getByText("It failed.")).toHaveClass("text-[var(--voice-text)]")
    expect(screen.getByText(/Nothing is lost/)).toHaveClass("text-[var(--voice-faint)]")
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

// jsdom does not compute stylesheet custom properties or paint SVG. Inspect
// the rules matching the rendered surface; native/browser contrast is visual.
describe("voice surface theme bindings", () => {
  afterEach(() => {
    document.getElementById("voice-test-css")?.remove()
    localStorage.removeItem("legato:app-theme")
    applyAppTheme()
  })
  it("binds sheet text, surface and action ink to semantic roles, retaining default fallbacks", async () => {
    localStorage.setItem("legato:app-theme", "ocular-light")
    applyAppTheme()
    const style = document.createElement("style")
    style.id = "voice-test-css"
    style.textContent = voiceCSS
    document.head.append(style)
    const { rerender } = render(<VoiceSheet view={view({ state: "listening", partial: "Check the build" })} main="main" onSend={() => {}} onCancel={() => {}} />)
    const sheet = screen.getByRole("dialog")
    const css = Array.from(style.sheet!.cssRules).find(rule => rule.type === 1 && sheet.matches((rule as CSSStyleRule).selectorText)) as CSSStyleRule
    expect(css.style.getPropertyValue("--voice-accent")).toBe("var(--theme-accent,#6d5ef6)")
    expect(css.style.getPropertyValue("--voice-link")).toBe("var(--theme-identity,var(--theme-link,#a99cff))")
    expect(css.style.getPropertyValue("--voice-text")).toBe("var(--theme-text,#e6e5eb)")
    expect(css.style.getPropertyValue("--voice-surface")).toBe("var(--theme-surface,linear-gradient(#15141c,#111114))")
    expect(getComputedStyle(screen.getByRole("button", { name: "Send now" })).color).toBe("var(--theme-canvas, #fff)")
    expect(screen.getByText("Check the build")).toBeInTheDocument()
    const surface = () => document.documentElement.style.getPropertyValue("--theme-surface")
    const lightSurface = surface()
    expect(lightSurface).toBe(document.documentElement.style.getPropertyValue("--color-lw-surface"))
    await setAppTheme("dracula")
    expect(surface()).not.toBe(lightSurface)
    expect(surface()).toBe(document.documentElement.style.getPropertyValue("--color-lw-surface"))
    expect(screen.getByRole("dialog")).toBe(sheet)
    expect(sheet.className).not.toMatch(/(?:text|border|bg)-\[#[0-9a-f]+/i)
    rerender(<VoiceCard view={view({ state: "thinking", heard: "Check the build", activity: "reading logs" })} main="main" />)
    expect(screen.getByText("reading logs…")).toHaveClass("text-[var(--voice-link)]")
    expect(screen.getByText("“Check the build”")).toHaveClass("text-[var(--voice-dim)]")
  })

  it.each(["idle", "blocked", "listening", "thinking", "speaking"] as const)("binds the %s gauge and ticks to semantic roles", mode => {
    render(<VoiceRing mode={mode} />)
    const ring = screen.getByRole("img")
    const role = mode === "speaking" ? "success" : mode === "blocked" ? "warning" : "focus"
    expect(ring.querySelector("text")?.getAttribute("fill")).toContain(`var(--theme-${role},`)
    const ticks = ring.querySelectorAll(".tick")
    for (const tick of ticks) expect(tick.getAttribute("stroke")).toMatch(mode === "speaking" ? /^color-mix\(in srgb, var\(--theme-voice-start,/ : /^var\(--theme-/)
    if (mode === "speaking") {
      expect(ticks[0].getAttribute("stroke")).toContain("100%")
      expect(ticks[59].getAttribute("stroke")).toContain("var(--theme-success,")
      expect(ticks[0].getAttribute("stroke")).not.toBe(ticks[59].getAttribute("stroke"))
    }
  })
})
