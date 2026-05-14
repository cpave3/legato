import { describe, expect, it, vi, beforeEach } from "vitest"
import { render, screen, fireEvent, waitFor } from "@testing-library/react"
import { LinkPRModal } from "../LinkPRModal"

const mockFetch = vi.fn()
beforeEach(() => {
  vi.resetAllMocks()
})

vi.stubGlobal("fetch", mockFetch)

vi.mock("../../../hooks/useServer", () => ({
  useServer: () => ({ baseUrl: "" }),
}))

describe("LinkPRModal", () => {
  it("two-phase flow: input → preview → confirm", async () => {
    // Phase 1: detect-repo on open
    mockFetch.mockResolvedValueOnce({
      ok: true,
      json: async () => ({ owner: "acme", repo: "app" }),
    })

    // Phase 2: pr-preview after fetch
    mockFetch.mockResolvedValueOnce({
      ok: true,
      json: async () => ({
        number: 123,
        url: "https://github.com/acme/app/pull/123",
        state: "OPEN",
        is_draft: false,
        check_status: "pass",
        review_decision: "APPROVED",
        comment_count: 2,
      }),
    })

    // Phase 3: link-pr confirm
    mockFetch.mockResolvedValueOnce({ ok: true })

    const onLinked = vi.fn()
    render(<LinkPRModal open={true} taskId="T-1" onClose={() => {}} onLinked={onLinked} />)

    await waitFor(() => expect(screen.getByDisplayValue(/acme\/app/)).toBeTruthy(), { timeout: 2000 })

    const prInput = screen.getByLabelText(/PR #/i)
    fireEvent.change(prInput, { target: { value: "123" } })

    const fetchBtn = screen.getByRole("button", { name: /Fetch/i })
    fireEvent.click(fetchBtn)

    await waitFor(() => expect(screen.getByText(/#123/)).toBeTruthy(), { timeout: 2000 })

    const linkBtn = screen.getByRole("button", { name: /Link/i })
    fireEvent.click(linkBtn)

    await waitFor(() => expect(onLinked).toHaveBeenCalled(), { timeout: 2000 })
  })
})
