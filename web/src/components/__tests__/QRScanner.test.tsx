import { render, screen, fireEvent } from "@testing-library/react"
import { expect, it, vi } from "vitest"
import { QRScanner } from "../QRScanner"
vi.mock("html5-qrcode", () => ({ Html5Qrcode: class {start() { return Promise.reject(new Error("Permission denied")) } stop() {return Promise.resolve()} } }))
it("keeps camera failure and dismissal reachable in the scanner dialog", async () => {
  const close = vi.fn()
  render(<QRScanner onScan={vi.fn()} onClose={close} />)
  const dialog = screen.getByRole("dialog", {name:"Scan Pairing QR Code"})
  expect(dialog.contains(document.activeElement)).toBe(true)
  expect((await screen.findByRole("alert")).textContent).toContain("Camera access denied")
  fireEvent.keyDown(document.activeElement!, {key:"Escape"})
  expect(close).toHaveBeenCalledTimes(1)
})
