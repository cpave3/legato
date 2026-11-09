import { describe, it, expect, vi, afterEach, beforeEach } from "vitest"
import { render, screen, cleanup, fireEvent, waitFor, within } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"

const mockFetch = vi.fn()
const mockStart = vi.fn()
const mockEnd = vi.fn()
const mockResume = vi.fn()
const mockDelete = vi.fn()
const mockPeers = vi.fn()
const mockPair = vi.fn()
const mockRemovePeer = vi.fn()
vi.mock("../../lib/reeve", () => ({
  listReevePeers: (...a: unknown[]) => mockPeers(...a),
  pairReevePeer: (...a: unknown[]) => mockPair(...a),
  removeReevePeer: (...a: unknown[]) => mockRemovePeer(...a),
  fetchReeve: (...a: unknown[]) => mockFetch(...a),
  startReeve: (...a: unknown[]) => mockStart(...a),
  endReeve: (...a: unknown[]) => mockEnd(...a),
  resumeReeve: (...a: unknown[]) => mockResume(...a),
  deleteReeve: (...a: unknown[]) => mockDelete(...a),
}))

import { ReevePairings, ReeveSessions } from "../settings/ReeveSettings"

const ended = { task_id: "old1", state: "ended", agent_kind: "codex", title: "The Reeve", created_at: "2026-11-01T10:00:00Z", updated_at: "", active_at: "" }

function show() {
  render(<QueryClientProvider client={new QueryClient()}><ReeveSessions baseUrl="http://legato" /></QueryClientProvider>)
}

describe("ReeveSessions", () => {
  beforeEach(() => {
    for (const m of [mockStart, mockEnd, mockResume, mockDelete]) { m.mockReset(); m.mockResolvedValue({}) }
    mockFetch.mockReset()
  })
  afterEach(() => cleanup())

  it("starts a session with an override of its tool and model", async () => {
    mockFetch.mockResolvedValue({ history: [ended], folder: "/home/me/.local/state/legato/reeve", instance: "box", role: "standalone", to_approve: 0 })
    show()
    fireEvent.change(await screen.findByRole("textbox", { name: "Model for this session" }), { target: { value: "opus" } })
    fireEvent.click(screen.getByRole("button", { name: "Start the Reeve" }))
    await waitFor(() => expect(mockStart).toHaveBeenCalledWith("http://legato", { agent: undefined, model: "opus" }))
  })

  it("ends the running session, and resumes or deletes an old one", async () => {
    mockFetch.mockResolvedValue({ current: { ...ended, task_id: "now1", state: "running" }, history: [{ ...ended, task_id: "now1", state: "running" }, ended], folder: "/r" })
    show()
    fireEvent.click(await screen.findByRole("button", { name: "End session" }))
    await waitFor(() => expect(mockEnd).toHaveBeenCalledWith("http://legato", "now1"))
    const old = screen.getByTestId("reeve-session-old1")
    expect((within(old).getByRole("button", { name: "Resume" }) as HTMLButtonElement).disabled).toBe(true)
    fireEvent.click(within(old).getByRole("button", { name: "Delete" }))
    await waitFor(() => expect(mockDelete).toHaveBeenCalledWith("http://legato", "old1"))
  })

  it("says why a start was refused", async () => {
    mockFetch.mockResolvedValue({ history: [], folder: "/r" })
    mockStart.mockRejectedValue(new Error("the Reeve runs only in Legato's sandbox, which is off here"))
    show()
    fireEvent.click(await screen.findByRole("button", { name: "Start the Reeve" }))
    expect((await screen.findByRole("alert")).textContent).toContain("sandbox")
  })
})

describe("ReevePairings", () => {
  beforeEach(() => {
    for (const m of [mockPair, mockRemovePeer]) { m.mockReset(); m.mockResolvedValue({}) }
    mockPeers.mockReset()
  })
  afterEach(() => cleanup())

  const peers = [
    { instance_id: "srv-0b8e2a91d4c7f356", name: "vm-1", via: "worker" },
    { instance_id: "srv-1111222233334444", name: "nas", via: "pairing", address: "https://nas.lan:3080" },
  ]

  it("lists the instances whose Reeves this one can message, and how", async () => {
    mockPeers.mockResolvedValue(peers)
    render(<ReevePairings baseUrl="http://legato" />)
    const nas = await screen.findByTestId("reeve-peer-srv-1111222233334444")
    expect(nas.textContent).toContain("nas")
    expect(nas.textContent).toContain("Paired Reeve")
    expect(screen.getByTestId("reeve-peer-srv-0b8e2a91d4c7f356").textContent).toContain("Worker")
    // Only a Reeve pairing is removed here; a worker is unpaired in Workers.
    expect(within(screen.getByTestId("reeve-peer-srv-0b8e2a91d4c7f356")).queryByRole("button", { name: /Remove/ })).toBeNull()
    fireEvent.click(within(nas).getByRole("button", { name: "Remove nas" }))
    await waitFor(() => expect(mockRemovePeer).toHaveBeenCalledWith("http://legato", "srv-1111222233334444"))
  })

  it("pairs with another instance's Reeve by its address and phrase", async () => {
    mockPeers.mockResolvedValue([])
    render(<ReevePairings baseUrl="http://legato" />)
    fireEvent.change(await screen.findByRole("textbox", { name: "Address of the other instance" }), { target: { value: "https://nas.lan:3080" } })
    fireEvent.change(screen.getByRole("textbox", { name: "Its phrase" }), { target: { value: "apple-river-stone" } })
    fireEvent.click(screen.getByRole("button", { name: "Pair" }))
    await waitFor(() => expect(mockPair).toHaveBeenCalledWith("http://legato", { address: "https://nas.lan:3080", phrase: "apple-river-stone" }))
  })

  it("says why a pairing failed", async () => {
    mockPeers.mockResolvedValue([])
    mockPair.mockRejectedValue(new Error("the phrase did not match"))
    render(<ReevePairings baseUrl="http://legato" />)
    fireEvent.change(await screen.findByRole("textbox", { name: "Address of the other instance" }), { target: { value: "https://nas.lan:3080" } })
    fireEvent.change(screen.getByRole("textbox", { name: "Its phrase" }), { target: { value: "x" } })
    fireEvent.click(screen.getByRole("button", { name: "Pair" }))
    expect((await screen.findByRole("alert")).textContent).toContain("phrase did not match")
  })
})
