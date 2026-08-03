import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { InlineNotice } from "../InlineNotice"

describe("InlineNotice", () => {
  it("announces outcomes without treating successful messages as warnings", () => {
    const { rerender } = render(<InlineNotice variant="success">Question sent</InlineNotice>)

    const status = screen.getByRole("status")
    expect(status.textContent).toBe("Question sent")
    expect(status.className).toContain("emerald")

    rerender(<InlineNotice variant="warning">The agent returned a warning</InlineNotice>)
    expect(screen.getByRole("status").className).toContain("amber")
  })

  it("announces errors as alerts", () => {
    render(<InlineNotice variant="error">Request failed</InlineNotice>)

    expect(screen.getByRole("alert").textContent).toBe("Request failed")
  })
})
