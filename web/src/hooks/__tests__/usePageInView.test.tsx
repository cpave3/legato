import { act, cleanup, renderHook } from "@testing-library/react"
import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { HIDDEN_GRACE_MS, usePageInView } from "../usePageInView"

let hidden = false

function setHidden(value: boolean) {
  hidden = value
  document.dispatchEvent(new Event("visibilitychange"))
}

beforeEach(() => {
  hidden = false
  vi.useFakeTimers()
  vi.spyOn(document, "hidden", "get").mockImplementation(() => hidden)
  vi.spyOn(document, "visibilityState", "get").mockImplementation(() => hidden ? "hidden" : "visible")
})

afterEach(() => {
  cleanup()
  vi.useRealTimers()
  vi.restoreAllMocks()
})

it("stays in view through a short look away, and leaves after the grace period", () => {
  const { result } = renderHook(() => usePageInView())
  expect(result.current).toBe(true)
  act(() => setHidden(true))
  act(() => { vi.advanceTimersByTime(HIDDEN_GRACE_MS - 1) })
  expect(result.current).toBe(true)
  act(() => { vi.advanceTimersByTime(1) })
  expect(result.current).toBe(false)
  act(() => setHidden(false))
  expect(result.current).toBe(true)
})

it("does not leave when the page comes back within the grace period", () => {
  const { result } = renderHook(() => usePageInView())
  act(() => setHidden(true))
  act(() => setHidden(false))
  act(() => { vi.advanceTimersByTime(HIDDEN_GRACE_MS * 2) })
  expect(result.current).toBe(true)
})

it("leaves at once when the mobile app goes to the background, and comes back with it", () => {
  const { result } = renderHook(() => usePageInView())
  act(() => { window.dispatchEvent(new Event("legato:shell-leave")) })
  expect(result.current).toBe(false)
  act(() => { window.dispatchEvent(new Event("legato:shell-return")) })
  expect(result.current).toBe(true)
})

it("starts out of view in a page that is hidden", () => {
  hidden = true
  const { result } = renderHook(() => usePageInView())
  expect(result.current).toBe(false)
})
