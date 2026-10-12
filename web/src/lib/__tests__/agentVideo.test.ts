import { describe, expect, it } from "vitest"
import { clock, describeMoment, momentAt, onScreen, parseVideoManifest, questionAbout, videoAddress, type VideoManifest } from "../agentVideo"

const manifest: VideoManifest = {
  version: 1,
  durationMs: 30000,
  chapters: [{ title: "Start", startMs: 0, endMs: 10000 }, { title: "The loop", startMs: 10000, endMs: 30000 }],
  scenes: [
    {
      id: "intro", kind: "card", startMs: 0, durationMs: 10000,
      source: { title: "Hello", subtitle: "A tour", bullets: ["one", "two", "three"] },
      actions: [{ ms: 2000, do: "reveal", index: 1 }, { ms: 6000, do: "reveal", index: 2 }],
      cues: [{ startMs: 350, endMs: 4000, text: "First." }, { startMs: 6000, endMs: 9000, text: "Second." }],
    },
    {
      id: "loop", kind: "code", startMs: 10000, durationMs: 12000,
      source: {
        file: "main.go", path: "/repo/main.go", title: "main.go",
        lines: [{ n: 10, text: "for i := range frames {" }, { n: 11, text: "\tseek(i)" }, { n: 12, text: "\tshoot()" }, { n: 13, text: "}" }],
      },
      actions: [{ ms: 14000, do: "highlight", lines: [11, 12] }, { ms: 20000, do: "clear" }],
      cues: [{ startMs: 10350, endMs: 15000, text: "It seeks, then shoots." }],
    },
    {
      id: "shot", kind: "image", startMs: 22000, durationMs: 8000,
      source: { image: "a.png", path: "/repo/a.png", title: "The result" },
      actions: [{ ms: 24000, do: "zoom", rect: [10, 20, 300, 200] }, { ms: 28000, do: "reset" }],
    },
  ],
}

describe("parseVideoManifest", () => {
  it("reads a manifest and gives it chapters when it has none", () => {
    const parsed = parseVideoManifest(JSON.stringify({ version: 1, durationMs: 4000, scenes: [{ id: "a", kind: "card", startMs: 0, durationMs: 4000 }] }))
    expect(parsed?.scenes).toHaveLength(1)
    expect(parsed?.chapters).toEqual([])
  })

  it.each([
    ["nothing", undefined],
    ["text that is not JSON", "version 1"],
    ["another version", JSON.stringify({ version: 2, durationMs: 4000, scenes: [{ id: "a" }] })],
    ["no scenes", JSON.stringify({ version: 1, durationMs: 4000, scenes: [] })],
    ["a list", "[1, 2]"],
  ])("returns null for %s", (_name, content) => {
    expect(parseVideoManifest(content)).toBeNull()
  })
})

describe("videoAddress", () => {
  const file = "/agent-ui-videos/task-1/" + "a".repeat(64) + ".mp4"

  it("is at the server the page talks to", () => {
    expect(videoAddress("http://legato.test:3080", { video: { url: file } })).toBe("http://legato.test:3080" + file)
  })

  it("is null for no video and for an address that is not a stored video", () => {
    expect(videoAddress("http://legato.test:3080", {})).toBeNull()
    expect(videoAddress("http://legato.test:3080", { video: { url: "/api/settings" } })).toBeNull()
    expect(videoAddress("http://legato.test:3080", { video: { url: "https://elsewhere.test" + file } })).toBeNull()
    expect(videoAddress("http://legato.test:3080", { video: { url: "/agent-ui-videos/../api/settings" } })).toBeNull()
  })
})

describe("momentAt", () => {
  it.each([
    [0, "intro", "Start", undefined],
    [1000, "intro", "Start", "First."],
    [5000, "intro", "Start", "First."], // in a pause, the last caption stands
    [9999, "intro", "Start", "Second."],
    [10000, "loop", "The loop", undefined],
    [16000, "loop", "The loop", "It seeks, then shoots."],
    [99999, "shot", "The loop", undefined], // past the end is the end
  ])("at %d ms finds the scene, the chapter and what is being said", (ms, scene, chapter, saying) => {
    const moment = momentAt(manifest, ms)
    expect(moment.scene.id).toBe(scene)
    expect(moment.chapter).toBe(chapter)
    expect(moment.saying).toBe(saying)
  })

  it("follows a scene's actions", () => {
    expect(momentAt(manifest, 1000).revealed).toBe(0)
    expect(momentAt(manifest, 7000).revealed).toBe(2)
    expect(momentAt(manifest, 13000).highlighted).toBeUndefined()
    expect(momentAt(manifest, 15000).highlighted).toEqual([11, 12])
    expect(momentAt(manifest, 21000).highlighted).toBeUndefined()
    expect(momentAt(manifest, 25000).zoomed).toEqual([10, 20, 300, 200])
    expect(momentAt(manifest, 29000).zoomed).toBeUndefined()
  })

  it("shows every bullet of a card that reveals none one by one", () => {
    const plain: VideoManifest = { ...manifest, scenes: [{ ...manifest.scenes[0], actions: [] }] }
    expect(momentAt(plain, 1000).revealed).toBe(3)
  })
})

describe("describeMoment", () => {
  it("gives the code that is highlighted, with a few rows around it", () => {
    const text = describeMoment(momentAt(manifest, 15000))
    expect(text).toContain('Video paused at 0:15 of 0:30, in the chapter "The loop", scene `loop`.')
    expect(text).toContain('Being said: "It seeks, then shoots."')
    expect(text).toContain("On screen: `/repo/main.go`, lines 10 to 13, with lines 11 to 12 highlighted.")
    expect(text).toContain("    10  for i := range frames {")
    expect(text).toContain(">   11  \tseek(i)")
    expect(text).toContain(">   12  \tshoot()")
    expect(text).toContain("The code is as it was when the video was rendered.")
  })

  it("lists only the bullets a card has shown so far", () => {
    const text = describeMoment(momentAt(manifest, 7000))
    expect(text).toContain('On screen: a title card, "Hello", with the subtitle "A tour".')
    expect(text).toContain("- one\n- two")
    expect(text).not.toContain("three")
  })

  it("says where an image is zoomed", () => {
    expect(describeMoment(momentAt(manifest, 25000))).toContain("On screen: the image `/repo/a.png` (The result), zoomed to x 10, y 20, 300 by 200 pixels.")
  })
})

describe("what the player says and asks", () => {
  it("names what is on screen in a few words", () => {
    expect(onScreen(momentAt(manifest, 15000))).toBe("main.go, lines 11 to 12")
    expect(onScreen(momentAt(manifest, 11000))).toBe("main.go")
    expect(onScreen(momentAt(manifest, 25000))).toBe("The result")
    expect(onScreen(momentAt(manifest, 1000))).toBe("Hello")
  })

  it("writes a time as minutes and seconds", () => {
    expect(clock(0)).toBe("0:00")
    expect(clock(83500)).toBe("1:23")
    expect(clock(600000)).toBe("10:00")
  })

  it("puts the moment before the place for the question", () => {
    const text = questionAbout("Tour of the change", momentAt(manifest, 15000))
    expect(text.startsWith('About the video "Tour of the change":\n\nVideo paused at 0:15')).toBe(true)
    expect(text.endsWith("\n\nMy question: ")).toBe(true)
  })
})
