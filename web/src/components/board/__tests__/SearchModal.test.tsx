import { describe, expect, it, vi, beforeEach } from "vitest"
import { render, screen, fireEvent } from "@testing-library/react"
import { SearchModal } from "../SearchModal"

const mockFetch = vi.fn()
beforeEach(() => {
  vi.resetAllMocks()
})

vi.stubGlobal("fetch", mockFetch)

vi.mock("../../../hooks/useServer", () => ({
  useServer: () => ({ baseUrl: "" }),
}))

describe("SearchModal", () => {
  it("debounces and shows results", async () => {
    mockFetch.mockResolvedValue({
      ok: true,
      json: async () => [
        { id: "T-1", title: "First result", priority: "High", status: "Backlog", provider: "" },
      ],
    })

    render(<SearchModal open={true} onClose={() => {}} onSelect={() => {}} />)

    const input = screen.getByPlaceholderText("Search...")
    fireEvent.change(input, { target: { value: "first" } })

    await vi.waitFor(() => expect(screen.getByText(/First result/)).toBeTruthy(), { timeout: 2000 })
  })
})
