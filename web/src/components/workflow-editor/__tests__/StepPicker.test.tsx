import { render, screen } from "@testing-library/react"
import { expect, it, vi } from "vitest"
import { StepPicker } from "../StepPicker"

// On a phone the picker is a sheet from the bottom, drawn over the page's
// fixed Save bar (z-50), so its Cancel and last entries can be touched.
it("is a phone sheet above the Save bar", () => {
  render(<StepPicker narrow onPick={vi.fn()} onClose={vi.fn()} />)
  expect(screen.getByRole("dialog", { name: "Add a step" }).className).toContain("z-[60]")
})
