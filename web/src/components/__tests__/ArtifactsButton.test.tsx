import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { ArtifactsButton } from "../ArtifactsButton"

afterEach(cleanup)

describe("ArtifactsButton", () => {
  it("is absent until the agent has shown something", () => {
    const { container } = render(<ArtifactsButton count={0} fresh={0} onToggle={() => {}} />)
    expect(container.textContent).toBe("")
  })

  it("offers a studio as a tool of its own, apart from what the agent made", () => {
    const onToggle = vi.fn()
    const onOpen = vi.fn()
    render(<ArtifactsButton count={2} fresh={0} onToggle={onToggle} studio={{ onOpen }} />)

    // A studio is somewhere the person works, not one more thing to read:
    // it has its own button, and is not counted among the artifacts.
    fireEvent.click(screen.getByRole("button", { name: "Studio" }))
    expect(onOpen).toHaveBeenCalledTimes(1)
    expect(onToggle).not.toHaveBeenCalled()
    expect(screen.getByRole("button", { name: "Artifacts (2)" })).toBeTruthy()
  })

  it("offers the studio when it is all the agent has opened", () => {
    render(<ArtifactsButton count={0} fresh={0} onToggle={() => {}} studio={{ onOpen: () => {} }} />)
    expect(screen.getByRole("button", { name: "Studio" })).toBeTruthy()
    expect(screen.queryByRole("button", { name: /Artifacts/ })).toBeNull()
  })
})
