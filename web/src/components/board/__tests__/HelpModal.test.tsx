import { describe, expect, it } from "vitest"
import { render, screen } from "@testing-library/react"
import { HelpModal } from "../HelpModal"

describe("HelpModal", () => {
  it("renders the keybinding reference section", () => {
    render(<HelpModal open={true} onClose={() => {}} />)
    expect(screen.getByText(/Keyboard Reference/)).toBeTruthy()
    expect(screen.getByText(/Navigation/)).toBeTruthy()
    expect(screen.getByText(/Actions/)).toBeTruthy()
    expect(screen.getByText(/Detail View/)).toBeTruthy()
  })
})
