import { fireEvent, render, screen } from "@testing-library/react"
import { expect, it, vi } from "vitest"
import { ReadImageRow } from "../ReadImageRow"
vi.mock("../../hooks/useServer", () => ({useServer: () => ({baseUrl:""})}))
vi.mock("../../lib/agentImage", () => ({fetchAgentImage: vi.fn().mockResolvedValue(new Blob(["image"]))}))
it("makes only the full-size image modal and restores its thumbnail", async () => {
  vi.stubGlobal("URL", class extends URL {
    static createObjectURL = vi.fn(() => "blob:image")
    static revokeObjectURL = vi.fn()
  })
  render(<ReadImageRow agentId="a" path="/tmp/shot.png" grouped={false} />)
  fireEvent.click(screen.getByRole("button"))
  const thumbnail = await screen.findByRole("button", {name:"Show shot.png full size"})
  expect(document.body.style.overflow).not.toBe("hidden")
  thumbnail.focus()
  fireEvent.click(thumbnail)
  const close = screen.getByRole("button", {name:"Close image"})
  expect(document.activeElement).toBe(close)
  expect(document.body.style.overflow).toBe("hidden")
  fireEvent.keyDown(close, {key:"Tab"})
  expect(document.activeElement).toBe(close)
  fireEvent.keyDown(close, {key:"Escape"})
  expect(screen.queryByRole("dialog")).toBeNull()
  expect(document.activeElement).toBe(thumbnail)
  vi.unstubAllGlobals()
})
