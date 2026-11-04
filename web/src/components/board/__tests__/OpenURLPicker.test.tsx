import { describe, expect, it, vi } from "vitest"
import { render, fireEvent } from "@testing-library/react"
import { OpenURLPicker } from "../OpenURLPicker"

describe("OpenURLPicker", () => {
  it("j/g/Esc keybindings work", () => {
    const onSelect = vi.fn()
    const onClose = vi.fn()
    render(
      <OpenURLPicker
        open={true}
        providerURL="https://jira.example.com/T-1"
        prURL="https://github.com/org/repo/pull/1"
        onSelect={onSelect}
        onClose={onClose}
      />
    )

    fireEvent.keyDown(window, { key: "j" })
    expect(onSelect).toHaveBeenCalledWith("https://jira.example.com/T-1")

    onSelect.mockClear()
    fireEvent.keyDown(window, { key: "g" })
    expect(onSelect).toHaveBeenCalledWith("https://github.com/org/repo/pull/1")

    fireEvent.keyDown(window, { key: "Escape" })
    expect(onClose).toHaveBeenCalled()
  })

  it("names the task's provider", () => {
    const { getByText } = render(
      <OpenURLPicker open providerURL="https://burnlite.example/EST-42" provider="burnlite" prURL="https://github.com/org/repo/pull/1" onSelect={() => {}} onClose={() => {}} />
    )
    expect(getByText("Burnlite task")).toBeTruthy()
  })
})
