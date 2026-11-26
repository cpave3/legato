import { act, renderHook } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { useCardFolds } from "../useCardFolds"

// LEG-655: the cards over a workflow's graph fold, and stay as they were
// left, under a key of each page's own.
describe("useCardFolds", () => {
  afterEach(() => { localStorage.clear(); vi.restoreAllMocks() })

  it("keeps each page's folds apart, with its own defaults until one is toggled", () => {
    const run = renderHook(() => useCardFolds("legato:run-page:folds", { steps: true }))
    const page = renderHook(() => useCardFolds("legato:workflow-page:folds", { outputs: true }))
    expect(run.result.current.folded("steps")).toBe(true)
    expect(run.result.current.folded("outputs")).toBe(false)
    expect(page.result.current.folded("outputs")).toBe(true)

    act(() => run.result.current.toggle("steps"))
    expect(run.result.current.folded("steps")).toBe(false)
    expect(JSON.parse(localStorage.getItem("legato:run-page:folds") ?? "{}")).toEqual({ steps: false })
    expect(localStorage.getItem("legato:workflow-page:folds")).toBeNull()

    // Opened again, the page finds the fold it was left with.
    const again = renderHook(() => useCardFolds("legato:run-page:folds", { steps: true }))
    expect(again.result.current.folded("steps")).toBe(false)
  })

  it("works for the visit when the browser keeps nothing", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => { throw new Error("blocked") })
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => { throw new Error("blocked") })
    const { result } = renderHook(() => useCardFolds("legato:workflow-page:folds", {}))
    act(() => result.current.toggle("inputs"))
    expect(result.current.folded("inputs")).toBe(true)
  })
})
