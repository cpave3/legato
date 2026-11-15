import { QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { afterEach, expect, it, vi } from "vitest"
import { apiFetch } from "../../lib/api"
import { createQueryClient, queryKeys } from "../../lib/query"
import { AttentionPage } from "../AttentionPage"

vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../lib/api", () => ({ apiFetch: vi.fn() }))
vi.mock("../../components/AskReeve", () => ({ AskReeve: () => null }))
afterEach(() => { cleanup(); vi.resetAllMocks() })

it("keeps cached attention after refresh failure and retries without a false all-clear", async () => {
  const client = createQueryClient()
  vi.mocked(apiFetch).mockResolvedValueOnce(new Response(JSON.stringify({ items: [{
    id: "att-1", producer: "plans", signal_kind: "plan_decision", source_key: "pl-1", source_revision: "1",
    title: "Review search plan", reason: "Awaiting decision", category: "approval", urgency: "normal",
    navigation: { kind: "plan", target: "pl-1" }, actions: [], status: "open",
    created_at: "2026-11-01T10:00:00Z", updated_at: "2026-11-01T10:00:00Z",
  }] })))
  render(<QueryClientProvider client={client}><MemoryRouter><AttentionPage /></MemoryRouter></QueryClientProvider>)
  await screen.findByText("Review search plan")
  vi.mocked(apiFetch).mockRejectedValueOnce(new Error("Server unavailable"))
  await act(async () => { await client.invalidateQueries({ queryKey: queryKeys.attention("") }) })
  await screen.findByRole("alert")
  expect(screen.getByText("Review search plan")).toBeTruthy()
  expect(screen.getByRole("alert").textContent).toMatch(/last successful request/)
  expect(screen.queryByText("Nothing needs your attention right now")).toBeNull()
  vi.mocked(apiFetch).mockResolvedValueOnce(new Response(JSON.stringify({ items: [] })))
  fireEvent.click(screen.getByRole("button", { name: "Retry" }))
  await screen.findByText("Nothing needs your attention right now")
  expect(screen.queryByRole("alert")).toBeNull()
  client.clear()
})

it("offers retry after an initial failure without claiming the dashboard is empty", async () => {
  const client = createQueryClient()
  vi.mocked(apiFetch).mockRejectedValueOnce(new Error("Connection lost"))
  render(<QueryClientProvider client={client}><MemoryRouter><AttentionPage /></MemoryRouter></QueryClientProvider>)
  await screen.findByRole("alert")
  expect(screen.queryByText("Nothing needs your attention right now")).toBeNull()
  expect(screen.queryByText("No interventions needed")).toBeNull()
  expect(screen.queryByText("0 items")).toBeNull()
  vi.mocked(apiFetch).mockResolvedValueOnce(new Response(JSON.stringify({ items: [] })))
  fireEvent.click(screen.getByRole("button", { name: "Retry" }))
  await screen.findByText("Nothing needs your attention right now")
  client.clear()
})
