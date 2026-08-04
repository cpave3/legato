import { QueryClientProvider } from "@tanstack/react-query"
import { createElement } from "react"
import { renderHook, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { createQueryClient } from "../../lib/query"
import { useBoard } from "../useBoard"

const mockFetch = vi.fn()
vi.stubGlobal("fetch", mockFetch)
vi.mock("../useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))

beforeEach(() => mockFetch.mockReset())

describe("useBoard", () => {
  it("shares a settled board snapshot between consumers", async () => {
    mockFetch.mockResolvedValue({
      ok: true,
      json: async () => ({ columns: [{ name: "Backlog", cards: [{ id: "T-1", title: "First" }] }], workspaces: [] }),
    })
    const client = createQueryClient()
    const wrapper = ({ children }: { children: React.ReactNode }) => createElement(QueryClientProvider, { client }, children)

    const first = renderHook(() => useBoard("all"), { wrapper })
    await waitFor(() => expect(first.result.current.columns[0]?.cards[0]?.title).toBe("First"))
    const second = renderHook(() => useBoard("all"), { wrapper })
    await waitFor(() => expect(second.result.current.columns[0]?.cards[0]?.title).toBe("First"))

    expect(mockFetch).toHaveBeenCalledTimes(1)
  })
})
