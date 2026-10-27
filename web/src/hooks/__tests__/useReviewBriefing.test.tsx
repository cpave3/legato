import { renderHook } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"
import type { ReviewBriefingPage } from "../../lib/reviewBriefing"
import { useUnseenBriefingPages } from "../useReviewBriefing"

const page = (label: string, version: string): ReviewBriefingPage => ({ path: `.review/feat-login/${label}.html`, folder: "feat-login", label, version })

afterEach(() => localStorage.clear())

describe("useUnseenBriefingPages", () => {
  it("counts pages that appeared or were rebuilt until the person looks", () => {
    const { result, rerender } = renderHook(({ pages, viewing }) => useUnseenBriefingPages("task-1", pages, viewing), {
      initialProps: { pages: [page("index", "v1")], viewing: false },
    })
    expect(result.current).toBe(1)

    // The panel is open on this agent: what it lists has been seen.
    rerender({ pages: [page("index", "v1")], viewing: true })
    expect(result.current).toBe(0)
    rerender({ pages: [page("index", "v1")], viewing: false })
    expect(result.current).toBe(0)

    // Rebuilt, and a page added.
    rerender({ pages: [page("index", "v2"), page("risks", "v1")], viewing: false })
    expect(result.current).toBe(2)
  })

  it("remembers what was seen for the next visit", () => {
    const first = renderHook(() => useUnseenBriefingPages("task-1", [page("index", "v1")], true))
    first.unmount()
    const again = renderHook(() => useUnseenBriefingPages("task-1", [page("index", "v1")], false))
    expect(again.result.current).toBe(0)
    const other = renderHook(() => useUnseenBriefingPages("task-2", [page("index", "v1")], false))
    expect(other.result.current).toBe(1)
  })
})
