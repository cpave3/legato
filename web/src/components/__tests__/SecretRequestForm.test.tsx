import { describe, it, expect, vi, afterEach } from "vitest"
import { render, screen, cleanup, fireEvent, waitFor } from "@testing-library/react"

const mockStore = vi.fn()
vi.mock("../../lib/reeve", () => ({ storeReeveSecret: (...a: unknown[]) => mockStore(...a) }))

import { SecretRequestForm } from "../SecretRequestForm"
import type { Approval } from "../../lib/approvals"

const approval: Approval = {
  id: "ap-9", source: "reeve_secret", source_key: "rv1:eleven", title: "The Reeve asks for a secret: eleven", summary: "ElevenLabs for videos",
  options: [], blocking: false, status: "pending", revision: 1, created_at: "", details: { name: "eleven" },
}

describe("SecretRequestForm", () => {
  afterEach(() => cleanup())

  it("stores the pasted value and never shows it", async () => {
    mockStore.mockResolvedValue("legato://eleven")
    const done = vi.fn().mockResolvedValue(undefined)
    render(<SecretRequestForm approval={approval} baseUrl="http://legato" onDone={done} />)
    const input = screen.getByLabelText("Value of eleven") as HTMLInputElement
    expect(input.type).toBe("password")
    fireEvent.change(input, { target: { value: "sk-123" } })
    fireEvent.click(screen.getByRole("button", { name: "Store the secret" }))
    await waitFor(() => expect(mockStore).toHaveBeenCalledWith("http://legato", "ap-9", "sk-123"))
    await waitFor(() => expect(done).toHaveBeenCalled())
  })
})
