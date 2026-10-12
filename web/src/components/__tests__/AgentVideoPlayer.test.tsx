import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { AgentUIItemView } from "../AgentUIItemView"
import { onComposerProposal, type AgentUIItem } from "../../lib/agentUi"

vi.mock("../../hooks/useServer", () => ({
  useServer: () => ({ baseUrl: "http://legato.test:3080" }),
}))

const FILE = "/agent-ui-videos/task-1/" + "a".repeat(64) + ".mp4"

const manifest = {
  version: 1, durationMs: 30000, captionsBurned: false,
  chapters: [{ title: "Start", startMs: 0, endMs: 10000 }, { title: "The loop", startMs: 10000, endMs: 30000 }],
  scenes: [
    { id: "intro", kind: "card", startMs: 0, durationMs: 10000, source: { title: "Hello" }, cues: [{ startMs: 350, endMs: 4000, text: "First." }] },
    {
      id: "loop", kind: "code", startMs: 10000, durationMs: 20000,
      source: { path: "/repo/main.go", title: "main.go", lines: [{ n: 10, text: "for i := range frames {" }, { n: 11, text: "\tseek(i)" }, { n: 12, text: "}" }] },
      actions: [{ ms: 14000, do: "highlight", lines: [11, 11] }],
      cues: [{ startMs: 10350, endMs: 15000, text: "It seeks to each frame." }],
    },
  ],
}

function item(overrides: Partial<AgentUIItem> = {}): AgentUIItem {
  return {
    id: "ui-0123456789", task_id: "task-1", placement: "panel", format: "video", title: "Tour of the change",
    revision: 1, oldest_revision: 1, content: JSON.stringify(manifest), video: { url: FILE },
    created_at: "2026-10-12 01:00:00", updated_at: "2026-10-12 01:00:00", ...overrides,
  }
}

// jsdom has a video element that plays nothing: the tests set its time and
// say that time passed.
function at(video: HTMLVideoElement, seconds: number) {
  video.currentTime = seconds
  fireEvent.timeUpdate(video)
}

beforeEach(() => {
  vi.spyOn(HTMLMediaElement.prototype, "play").mockResolvedValue(undefined)
  vi.spyOn(HTMLMediaElement.prototype, "pause").mockImplementation(() => {})
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

describe("AgentVideoPlayer", () => {
  it("plays the video from the server the page talks to", () => {
    render(<AgentUIItemView item={item()} fill />)

    const video = screen.getByLabelText("Tour of the change") as HTMLVideoElement
    expect(video.tagName).toBe("VIDEO")
    expect(video.src).toBe("http://legato.test:3080" + FILE)
    expect(video.controls).toBe(true)
  })

  it("shows the picture the video carries until it is played", () => {
    const poster = "/agent-ui-images/task-1/" + "b".repeat(64) + ".png"
    render(<AgentUIItemView item={item({ video: { url: FILE, poster } })} fill />)

    expect((screen.getByLabelText("Tour of the change") as HTMLVideoElement).poster).toBe("http://legato.test:3080" + poster)
  })

  it("shows the first frame when the video carries no picture, or names one that is not Legato's", () => {
    const { unmount } = render(<AgentUIItemView item={item()} fill />)
    expect((screen.getByLabelText("Tour of the change") as HTMLVideoElement).poster).toBe("")
    unmount()

    render(<AgentUIItemView item={item({ video: { url: FILE, poster: "https://elsewhere.test/track.gif" } })} fill />)
    expect((screen.getByLabelText("Tour of the change") as HTMLVideoElement).poster).toBe("")
  })

  it("lists the chapters, marks the one playing, and seeks to one on a click", () => {
    render(<AgentUIItemView item={item()} fill />)
    const video = screen.getByLabelText("Tour of the change") as HTMLVideoElement
    const chapters = within(screen.getByRole("navigation", { name: "Chapters" }))

    expect(chapters.getByRole("button", { name: /Start/ }).getAttribute("aria-current")).toBe("true")
    expect(chapters.getByRole("button", { name: /The loop/ }).getAttribute("aria-current")).toBeNull()

    fireEvent.click(chapters.getByRole("button", { name: /The loop/ }))

    expect(video.currentTime).toBe(10)
    expect(HTMLMediaElement.prototype.play).toHaveBeenCalled()
    expect(chapters.getByRole("button", { name: /The loop/ }).getAttribute("aria-current")).toBe("true")
  })

  it("says what is on screen as the video plays", () => {
    render(<AgentUIItemView item={item()} fill />)
    const video = screen.getByLabelText("Tour of the change") as HTMLVideoElement

    expect(screen.getByText("On screen: Hello")).toBeTruthy()
    at(video, 15)
    expect(screen.getByText("On screen: main.go, lines 11 to 11")).toBeTruthy()
  })

  it("pauses and puts the moment in the agent's message box when the person asks about it", () => {
    const proposed: string[] = []
    const stop = onComposerProposal("task-1", (text) => proposed.push(text))
    render(<AgentUIItemView item={item()} fill />)
    const video = screen.getByLabelText("Tour of the change") as HTMLVideoElement
    at(video, 15)

    fireEvent.click(screen.getByRole("button", { name: "Ask about this moment" }))
    stop()

    expect(HTMLMediaElement.prototype.pause).toHaveBeenCalled()
    expect(proposed).toHaveLength(1)
    expect(proposed[0]).toContain('About the video "Tour of the change":')
    expect(proposed[0]).toContain('Video paused at 0:15 of 0:30, in the chapter "The loop", scene `loop`.')
    expect(proposed[0]).toContain('Being said: "It seeks to each frame."')
    expect(proposed[0]).toContain(">   11  \tseek(i)")
    expect(proposed[0].endsWith("My question: ")).toBe(true)
  })

  it("asks about the time the video is at, not the last time the page heard of", () => {
    const proposed: string[] = []
    const stop = onComposerProposal("task-1", (text) => proposed.push(text))
    render(<AgentUIItemView item={item()} fill />)
    const video = screen.getByLabelText("Tour of the change") as HTMLVideoElement
    video.currentTime = 15 // no timeupdate yet

    fireEvent.click(screen.getByRole("button", { name: "Ask about this moment" }))
    stop()

    expect(proposed[0]).toContain("Video paused at 0:15")
  })

  it("says so when Legato's copy of the video is gone", () => {
    render(<AgentUIItemView item={item({ video: undefined })} fill />)

    expect(screen.getByText(/copy of this video is not stored any more/)).toBeTruthy()
    expect(screen.queryByRole("button", { name: "Ask about this moment" })).toBeNull()
  })

  it("says so when the manifest cannot be read", () => {
    render(<AgentUIItemView item={item({ content: "{}" })} fill />)

    expect(screen.getByText(/description cannot be read/)).toBeTruthy()
  })

  it("does not play a video from an address that is not Legato's own", () => {
    render(<AgentUIItemView item={item({ video: { url: "https://elsewhere.test/a.mp4" } })} fill />)

    expect(document.querySelector("video")).toBeNull()
  })
})
