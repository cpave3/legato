import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { StartSwarmModal } from "../StartSwarmModal"

const { apiFetch, startSwarm, started } = vi.hoisted(() => ({ apiFetch: vi.fn(), startSwarm: vi.fn(), started: vi.fn() }))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../hooks/useSettings", () => ({ useSettings: () => ({ data: { repositories: [] } }) }))
vi.mock("../../hooks/useToast", () => ({ useToast: () => ({ addToast: vi.fn() }) }))
vi.mock("../../lib/api", () => ({ apiFetch }))
vi.mock("../../lib/swarm", () => ({ startSwarm }))

beforeEach(() => {
  apiFetch.mockReset()
  startSwarm.mockReset().mockResolvedValue(undefined)
  started.mockReset()
})
afterEach(cleanup)

it("locks a fixed task, uses its initial directory, and skips the task picker request", async () => {
  render(<StartSwarmModal open onClose={() => {}} preSelectedParentId="T-1" initialWorkingDir="/trees/t-1" onStarted={started} />)

  expect(screen.getByRole("dialog", { name: "Start Swarm" })).toBeTruthy()
  expect(screen.getByText("T-1")).toBeTruthy()
  expect((screen.getByLabelText("Working Directory") as HTMLInputElement).value).toBe("/trees/t-1")
  expect(apiFetch).not.toHaveBeenCalled()
  fireEvent.click(screen.getByRole("button", { name: "Start Swarm" }))

  await waitFor(() => expect(startSwarm).toHaveBeenCalledWith("", "T-1", "/trees/t-1"))
  expect(started).toHaveBeenCalled()
})
