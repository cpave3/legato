import { describe, expect, it, vi, beforeEach } from "vitest"
import { renderHook, waitFor } from "@testing-library/react"
import { useBoard } from "../useBoard"

const mockFetch = vi.fn()
const mockSubscribe = vi.fn()

beforeEach(() => {
  vi.resetAllMocks()
})

vi.stubGlobal("fetch", mockFetch)

vi.mock("../useWebSocket", () => ({
  useWebSocket: () => ({
    subscribe: (handler: (msg: { type: string }) => void) => mockSubscribe(handler),
  }),
}))

vi.mock("../useServer", () => ({
  useServer: () => ({ baseUrl: "" }),
}))

describe("useBoard", () => {
  it("refetches on cards_changed WS message", async () => {
    mockFetch.mockResolvedValueOnce({
      ok: true,
      json: async () => ({
        columns: [{ name: "Backlog", cards: [{ id: "T-1", title: "First" }] }],
        workspaces: [],
      }),
    })

    let capturedHandler: ((msg: { type: string }) => void) | null = null
    mockSubscribe.mockImplementation((h: (msg: { type: string }) => void) => {
      capturedHandler = h
      return () => {}
    })

    const { result } = renderHook(() => useBoard("all"))

    await waitFor(() => expect(result.current.columns.length).toBe(1))
    expect(result.current.columns[0].cards[0].title).toBe("First")

    // Second fetch for refresh after cards_changed
    mockFetch.mockResolvedValueOnce({
      ok: true,
      json: async () => ({
        columns: [{ name: "Backlog", cards: [{ id: "T-2", title: "Second" }] }],
        workspaces: [],
      }),
    })

    // Simulate WS event
    ;(capturedHandler as unknown as (msg: { type: string }) => void)({ type: "cards_changed" })

    await waitFor(() => expect(result.current.columns[0].cards[0].title).toBe("Second"))
  })
})
