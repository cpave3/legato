import { describe, expect, it, vi } from "vitest"
import { createDiscussionPanel } from "./discussion-panel"

function fixture() {
  const sessions: Array<{ taskId: string; stop: ReturnType<typeof vi.fn> }> = []
  const timers: Array<() => void> = []
  const find = vi.fn()
  const show = vi.fn()
  const panel = createDiscussionPanel({
    find,
    startSession: (ref) => {
      const session = { taskId: ref.taskId, stop: vi.fn() }
      sessions.push(session)
      return session
    },
    show,
    schedule: (callback) => { timers.push(callback); return timers.length },
    cancel: vi.fn(),
  })
  return { panel, sessions, timers, find, show }
}

const ref = (taskId: string) => ({ runId: `run-${taskId}`, taskId, agentUrl: `http://legato/agents?task=${taskId}` })

describe("createDiscussionPanel", () => {
  it("follows the discussion of the tab's pull request", async () => {
    const f = fixture()
    f.find.mockResolvedValue(ref("a"))

    await f.panel.follow(4)

    expect(f.find).toHaveBeenCalledWith(4)
    expect(f.sessions.map((session) => session.taskId)).toEqual(["a"])
    expect(f.show).toHaveBeenLastCalledWith({ kind: "discussion", ref: ref("a") })
  })

  it("keeps looking while no discussion is running, and picks one up when it starts", async () => {
    const f = fixture()
    f.find.mockResolvedValueOnce(null).mockResolvedValueOnce(ref("a"))

    await f.panel.follow(4)
    expect(f.show).toHaveBeenLastCalledWith({ kind: "none" })
    expect(f.sessions).toHaveLength(0)

    f.timers.shift()!()
    await vi.waitFor(() => expect(f.sessions).toHaveLength(1))
  })

  it("keeps the same session when its tab is followed again", async () => {
    const f = fixture()
    f.find.mockResolvedValue(ref("a"))

    await f.panel.follow(4)
    await f.panel.follow(4)

    expect(f.sessions).toHaveLength(1)
    expect(f.sessions[0].stop).not.toHaveBeenCalled()
  })

  it("stops the session of a pull request the panel no longer shows", async () => {
    const f = fixture()
    f.find.mockResolvedValueOnce(ref("a")).mockResolvedValueOnce(ref("b"))

    await f.panel.follow(4)
    await f.panel.follow(5)

    expect(f.sessions[0].stop).toHaveBeenCalled()
    expect(f.sessions.map((session) => session.taskId)).toEqual(["a", "b"])
  })

  it("says why when the discussion cannot be looked up", async () => {
    const f = fixture()
    f.find.mockRejectedValue(new Error("No GitHub pull request context is available for this tab"))

    await f.panel.follow(4)

    expect(f.show).toHaveBeenLastCalledWith({ kind: "error", message: "No GitHub pull request context is available for this tab" })
  })

  it("lets go of everything when hidden", async () => {
    const f = fixture()
    f.find.mockResolvedValue(ref("a"))
    await f.panel.follow(4)

    f.panel.hide()

    expect(f.sessions[0].stop).toHaveBeenCalled()
  })
})
