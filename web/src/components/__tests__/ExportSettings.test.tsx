import { describe, it, expect, vi, afterEach, beforeEach } from "vitest"
import { render, screen, cleanup, fireEvent, waitFor } from "@testing-library/react"

const mockExport = vi.fn()
vi.mock("../../lib/configSettings", async (importActual) => ({
  ...(await importActual<typeof import("../../lib/configSettings")>()),
  exportSettings: (...a: unknown[]) => mockExport(...a),
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
})
