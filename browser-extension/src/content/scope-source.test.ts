// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest"
import { PROBE_ATTRIBUTE } from "../scope-source/types"
import { lookupSource } from "./scope-source"

afterEach(() => {
  vi.useRealTimers()
  document.body.innerHTML = ""
})

describe("lookupSource", () => {
  it("marks the element while the service worker reads it, then unmarks it", async () => {
    document.body.innerHTML = `<button>Save</button><span ${PROBE_ATTRIBUTE}>stale</span>`
    const button = document.querySelector("button")!
    const marked: Element[] = []
    const ask = vi.fn(async () => {
      marked.push(...document.querySelectorAll(`[${PROBE_ATTRIBUTE}]`))
      return { ok: true, source: { name: "SaveButton", file: "src/App.tsx", line: 7, confidence: "exact" } }
    })
    expect(await lookupSource(button, ask)).toEqual({ name: "SaveButton", file: "src/App.tsx", line: 7, confidence: "exact" })
    expect(ask).toHaveBeenCalledWith({ type: "scopeSource" })
    expect(marked).toEqual([button])
    expect(document.querySelectorAll(`[${PROBE_ATTRIBUTE}]`)).toHaveLength(0)
  })

  it("gives none when the service worker refuses or fails", async () => {
    document.body.innerHTML = `<button>Save</button>`
    const button = document.querySelector("button")!
    expect(await lookupSource(button, async () => ({ ok: false, error: "Scope is not allowed" }))).toEqual({ confidence: "none" })
    expect(await lookupSource(button, async () => { throw new Error("Extension context invalidated") })).toEqual({ confidence: "none" })
    expect(await lookupSource(button, async () => ({ ok: true, source: "src/App.tsx" }))).toEqual({ confidence: "none" })
  })

  it("does not wait past its bound", async () => {
    vi.useFakeTimers()
    document.body.innerHTML = `<button>Save</button>`
    const button = document.querySelector("button")!
    const looking = lookupSource(button, () => new Promise(() => undefined), 1500)
    await vi.advanceTimersByTimeAsync(1500)
    expect(await looking).toEqual({ confidence: "none" })
    expect(button.hasAttribute(PROBE_ATTRIBUTE)).toBe(false)
  })
})
