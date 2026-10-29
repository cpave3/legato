// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest"
import { loadLegatoFonts } from "./fonts"

class FakeFontFace {
  constructor(public family: string, public source: string, public descriptors: { weight: string }) {}
  load(): Promise<this> { return Promise.resolve(this) }
}

describe("loadLegatoFonts", () => {
  afterEach(() => vi.unstubAllGlobals())

  it("adds the extension's Geist faces to the page once, under names of their own", async () => {
    const added: FakeFontFace[] = []
    vi.stubGlobal("FontFace", FakeFontFace)
    vi.stubGlobal("chrome", { runtime: { getURL: (path: string) => `chrome-extension://legato/${path}` } })
    const doc = { fonts: { add: (face: FakeFontFace) => added.push(face) } } as unknown as Document

    loadLegatoFonts(doc)
    loadLegatoFonts(doc)
    await vi.waitFor(() => expect(added).toHaveLength(5))

    expect(added.map((face) => `${face.family} ${face.descriptors.weight}`)).toEqual([
      "Legato Geist 400", "Legato Geist 500", "Legato Geist 600", "Legato Geist Mono 400", "Legato Geist Mono 500",
    ])
    expect(added[0].source).toBe('url("chrome-extension://legato/fonts/geist-sans-latin-400-normal.woff2")')
  })

  it("leaves the page alone outside the extension", () => {
    expect(() => loadLegatoFonts(document)).not.toThrow()
  })
})
