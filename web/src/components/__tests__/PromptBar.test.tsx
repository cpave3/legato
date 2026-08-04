import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { PromptBar } from "../PromptBar"

const mockFetch = vi.fn()
vi.stubGlobal("fetch", mockFetch)

vi.mock("../../hooks/useServer", () => ({
  useServer: () => ({ baseUrl: "" }),
}))

beforeEach(() => {
  mockFetch.mockReset()
  localStorage.clear()
})

afterEach(() => {
  cleanup()
})

it("submits macros through the chat interface without sending raw keys", async () => {
  const macro = "!legato task show $LEGATO_TASK_ID --format full"
  mockFetch.mockResolvedValue({
    ok: true,
    json: async () => ({ macros: [{ name: "Show task", keys: macro }] }),
  })
  const onSendKeys = vi.fn()
  const onSubmitMessage = vi.fn()

  render(<PromptBar
    promptState={null}
    onSendKeys={onSendKeys}
    onSubmitMessage={onSubmitMessage}
    onDismissPrompt={vi.fn()}
    onDetectPrompt={vi.fn()}
    onDisconnect={vi.fn()}
    onKill={vi.fn()}
    onRefresh={vi.fn()}
    onTogglePromptDetection={vi.fn()}
    promptDetectionEnabled
    agentId="agent-1"
  />)

  fireEvent.click(screen.getByRole("button", { name: /macros/i }))
  const item = await screen.findByRole("button", { name: /show task/i })
  fireEvent.click(item)

  await waitFor(() => expect(onSubmitMessage).toHaveBeenCalledOnce())
  expect(onSubmitMessage).toHaveBeenCalledWith(macro)
  expect(onSendKeys).not.toHaveBeenCalled()
})

it("opens with M and submits the fourth macro through the chat interface", async () => {
  const macros = Array.from({ length: 10 }, (_, index) => ({
    name: `Macro ${index + 1}`,
    keys: `macro-${index + 1}`,
  }))
  mockFetch.mockResolvedValue({
    ok: true,
    json: async () => ({ macros }),
  })
  const onSendKeys = vi.fn()
  const onSubmitMessage = vi.fn()

  render(<PromptBar
    promptState={null}
    onSendKeys={onSendKeys}
    onSubmitMessage={onSubmitMessage}
    onDismissPrompt={vi.fn()}
    onDetectPrompt={vi.fn()}
    onDisconnect={vi.fn()}
    onKill={vi.fn()}
    onRefresh={vi.fn()}
    onTogglePromptDetection={vi.fn()}
    promptDetectionEnabled
    agentId="agent-1"
  />)

  const textarea = screen.getByPlaceholderText("Type a message...")
  textarea.blur()
  await waitFor(() => expect(mockFetch).toHaveBeenCalledWith("/api/macros", expect.any(Object)))
  fireEvent.keyDown(window, { key: "M" })

  for (let index = 1; index <= 9; index++) {
    const item = screen.getByRole("button", { name: new RegExp(`${index}.*macro ${index}`, "i") })
    expect(item.getAttribute("aria-keyshortcuts")).toBe(String(index))
  }
  const tenth = screen.getByRole("button", { name: "Macro 10" })
  expect(tenth.getAttribute("aria-keyshortcuts")).toBeNull()
  expect(within(tenth).queryByText("10")).toBeNull()
  fireEvent.click(tenth)
  expect(onSubmitMessage).toHaveBeenCalledWith("macro-10")
  onSubmitMessage.mockClear()

  fireEvent.keyDown(window, { key: "m" })
  fireEvent.keyDown(window, { key: "4" })

  expect(onSubmitMessage).toHaveBeenCalledOnce()
  expect(onSubmitMessage).toHaveBeenCalledWith("macro-4")
  expect(onSendKeys).not.toHaveBeenCalled()
  expect(screen.queryByRole("button", { name: /macro 4/i })).toBeNull()
})

it("does not handle macro shortcuts from the textarea or with modifiers", async () => {
  mockFetch.mockResolvedValue({
    ok: true,
    json: async () => ({ macros: [{ name: "First macro", keys: "first" }] }),
  })
  const onSubmitMessage = vi.fn()

  render(<PromptBar
    promptState={null}
    onSendKeys={vi.fn()}
    onSubmitMessage={onSubmitMessage}
    onDismissPrompt={vi.fn()}
    onDetectPrompt={vi.fn()}
    onDisconnect={vi.fn()}
    onKill={vi.fn()}
    onRefresh={vi.fn()}
    onTogglePromptDetection={vi.fn()}
    promptDetectionEnabled
    agentId="agent-1"
  />)

  const textarea = screen.getByPlaceholderText("Type a message...")
  await waitFor(() => expect(mockFetch).toHaveBeenCalled())
  fireEvent.keyDown(textarea, { key: "m" })
  expect(screen.queryByRole("button", { name: /first macro/i })).toBeNull()

  textarea.blur()
  for (const modifier of ["ctrlKey", "altKey", "metaKey", "shiftKey"] as const) {
    fireEvent.keyDown(window, { key: "m", [modifier]: true })
    fireEvent.keyDown(window, { key: "1", [modifier]: true })
  }
  fireEvent.keyDown(window, { key: "m", repeat: true })

  expect(screen.queryByRole("button", { name: /first macro/i })).toBeNull()
  expect(onSubmitMessage).not.toHaveBeenCalled()
})

it("submits typed text as message content without a terminal newline", () => {
  mockFetch.mockResolvedValue({
    ok: true,
    json: async () => ({ macros: [] }),
  })
  const onSubmitMessage = vi.fn()

  const view = render(<PromptBar
    promptState={null}
    onSendKeys={vi.fn()}
    onSubmitMessage={onSubmitMessage}
    onDismissPrompt={vi.fn()}
    onDetectPrompt={vi.fn()}
    onDisconnect={vi.fn()}
    onKill={vi.fn()}
    onRefresh={vi.fn()}
    onTogglePromptDetection={vi.fn()}
    promptDetectionEnabled
    agentId="agent-1"
  />)

  const promptBar = within(view.container)
  fireEvent.change(promptBar.getByPlaceholderText("Type a message..."), { target: { value: "  hello agent  " } })
  fireEvent.click(promptBar.getByRole("button", { name: "Send" }))

  expect(onSubmitMessage).toHaveBeenCalledWith("hello agent")
})
