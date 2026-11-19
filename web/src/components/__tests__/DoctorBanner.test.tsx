import { describe, it, expect, vi, afterEach, beforeEach } from "vitest"
import { render, screen, cleanup, fireEvent, waitFor } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"

const mockFetch = vi.fn()
const mockRecheck = vi.fn()
vi.mock("../../lib/doctor", async (importActual) => ({
  ...(await importActual<typeof import("../../lib/doctor")>()),
  fetchDoctor: (...a: unknown[]) => mockFetch(...a),
  recheckDoctor: (...a: unknown[]) => mockRecheck(...a),
}))

import { DoctorBanner } from "../DoctorBanner"
import type { DoctorReport } from "../../lib/doctor"

const tmuxMissing: DoctorReport = {
  ok: false, online: false, checked_at: "2026-11-19T12:00:00Z",
  checks: [
    { id: "tmux", title: "tmux", required: true, result: "missing", detail: "tmux not found on Legato's PATH", fix: "Install tmux (apt install tmux, brew install tmux)" },
    { id: "yg", title: "Yggdrasil (yg)", required: false, result: "missing", fix: "Install Yggdrasil" },
  ],
}
const fixed: DoctorReport = { ...tmuxMissing, ok: true, checks: [{ id: "tmux", title: "tmux", required: true, result: "ok" }] }

function renderBanner() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={client}><DoctorBanner baseUrl="http://legato" /></QueryClientProvider>)
}

describe("DoctorBanner", () => {
  beforeEach(() => {
    mockFetch.mockReset()
    mockRecheck.mockReset()
  })
  afterEach(() => cleanup())

  it("says which required check failed, with its fix", async () => {
    mockFetch.mockResolvedValue(tmuxMissing)
    renderBanner()

    const banner = await screen.findByRole("alert")
    expect(banner.textContent).toMatch(/tmux/)
    expect(banner.textContent).toMatch(/Install tmux/)
    expect(banner.textContent).not.toMatch(/Yggdrasil/)
  })

  it("goes once Check again passes", async () => {
    mockFetch.mockResolvedValue(tmuxMissing)
    mockRecheck.mockResolvedValue(fixed)
    renderBanner()

    fireEvent.click(await screen.findByRole("button", { name: "Check again" }))

    await waitFor(() => expect(screen.queryByRole("alert")).toBeNull())
    expect(mockRecheck).toHaveBeenCalledWith("http://legato")
  })

  it("stays away while every required check passes", async () => {
    mockFetch.mockResolvedValue(fixed)
    renderBanner()

    await waitFor(() => expect(mockFetch).toHaveBeenCalled())
    expect(screen.queryByRole("alert")).toBeNull()
  })
})
