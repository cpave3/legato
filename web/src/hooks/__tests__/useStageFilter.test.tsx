import { act, cleanup, renderHook } from "@testing-library/react"
import type { ReactNode } from "react"
import { MemoryRouter, useLocation } from "react-router-dom"
import { afterEach, beforeEach, describe, expect, it } from "vitest"
import { STAGE_FILTER_KEY, useStageFilter } from "../useStageFilter"

beforeEach(() => localStorage.clear())
afterEach(cleanup)

function at(path: string) {
  return ({ children }: { children: ReactNode }) => <MemoryRouter initialEntries={[path]}>{children}</MemoryRouter>
}

function useFilterAndPlace() {
  return { filter: useStageFilter(), location: useLocation() }
}

describe("useStageFilter", () => {
  it("reads the filter from the address first", () => {
    localStorage.setItem(STAGE_FILTER_KEY, "finished")
    const { result } = renderHook(useFilterAndPlace, { wrapper: at("/agents?stage=reviewing") })
    expect(result.current.filter.stage).toBe("reviewing")
  })

  it("remembers the viewer's filter when the address names none", () => {
    localStorage.setItem(STAGE_FILTER_KEY, "finished")
    const { result } = renderHook(useFilterAndPlace, { wrapper: at("/agents") })
    expect(result.current.filter.stage).toBe("finished")
  })

  it("keeps a new filter in the address and for the viewer, and clears both for all", () => {
    const { result } = renderHook(useFilterAndPlace, { wrapper: at("/agents?task=LEG-1") })
    act(() => result.current.filter.setStage("planning"))
    expect(result.current.filter.stage).toBe("planning")
    expect(new URLSearchParams(result.current.location.search).get("stage")).toBe("planning")
    expect(new URLSearchParams(result.current.location.search).get("task")).toBe("LEG-1")
    expect(localStorage.getItem(STAGE_FILTER_KEY)).toBe("planning")

    act(() => result.current.filter.setStage(null))
    expect(result.current.filter.stage).toBeNull()
    expect(new URLSearchParams(result.current.location.search).has("stage")).toBe(false)
    expect(localStorage.getItem(STAGE_FILTER_KEY)).toBeNull()
  })

  it("ignores a stage it does not know", () => {
    const { result } = renderHook(useFilterAndPlace, { wrapper: at("/agents?stage=bogus") })
    expect(result.current.filter.stage).toBeNull()
  })
})
