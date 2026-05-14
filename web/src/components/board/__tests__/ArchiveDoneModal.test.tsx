import { describe, expect, it, vi } from "vitest"
import { render, screen, fireEvent } from "@testing-library/react"
import { ArchiveDoneModal } from "../ArchiveDoneModal"

describe("ArchiveDoneModal", () => {
  it("shows the count and calls onConfirm", () => {
    const onConfirm = vi.fn()
    render(
      <ArchiveDoneModal open={true} count={3} onClose={() => {}} onConfirm={onConfirm} />
    )
    expect(screen.getByText(/Archive 3 done cards/)).toBeTruthy()

    const btn = screen.getByRole("button", { name: /Archive/i })
    fireEvent.click(btn)
    expect(onConfirm).toHaveBeenCalled()
  })
})
