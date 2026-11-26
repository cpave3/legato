import { describe, expect, it } from "vitest"
import { centerOn, fitView, inView, openingView } from "../graphViewport"

const frame = { width: 1000, height: 600 }
const none = { top: 0, right: 0, bottom: 0, left: 0 }
const box = (x: number, width: number) => ({ x, y: 0, width, height: 100 })
// A 13-step chain, about 4,000px wide, and the cards' column on the left.
const chain = [box(0, 220), box(3760, 240)]
const leftCards = { ...none, left: 376 }

describe("fitView", () => {
  it("centres a flow that fits at full size in the free area", () => {
    expect(fitView([box(0, 400)], frame, none)).toEqual({ x: 300, y: 250, zoom: 1 })
  })

  it("centres it beside the cards, not under them", () => {
    expect(fitView([box(0, 400)], frame, { ...none, left: 400 })).toEqual({ x: 500, y: 250, zoom: 1 })
  })

  it("shrinks a long flow to fit the free area", () => {
    const view = fitView([box(0, 220), box(1760, 240)], frame, leftCards)
    expect(view.zoom).toBeCloseTo(576 / 2000)
    expect(view.x).toBeCloseTo(400)
  })

  it("never shrinks below the graph's least zoom", () => {
    expect(fitView(chain, frame, leftCards).zoom).toBe(0.2)
  })
})

describe("openingView", () => {
  it("fits a flow that stays readable", () => {
    expect(openingView([box(0, 400)], frame, { insets: leftCards, floor: 0.75 })).toEqual({ x: 488, y: 250, zoom: 1 })
  })

  it("opens a long run at 80% on the step it is at, in the free area", () => {
    expect(openingView(chain, frame, { insets: leftCards, floor: 0.75, focus: { x: 2640, y: 0, width: 240, height: 100 } })).toEqual({ x: -1520, y: 260, zoom: 0.8 })
  })

  it("opens a long flow with nothing to focus at its start, at the readable floor", () => {
    expect(openingView(chain, frame, { insets: leftCards, floor: 0.75 })).toEqual({ x: 400, y: 262.5, zoom: 0.75 })
  })
})

describe("centerOn", () => {
  it("puts a node at the centre of the free area at the zoom given", () => {
    expect(centerOn({ x: 100, y: 0, width: 200, height: 100 }, frame, { ...none, right: 400 }, 1)).toEqual({ x: 100, y: 250, zoom: 1 })
  })
})

describe("inView", () => {
  const view = { x: 0, y: 0, zoom: 1 }
  it("is true for a node wholly in the free area", () => {
    expect(inView({ x: 100, y: 100, width: 200, height: 100 }, view, frame, { ...none, right: 400 })).toBe(true)
  })

  it("is false for a node under the side panel", () => {
    expect(inView({ x: 500, y: 100, width: 200, height: 100 }, view, frame, { ...none, right: 400 })).toBe(false)
  })
})
