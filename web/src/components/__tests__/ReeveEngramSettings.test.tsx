import { describe, it, expect, vi, afterEach, beforeEach } from "vitest"
import { render, screen, cleanup, fireEvent, waitFor, within } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"

const mockFetch = vi.fn()
const mockSeed = vi.fn()
const mockRemoveMemory = vi.fn()
const mockRemoveThread = vi.fn()
vi.mock("../../lib/reeve", () => ({
  fetchReeveEngram: (...a: unknown[]) => mockFetch(...a),
  saveReeveSeed: (...a: unknown[]) => mockSeed(...a),
  removeReeveMemory: (...a: unknown[]) => mockRemoveMemory(...a),
  removeReeveThread: (...a: unknown[]) => mockRemoveThread(...a),
}))

import { ReeveEngramSettings } from "../settings/ReeveEngramSettings"

const template = "You are the Reeve of a Legato instance."
const engram = {
  enabled: true, name: "reeve-1a2b3c4d", store: "/home/me/.local/state/legato/reeve/engram", exists: true,
  seed: { text: template, edited: false, template, newer: false },
  memories: [{ id: "m1", kind: "preference", scope: "global", source: "told", created: "2026-11-10T23:08:45Z", body: "The person wants short answers" }],
  threads: [{ id: "t1", title: "Worker 2 drops off the wifi", scope: "global", opened: "2026-11-09T10:00:00Z" }],
}

function show() {
  render(<QueryClientProvider client={new QueryClient()}><ReeveEngramSettings baseUrl="http://legato" /></QueryClientProvider>)
}

describe("ReeveEngramSettings", () => {
  beforeEach(() => {
    for (const m of [mockFetch, mockSeed, mockRemoveMemory, mockRemoveThread]) m.mockReset()
    mockSeed.mockResolvedValue({ status: "needs_signature", approval_id: "ap-1" })
    mockRemoveMemory.mockResolvedValue({})
    mockRemoveThread.mockResolvedValue({})
  })
  afterEach(() => cleanup())

  it("shows the engram, its seed, memories and open threads", async () => {
    mockFetch.mockResolvedValue(engram)
    show()
    expect(await screen.findByText("reeve-1a2b3c4d")).toBeTruthy()
    expect((screen.getByRole("textbox", { name: "The Reeve's seed" }) as HTMLTextAreaElement).value).toBe(template)
    expect(screen.getByText("The person wants short answers")).toBeTruthy()
    expect(screen.getByText("Worker 2 drops off the wifi")).toBeTruthy()
  })

  it("removes a memory and a thread", async () => {
    mockFetch.mockResolvedValue(engram)
    show()
    fireEvent.click(within(await screen.findByTestId("reeve-memory-m1")).getByRole("button", { name: "Remove" }))
    await waitFor(() => expect(mockRemoveMemory).toHaveBeenCalledWith("http://legato", "m1"))
    fireEvent.click(within(screen.getByTestId("reeve-thread-t1")).getByRole("button", { name: "Remove" }))
    await waitFor(() => expect(mockRemoveThread).toHaveBeenCalledWith("http://legato", "t1"))
  })

  it("sends an edited seed for a signature", async () => {
    mockFetch.mockResolvedValue(engram)
    show()
    fireEvent.change(await screen.findByRole("textbox", { name: "The Reeve's seed" }), { target: { value: "Be brief." } })
    fireEvent.click(screen.getByRole("button", { name: "Save the seed" }))
    await waitFor(() => expect(mockSeed).toHaveBeenCalledWith("http://legato", "Be brief."))
    expect((await screen.findByRole("status")).textContent).toMatch(/approve/i)
  })

  it("offers a newer built-in seed and never puts it in by itself", async () => {
    mockFetch.mockResolvedValue({ ...engram, seed: { text: "Mine.", edited: true, template, newer: true } })
    show()
    expect((await screen.findByRole("textbox", { name: "The Reeve's seed" }) as HTMLTextAreaElement).value).toBe("Mine.")
    fireEvent.click(screen.getByRole("button", { name: "Use the new built-in seed" }))
    await waitFor(() => expect(mockSeed).toHaveBeenCalledWith("http://legato", template))
  })

  it("says why the Reeve has no engram", async () => {
    mockFetch.mockResolvedValue({ ...engram, enabled: false, exists: false, error: "engrams are off here (engram.enabled)", memories: [], threads: [] })
    show()
    expect((await screen.findByText(/engram.enabled/)).textContent).toContain("off")
    expect(screen.queryByRole("button", { name: "Save the seed" })).toBeNull()
  })
})
