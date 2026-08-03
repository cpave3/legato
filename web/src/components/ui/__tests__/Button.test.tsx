import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { Button } from "../Button"

describe("Button", () => {
  it("provides consistent native button behavior", () => {
    const onClick = vi.fn()
    const { rerender } = render(
      <Button variant="destructive" size="icon" aria-label="Delete task" onClick={onClick}>
        ×
      </Button>,
    )

    const button = screen.getByRole("button", { name: "Delete task" })
    expect(button.getAttribute("type")).toBe("button")
    expect(button.className).toContain("size-11")
    fireEvent.click(button)
    expect(onClick).toHaveBeenCalledOnce()

    rerender(
      <Button disabled onClick={onClick}>
        Save
      </Button>,
    )
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    expect(onClick).toHaveBeenCalledOnce()
  })

  it("styles a child link without changing its semantics", () => {
    render(
      <Button asChild variant="outline" size="sm">
        <a href="/plans">View plans</a>
      </Button>,
    )

    const link = screen.getByRole("link", { name: "View plans" })
    expect(link.getAttribute("href")).toBe("/plans")
    expect(link.className).toContain("min-h-9")
  })
})
