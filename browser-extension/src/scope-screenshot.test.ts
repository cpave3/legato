import { describe, expect, it, vi } from "vitest"
import { blobToDataUrl, encodeWithinLimit, planCrop } from "./scope-screenshot"

const viewport = { width: 1000, height: 800 }

describe("planCrop", () => {
  it("crops around the element with a margin, at the capture's pixel ratio", () => {
    const plan = planCrop({ box: { x: 100, y: 200, width: 50, height: 20 }, viewport, image: { width: 2000, height: 1600 }, margin: 24 })
    expect(plan).toEqual({
      source: { x: 152, y: 352, width: 196, height: 136 },
      output: { width: 196, height: 136 },
      outline: { x: 48, y: 48, width: 100, height: 40 },
    })
  })

  it("stops at the edges of the visible page", () => {
    const plan = planCrop({ box: { x: -30, y: 790, width: 100, height: 40 }, viewport, image: { width: 1000, height: 800 }, margin: 24 })
    expect(plan?.source).toEqual({ x: 0, y: 766, width: 94, height: 34 })
    // The outline is cut where the element leaves the picture.
    expect(plan?.outline).toEqual({ x: 0, y: 24, width: 70, height: 10 })
  })

  it("scales a wide crop down to 1024 pixels wide", () => {
    const plan = planCrop({ box: { x: 0, y: 0, width: 1000, height: 100 }, viewport, image: { width: 2000, height: 1600 }, margin: 24 })
    expect(plan?.source).toEqual({ x: 0, y: 0, width: 2000, height: 248 })
    expect(plan?.output).toEqual({ width: 1024, height: 127 })
    expect(plan?.outline).toEqual({ x: 0, y: 0, width: 1024, height: 102 })
  })

  it("reads the ratio from the picture, so page zoom does not throw it off", () => {
    const plan = planCrop({ box: { x: 100, y: 100, width: 10, height: 10 }, viewport, image: { width: 1500, height: 1200 }, margin: 0 })
    expect(plan?.source).toEqual({ x: 150, y: 150, width: 15, height: 15 })
  })

  it("gives nothing for an element outside the visible page or without size", () => {
    expect(planCrop({ box: { x: 100, y: 2000, width: 10, height: 10 }, viewport, image: { width: 1000, height: 800 }, margin: 24 })).toBeNull()
    expect(planCrop({ box: { x: 100, y: 100, width: 0, height: 0 }, viewport, image: { width: 1000, height: 800 }, margin: 0 })).toBeNull()
  })
})

describe("encodeWithinLimit", () => {
  it("lowers the quality, then the size, until the picture fits", async () => {
    const sizes: Record<string, number> = { "0.85@1": 900, "0.7@1": 700, "0.55@1": 600, "0.4@1": 550, "0.85@0.75": 520, "0.7@0.75": 480 }
    const encode = vi.fn(async (quality: number, scale: number) => new Blob([new Uint8Array(sizes[`${quality}@${scale}`] ?? 10)]))
    const blob = await encodeWithinLimit(encode, 500)
    expect(blob?.size).toBe(480)
    expect(encode).toHaveBeenLastCalledWith(0.7, 0.75)
  })

  it("gives nothing when no attempt fits", async () => {
    expect(await encodeWithinLimit(async () => new Blob([new Uint8Array(600)]), 500)).toBeNull()
  })
})

describe("blobToDataUrl", () => {
  it("writes a blob as a base64 data URL", async () => {
    const blob = new Blob([new Uint8Array([255, 216, 255])], { type: "image/jpeg" })
    expect(await blobToDataUrl(blob)).toBe("data:image/jpeg;base64,/9j/")
  })
})
