import { fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { beforeEach, expect, it, vi } from "vitest"
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
