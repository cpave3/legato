import { describe, it, expect, vi, afterEach, beforeEach } from "vitest"
import { render, screen, cleanup, fireEvent, waitFor } from "@testing-library/react"

const mockExport = vi.fn()
const mockImport = vi.fn()
vi.mock("../../lib/configSettings", async (importActual) => ({
  ...(await importActual<typeof import("../../lib/configSettings")>()),
  exportSettings: (...a: unknown[]) => mockExport(...a),
  importSettings: (...a: unknown[]) => mockImport(...a),
}))

import { ExportSettings } from "../settings/ExportSettings"

describe("ExportSettings", () => {
  beforeEach(() => { mockExport.mockReset(); mockExport.mockResolvedValue("agents:\n  reviews: false\n") })
  afterEach(() => cleanup())

  it("downloads the settings, with config.yaml's when asked", async () => {
    const save = vi.fn()
    render(<ExportSettings baseUrl="http://legato" save={save} />)
    fireEvent.click(screen.getByRole("checkbox", { name: "Include settings from config.yaml" }))
    fireEvent.click(screen.getByRole("button", { name: "Export settings" }))
    await waitFor(() => expect(save).toHaveBeenCalledWith("legato-settings.yaml", "agents:\n  reviews: false\n"))
    expect(mockExport).toHaveBeenCalledWith("http://legato", true)
  })

  it("imports a file as one change set to approve", async () => {
    mockImport.mockResolvedValue({ id: "cs-1", approval_id: "ap-1", level: "approve", calls: 3, skipped: ["web.port"] })
    render(<ExportSettings baseUrl="http://legato" />)
    const file = new File(["agents:\n  reviews: false\n"], "settings.yaml")
    fireEvent.change(screen.getByLabelText("Import settings"), { target: { files: [file] } })
    const status = await screen.findByRole("status")
    expect(status.textContent).toContain("3 changes wait for your approval")
    expect(status.textContent).toContain("web.port")
    expect(mockImport).toHaveBeenCalledWith("http://legato", "agents:\n  reviews: false\n")
  })
})
