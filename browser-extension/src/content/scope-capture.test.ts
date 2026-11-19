// @vitest-environment jsdom
// @vitest-environment-options {"url":"http://localhost:5173/settings?tab=2"}
import { beforeEach, describe, expect, it } from "vitest"
import { capturePage, captureTarget, locateTarget, nthChildPath, resolveSelector } from "./scope-capture"

beforeEach(() => {
  document.title = "Settings"
  document.body.innerHTML = `
    <main>
      <section><p>One</p><p>Two <b>bold</b></p></section>
      <section><p>Three</p></section>
    </main>`
})

describe("the page a pin is on", () => {
  it("records its address, title, viewport, scroll and pixel ratio", () => {
    expect(capturePage(window)).toEqual({
      url: "http://localhost:5173/settings?tab=2",
      title: "Settings",
      viewport: { width: window.innerWidth, height: window.innerHeight },
      scroll: { x: 0, y: 0 },
      dpr: window.devicePixelRatio || 1,
    })
  })
})

describe("nthChildPath", () => {
  it("names an element by its place under body, which matches it alone", () => {
    const bold = document.querySelector("b")!
    const path = nthChildPath(bold)
    expect(path).toBe("body > main:nth-child(1) > section:nth-child(1) > p:nth-child(2) > b:nth-child(1)")
    expect([...document.querySelectorAll(path)]).toEqual([bold])
  })
})

describe("the pinned element", () => {
  it("records a selector, a text snippet and its box in page coordinates", () => {
    const p = document.querySelectorAll("p")[1]
    p.getBoundingClientRect = () => ({ left: 10, top: 20, width: 100, height: 18, right: 110, bottom: 38, x: 10, y: 20, toJSON: () => ({}) })
    window.scrollTo = () => undefined
    Object.defineProperty(window, "scrollY", { value: 300, configurable: true })
    const target = captureTarget(p)
    Object.defineProperty(window, "scrollY", { value: 0, configurable: true })
    expect(target.selectors.at(-1)).toBe(nthChildPath(p))
    expect(target.text).toBe("Two bold")
    expect(target.box).toEqual({ x: 10, y: 320, width: 100, height: 18 })
  })

  it("is found again by its selectors", () => {
    const p = document.querySelectorAll("p")[2]
    const target = captureTarget(p)
    expect(locateTarget(document, target)).toBe(p)
    expect(locateTarget(document, { ...target, selectors: ["#nowhere"], text: "" })).toBeNull()
  })

  it("resolves a selector only when it matches exactly one element", () => {
    expect(resolveSelector(document, "p")).toBeNull()
    expect(resolveSelector(document, "b")).toBe(document.querySelector("b"))
    expect(resolveSelector(document, "::not a selector")).toBeNull()
  })
})
