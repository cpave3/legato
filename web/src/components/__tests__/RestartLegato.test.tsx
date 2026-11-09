import { describe, it, expect, vi, afterEach, beforeEach } from "vitest"
import { render, screen, cleanup, fireEvent, waitFor } from "@testing-library/react"

const mockRestart = vi.fn()
vi.mock("../../lib/restart", () => ({ restartLegato: (...a: unknown[]) => mockRestart(...a) }))

import { RestartLegato } from "../RestartLegato"

describe("RestartLegato", () => {
  beforeEach(() => { mockRestart.mockReset() })
  afterEach(() => cleanup())

  it("restarts after the person confirms", async () => {
    mockRestart.mockResolvedValue(undefined)
    render(<RestartLegato baseUrl="http://legato" />)

    fireEvent.click(screen.getByRole("button", { name: "Restart Legato" }))
    expect(mockRestart).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole("button", { name: "Restart" }))

    await waitFor(() => expect(mockRestart).toHaveBeenCalledWith("http://legato"))
    expect(screen.getByRole("status").textContent).toMatch(/Restarting/)
  })

  it("says why a restart was refused", async () => {
    mockRestart.mockRejectedValue(new Error("this Legato cannot restart itself"))
    render(<RestartLegato baseUrl="" />)

    fireEvent.click(screen.getByRole("button", { name: "Restart Legato" }))
    fireEvent.click(screen.getByRole("button", { name: "Restart" }))

    expect((await screen.findByRole("alert")).textContent).toMatch(/cannot restart/)
  })
})
