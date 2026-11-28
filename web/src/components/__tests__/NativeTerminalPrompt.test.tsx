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

  const question = {
    type: "menu" as const,
    context: "☐ Colour\nWhich colour?",
    actions: [
      { label: "Red", keys: "Enter" },
      { label: "Type something.", keys: "Down Enter", takes_text: true, text_submits: true },
      { label: "Cancel (Esc)", keys: "Escape" },
    ],
  }

  it("answers a row that takes text with what the operator types", () => {
    const onChoose = vi.fn()
    render(<NativeTerminalPrompt prompt={question} askedBy="Claude Code" onChoose={onChoose} />)

    const field = screen.getByRole("textbox", { name: "Type something." })
    const sendButton = screen.getByRole("button", { name: "Send answer" })
    expect((sendButton as HTMLButtonElement).disabled).toBe(true)
    fireEvent.change(field, { target: { value: "Purple" } })
    fireEvent.click(sendButton)

    expect(onChoose).toHaveBeenCalledWith("Down Enter", "Purple")
    expect(screen.queryByRole("button", { name: "Type something." })).toBeNull()
  })

  it("holds its buttons while an answer is on its way", () => {
    const onChoose = vi.fn()
    render(<NativeTerminalPrompt prompt={question} askedBy="Claude Code" onChoose={onChoose} pending />)

    expect(screen.getByText("Answering…")).not.toBeNull()
    const red = screen.getByRole("button", { name: "Red" }) as HTMLButtonElement
    expect(red.disabled).toBe(true)
    fireEvent.click(red)
    expect(onChoose).not.toHaveBeenCalled()
  })

  it("says when the terminal did not take an answer", () => {
    render(<NativeTerminalPrompt prompt={question} askedBy="Claude Code" onChoose={vi.fn()} notice="The terminal didn't take that answer." />)

    expect(screen.getByRole("status").textContent).toContain("The terminal didn't take that answer.")
    expect((screen.getByRole("button", { name: "Red" }) as HTMLButtonElement).disabled).toBe(false)
  })
})
