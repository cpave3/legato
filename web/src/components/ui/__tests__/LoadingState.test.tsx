import { cleanup, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"

import { LoadingState } from "../LoadingState"

describe("LoadingState", () => {
  afterEach(cleanup)

  it("announces what is loading, with the detail under it", () => {
    render(<LoadingState label="Loading the session" detail="Claude Code · fekz3ove" />)

    const status = screen.getByRole("status")
    expect(status.getAttribute("aria-label")).toBe("Loading the session")
    expect(status.textContent).toContain("Loading the session")
    expect(status.textContent).toContain("Claude Code · fekz3ove")
  })

  it("fills the space it is given, in a row or a column, and centres in it", () => {
    render(<LoadingState label="Loading" />)

    const classes = screen.getByRole("status").className.split(" ")
    // flex-1 takes the space of a flex parent either way; w-full and h-full
    // take it from a block parent.
    for (const name of ["flex-1", "w-full", "h-full", "items-center", "justify-center", "text-center"]) {
      expect(classes).toContain(name)
    }
  })

  it("sits on a line of its own when it is small", () => {
    render(<LoadingState label="Loading workflows" size="inline" />)

    const classes = screen.getByRole("status").className.split(" ")
    expect(classes).toContain("inline-flex")
    expect(classes).not.toContain("flex-1")
  })
})
