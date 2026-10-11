import { act, cleanup, renderHook } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { useArtifactPanel } from "../useArtifactPanel"

let websocketHandler: ((message: unknown) => void) | undefined

vi.mock("../useWebSocket", () => ({
  useWebSocket: () => ({
    subscribe: (handler: (message: unknown) => void) => {
      websocketHandler = handler
      return () => { websocketHandler = undefined }
    },
  }),
}))

afterEach(cleanup)

function shown(taskId: string, itemId: string, placement: "panel" | "inline" = "panel", revision = 1) {
  act(() => websocketHandler?.({ type: "agent_ui_changed", task_id: taskId, item_id: itemId, placement, revision, kind: "shown" }))
}

describe("useArtifactPanel", () => {
  it("opens on a new artifact from the agent in view, focused on that artifact", () => {
    const { result } = renderHook(() => useArtifactPanel("task-1", true))
    expect(result.current.open).toBe(false)

    shown("task-1", "ui-aaaaaaaaaa")

    expect(result.current.open).toBe(true)
    expect(result.current.focus).toEqual({ itemId: "ui-aaaaaaaaaa", revision: 0 })
    expect(result.current.unseen).toEqual({})
  })

  it("does not open for a card: a card is in the conversation already", () => {
    const { result } = renderHook(() => useArtifactPanel("task-1", true))

    shown("task-1", "ui-cccccccccc", "inline")

    expect(result.current.open).toBe(false)
    expect(result.current.unseen).toEqual({})
  })

  it("counts artifacts from agents that are not in view instead of opening", () => {
    const { result } = renderHook(() => useArtifactPanel("task-1", true))

    shown("task-2", "ui-aaaaaaaaaa")
    shown("task-2", "ui-bbbbbbbbbb")
    // A new revision of an artifact already counted is not another artifact.
    shown("task-2", "ui-bbbbbbbbbb", "panel", 2)

    expect(result.current.open).toBe(false)
    expect(result.current.unseen).toEqual({ "task-2": 2 })
  })

  it("opens on the newest unseen artifact when the person goes to that agent", () => {
    const { result, rerender } = renderHook(({ selected }) => useArtifactPanel(selected, true), { initialProps: { selected: "task-1" as string | null } })
    shown("task-2", "ui-aaaaaaaaaa")
    shown("task-2", "ui-bbbbbbbbbb")

    rerender({ selected: "task-2" })

    expect(result.current.open).toBe(true)
    expect(result.current.focus).toEqual({ itemId: "ui-bbbbbbbbbb", revision: 0 })
    expect(result.current.unseen).toEqual({})
  })

  it("closes when the person goes to an agent with nothing new", () => {
    const { result, rerender } = renderHook(({ selected }) => useArtifactPanel(selected, true), { initialProps: { selected: "task-1" as string | null } })
    shown("task-1", "ui-aaaaaaaaaa")
    expect(result.current.open).toBe(true)

    rerender({ selected: "task-3" })

    expect(result.current.open).toBe(false)
    expect(result.current.focus).toBeNull()
  })

  it("on a phone, marks the artifact as new instead of covering the screen", () => {
    const { result } = renderHook(() => useArtifactPanel("task-1", false))

    shown("task-1", "ui-aaaaaaaaaa")

    expect(result.current.open).toBe(false)
    expect(result.current.unseen).toEqual({ "task-1": 1 })

    act(() => result.current.toggle())
    expect(result.current.open).toBe(true)
    expect(result.current.focus).toEqual({ itemId: "ui-aaaaaaaaaa", revision: 0 })
    expect(result.current.unseen).toEqual({})
  })

  it("opens on one item at one revision when asked, and closes", () => {
    const { result } = renderHook(() => useArtifactPanel("task-1", true))

    act(() => result.current.openItem("ui-cccccccccc", 2))
    expect(result.current.open).toBe(true)
    expect(result.current.focus).toEqual({ itemId: "ui-cccccccccc", revision: 2 })

    act(() => result.current.close())
    expect(result.current.open).toBe(false)
  })

  it("lets go of an item that was removed while in focus", () => {
    const { result } = renderHook(() => useArtifactPanel("task-1", true))
    shown("task-1", "ui-aaaaaaaaaa")

    act(() => websocketHandler?.({ type: "agent_ui_changed", task_id: "task-1", item_id: "ui-aaaaaaaaaa", placement: "panel", revision: 1, kind: "deleted" }))

    expect(result.current.focus).toBeNull()
  })
})

// A link from outside the page (the TUI shows one) names an agent and an item.
describe("useArtifactPanel links", () => {
  it("opens on the linked item once the linked agent is in view", () => {
    const { result, rerender } = renderHook(({ selected }) => useArtifactPanel(selected, true), { initialProps: { selected: "task-1" as string | null } })

    act(() => result.current.showFor("task-2", "ui-cccccccccc"))
    // The agent in view has not changed, so nothing opens yet.
    expect(result.current.open).toBe(false)

    rerender({ selected: "task-2" })

    expect(result.current.open).toBe(true)
    expect(result.current.focus).toEqual({ itemId: "ui-cccccccccc", revision: 0 })
  })

  it("opens at once when the linked agent is already in view", () => {
    const { result } = renderHook(() => useArtifactPanel("task-1", true))

    act(() => result.current.showFor("task-1", "ui-cccccccccc"))

    expect(result.current.open).toBe(true)
    expect(result.current.focus).toEqual({ itemId: "ui-cccccccccc", revision: 0 })
  })

  it("opens for a link on a phone too, where new artifacts do not open by themselves", () => {
    const { result, rerender } = renderHook(({ selected }) => useArtifactPanel(selected, false), { initialProps: { selected: null as string | null } })

    act(() => result.current.showFor("task-2", "ui-cccccccccc"))
    rerender({ selected: "task-2" })

    expect(result.current.open).toBe(true)
  })

  it("is used once: going away and back does not open the panel again", () => {
    const { result, rerender } = renderHook(({ selected }) => useArtifactPanel(selected, true), { initialProps: { selected: "task-1" as string | null } })
    act(() => result.current.showFor("task-2", "ui-cccccccccc"))
    rerender({ selected: "task-2" })
    expect(result.current.open).toBe(true)

    rerender({ selected: "task-1" })
    rerender({ selected: "task-2" })

    expect(result.current.open).toBe(false)
  })
})
