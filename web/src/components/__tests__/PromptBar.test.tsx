import { fireEvent, render, screen, waitFor } from "@testing-library/react"
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

it("renders macro names from the API and sends the selected keys", async () => {
  mockFetch.mockResolvedValue({
    ok: true,
    json: async () => ({ macros: [{ name: "Run tests", keys: "task test" }] }),
  })
  const onSendKeys = vi.fn()

  render(<PromptBar
    promptState={null}
    onSendKeys={onSendKeys}
    onSubmitText={vi.fn()}
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
  const item = await screen.findByRole("button", { name: /run tests/i })
  fireEvent.click(item)

  await waitFor(() => expect(onSendKeys).toHaveBeenCalledWith("task test"))
})
