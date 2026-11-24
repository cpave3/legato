import { describe, it, expect, vi, afterEach } from "vitest"
import { render, screen, fireEvent, cleanup, waitFor } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import type { AttentionItem } from "../../lib/attention"

const mockFetchMacros = vi.fn()
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "http://legato" }) }))
vi.mock("../../lib/macros", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../../lib/macros")>()),
  fetchMacroList: (...a: unknown[]) => mockFetchMacros(...a),
}))

import { NeedsYouRow } from "../needsYou/NeedsYouRow"

// A run waiting for its pull request to merge (LEG-614).
function mergeItem(resumeLabel = "Resume agent"): AttentionItem {
  return {
    id: "att-1", producer: "workflows", source_key: "wr-1:merged", source_revision: "merge:merged:1:true:fail",
    task_id: "card0001", title: "Review and merge PR #12", reason: "PR #12 conflicts with main · CI failing",
    category: "workflow", urgency: "normal", signal_kind: "workflow_merge_waiting", status: "open",
    navigation: { kind: "workflow_run", target: "wr-1" },
    actions: [
      { id: "open", label: "Open run", kind: "navigate", intent: "primary" },
      { id: "open_pr", label: "Open PR", kind: "navigate", href: "https://github.com/acme/widget/pull/12" },
      { id: "resume_agent", label: resumeLabel, kind: "execute", picks: "macro" },
    ],
    created_at: "2026-11-24T00:00:00Z", updated_at: "2026-11-24T00:00:00Z",
  } as AttentionItem
}

afterEach(() => { cleanup(); vi.clearAllMocks() })

describe("a waiting merge's row", () => {
  it("picks a macro from the task, pull request and unscoped ones, favourites first, and runs Resume agent with it", async () => {
    mockFetchMacros.mockResolvedValue({
      macros: [
        { name: "Review", keys: "Review.\n", applies_to: "pr" },
        { name: "Fix CI", keys: "Fix.\n", applies_to: "task", favorite: true },
        { name: "Plan", keys: "Plan it.\n" },
        { name: "Rebase and repush", keys: "Rebase.\n", applies_to: "task", favorite: true },
      ],
      suggested: { task: "Plan", my_pr: "Address feedback", others_pr: "Review" }, tiers: {},
    })
    const onAction = vi.fn()
    render(<MemoryRouter><NeedsYouRow item={mergeItem()} onAction={onAction} onDismiss={vi.fn()} /></MemoryRouter>)
    expect(screen.getByText("Merge")).toBeTruthy()
    expect(screen.getByRole("link", { name: /Open PR/ }).getAttribute("href")).toBe("https://github.com/acme/widget/pull/12")
    expect(screen.queryByRole("button", { name: "Resume agent" })).toBeNull()
    fireEvent.click(screen.getByRole("button", { name: "Resume agent…" }))
    await screen.findByRole("menuitem", { name: "Rebase and repush" })
    const rows = () => screen.getAllByRole("menuitem").map(row => row.textContent)
    expect(rows()).toEqual(["Fix CI", "Rebase and repush", "Show all (2 more)"])
    fireEvent.click(screen.getByRole("menuitem", { name: /Show all/ }))
    expect(rows()).toEqual(["Fix CI", "Rebase and repush", "Review", "Plan"])
    fireEvent.click(screen.getByRole("menuitem", { name: "Rebase and repush" }))
    await waitFor(() => expect(onAction).toHaveBeenCalledWith(expect.objectContaining({ id: "att-1" }), "resume_agent", "Rebase and repush"))
  })

  it("says the macro goes to the running agent", () => {
    mockFetchMacros.mockResolvedValue({ macros: [], suggested: { task: "", my_pr: "", others_pr: "" }, tiers: {} })
    render(<MemoryRouter><NeedsYouRow item={mergeItem("Send a macro")} onAction={vi.fn()} onDismiss={vi.fn()} /></MemoryRouter>)
    expect(screen.getByRole("button", { name: "Send a macro…" })).toBeTruthy()
  })
})
