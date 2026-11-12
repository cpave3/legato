import { describe, it, expect, vi, afterEach } from "vitest"
import { render, screen, cleanup } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"

vi.mock("../../lib/reeve", () => ({
  fetchReeve: () => Promise.resolve({ history: [], folder: "/r", instance: "legato-1", role: "main", to_approve: 0 }),
  startReeve: vi.fn(),
  resumeReeve: vi.fn(),
}))

import { ReeveEntry } from "../ReeveEntry"
import { ChangeSetCard } from "../ChangeSetCard"
import { ReeveNameContext } from "../../lib/reeveName"
import type { Approval } from "../../lib/approvals"

describe("the Reeve's name", () => {
  afterEach(() => cleanup())

  it("is what the Reeve's entry says, and what picking it starts", async () => {
    render(
      <QueryClientProvider client={new QueryClient()}>
        <ReeveNameContext.Provider value="Tessitura">
          <ReeveEntry baseUrl="" selected={false} onSelect={() => {}} />
        </ReeveNameContext.Provider>
      </QueryClientProvider>,
    )
    const start = await screen.findByRole("button", { name: "Start Tessitura" })
    expect(start.textContent).toContain("Tessitura")
    expect(start.textContent).not.toContain("Reeve")
  })

  it("is the name of the Reeve that proposed a change set, at the start of its line", () => {
    const approval = {
      id: "ap-1", source: "reeve_change_set", source_key: "cs-1", title: "Restart", options: [], blocking: false, status: "pending", revision: 1, created_at: "",
      details: { id: "cs-1", instance: "legato-1", level: "approve", reeve_name: "Tessitura", calls: [{ kind: "restart", class: "risky", description: "Restart Legato", level: "approve" }] },
    } as Approval
    render(<ChangeSetCard approval={approval} />)
    expect(screen.getByTestId("change-set").textContent).toContain("Tessitura on legato-1 · cs-1 · 1 change")
  })
})
