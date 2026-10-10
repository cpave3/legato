import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { NativeTerminalPrompt } from "../NativeTerminalPrompt"

afterEach(cleanup)

describe("NativeTerminalPrompt", () => {
  const prompt = {
    type: "menu" as const,
    context: "Update available · 0.157.1 → 0.162.1\nRelease notes: https://example.test/releases",
    actions: [
      { label: "Update now", keys: "Enter" },
      { label: "Skip", keys: "Down Enter" },
      { label: "Cancel (Esc)", keys: "Escape" },
    ],
  }

  it("shows what the tool is asking in its terminal and answers it with the option's keys", () => {
    const onChoose = vi.fn()
    render(<NativeTerminalPrompt prompt={prompt} askedBy="Codex" onChoose={onChoose} />)
    const card = screen.getByRole("region", { name: "Codex is asking in its terminal" })

    expect(within(card).getByText(/Update available · 0\.157\.1 → 0\.162\.1/)).not.toBeNull()
    fireEvent.click(within(card).getByRole("button", { name: "Skip" }))

    expect(onChoose).toHaveBeenCalledWith("Down Enter")
  })

  it("sets the way out apart from the choices", () => {
    render(<NativeTerminalPrompt prompt={prompt} askedBy="Codex" onChoose={vi.fn()} />)

    const cancel = screen.getByRole("button", { name: "Cancel (Esc)" })
    expect(cancel.className).not.toBe(screen.getByRole("button", { name: "Skip" }).className)
  })

  it("can take the person to the terminal for what it cannot show", () => {
    const onOpenTerminal = vi.fn()
    render(<NativeTerminalPrompt prompt={prompt} askedBy="Codex" onChoose={vi.fn()} onOpenTerminal={onOpenTerminal} />)

    fireEvent.click(screen.getByRole("button", { name: "Open terminal" }))

    expect(onOpenTerminal).toHaveBeenCalledOnce()
  })
})
