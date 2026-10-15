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

it("offers native Chimera upgrade and surfaces failures without hiding the terminal controls", async () => {
  mockFetch.mockResolvedValue({ ok: true, json: async () => ({ macros: [] }) })
  let rejectUpgrade: (error: Error) => void = () => {}
  const onUpgradeChimera = vi.fn(() => new Promise<void>((_, reject) => { rejectUpgrade = reject }))

  render(<PromptBar
    promptState={null}
    onSendKeys={vi.fn()}
    onSubmitMessage={vi.fn()}
    onDismissPrompt={vi.fn()}
    onDetectPrompt={vi.fn()}
    onDisconnect={vi.fn()}
    onKill={vi.fn()}
    onRefresh={vi.fn()}
    onTogglePromptDetection={vi.fn()}
    promptDetectionEnabled
    agentId="TASK-42"
    agentKind="chimera"
    onUpgradeChimera={onUpgradeChimera}
  />)

  fireEvent.click(screen.getByTitle("More actions"))
  fireEvent.click(screen.getByRole("menuitem", { name: "Upgrade to native Chimera" }))
  expect(onUpgradeChimera).toHaveBeenCalledOnce()
  fireEvent.click(screen.getByTitle("More actions"))
  expect(screen.getByRole("menuitem", { name: "Upgrading…" }).hasAttribute("disabled")).toBe(true)

  rejectUpgrade(new Error("no live Chimera instance for this task was found"))
  expect((await screen.findByRole("alert")).textContent).toContain("no live Chimera instance for this task was found")
  expect(screen.getByPlaceholderText("Type a message...")).not.toBeNull()
})

it("shows which agent tool a terminal session runs", () => {
  mockFetch.mockResolvedValue({ ok: true, json: async () => ({ macros: [] }) })
  render(<PromptBar
    promptState={null}
    onSendKeys={vi.fn()}
    onSubmitMessage={vi.fn()}
    onDismissPrompt={vi.fn()}
    onDetectPrompt={vi.fn()}
    onDisconnect={vi.fn()}
    onKill={vi.fn()}
    onRefresh={vi.fn()}
    onTogglePromptDetection={vi.fn()}
    promptDetectionEnabled
    agentId="TASK-42"
    agentKind="codex"
  />)

  expect(screen.getByLabelText("Codex, terminal session")).not.toBeNull()
})

it("leaves the agent tool out of the prompt bar where the mobile agent picker shows it", () => {
  mockFetch.mockResolvedValue({ ok: true, json: async () => ({ macros: [] }) })
  render(<PromptBar
    promptState={null}
    onSendKeys={vi.fn()}
    onSubmitMessage={vi.fn()}
    onDismissPrompt={vi.fn()}
    onDetectPrompt={vi.fn()}
    onDisconnect={vi.fn()}
    onKill={vi.fn()}
    onRefresh={vi.fn()}
    onTogglePromptDetection={vi.fn()}
    promptDetectionEnabled
    agentId="TASK-42"
    agentKind="codex"
  />)

  // The picker replaces the sidebar below md, and names the tool itself.
  const badge = screen.getByLabelText("Codex, terminal session")
  expect(badge.classList.contains("hidden")).toBe(true)
  expect(badge.classList.contains("md:inline-flex")).toBe(true)
  expect(badge.classList.contains("inline-flex")).toBe(false)
})

it("offers the native panel for Claude Code terminals", () => {
  mockFetch.mockResolvedValue({ ok: true, json: async () => ({ macros: [] }) })
  const onUpgradeChimera = vi.fn().mockResolvedValue(undefined)
  render(<PromptBar
    promptState={null}
    onSendKeys={vi.fn()}
    onSubmitMessage={vi.fn()}
    onDismissPrompt={vi.fn()}
    onDetectPrompt={vi.fn()}
    onDisconnect={vi.fn()}
    onKill={vi.fn()}
    onRefresh={vi.fn()}
    onTogglePromptDetection={vi.fn()}
    promptDetectionEnabled
    agentId="TASK-42"
    agentKind="claude-code"
    onUpgradeChimera={onUpgradeChimera}
  />)

  fireEvent.click(screen.getByTitle("More actions"))
  expect(screen.queryByRole("menuitem", { name: "Upgrade to native Chimera" })).toBeNull()
  fireEvent.click(screen.getByRole("menuitem", { name: "Upgrade to native panel" }))

  expect(onUpgradeChimera).toHaveBeenCalledOnce()
})

it("does not offer native upgrade for non-Chimera terminals", () => {
  mockFetch.mockResolvedValue({ ok: true, json: async () => ({ macros: [] }) })
  render(<PromptBar
    promptState={null}
    onSendKeys={vi.fn()}
    onSubmitMessage={vi.fn()}
    onDismissPrompt={vi.fn()}
    onDetectPrompt={vi.fn()}
    onDisconnect={vi.fn()}
    onKill={vi.fn()}
    onRefresh={vi.fn()}
    onTogglePromptDetection={vi.fn()}
    promptDetectionEnabled
    agentId="agent-1"
    agentKind="shell"
    onUpgradeChimera={vi.fn()}
  />)
  fireEvent.click(screen.getByTitle("More actions"))
  expect(screen.queryByRole("menuitem", { name: "Upgrade to native Chimera" })).toBeNull()
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

it("offers the native panel for Codex terminals", () => {
  mockFetch.mockResolvedValue({ ok: true, json: async () => ({ macros: [] }) })
  const onUpgradeChimera = vi.fn().mockResolvedValue(undefined)
  render(<PromptBar
    promptState={null}
    onSendKeys={vi.fn()}
    onSubmitMessage={vi.fn()}
    onDismissPrompt={vi.fn()}
    onDetectPrompt={vi.fn()}
    onDisconnect={vi.fn()}
    onKill={vi.fn()}
    onRefresh={vi.fn()}
    onTogglePromptDetection={vi.fn()}
    promptDetectionEnabled
    agentId="TASK-42"
    agentKind="codex"
    onUpgradeChimera={onUpgradeChimera}
  />)

  fireEvent.click(screen.getByTitle("More actions"))
  fireEvent.click(screen.getByRole("menuitem", { name: "Upgrade to native panel" }))

  expect(onUpgradeChimera).toHaveBeenCalledOnce()
})

it("offers the options of a menu the tool is showing in its terminal", () => {
  mockFetch.mockResolvedValue({ ok: true, json: async () => ({ macros: [] }) })
  const onSendKeys = vi.fn()
  render(<PromptBar
    promptState={{ type: "menu", context: "Update available", actions: [{ label: "Update now", keys: "Enter" }, { label: "Skip", keys: "Down Enter" }] }}
    onSendKeys={onSendKeys}
    onSubmitMessage={vi.fn()}
    onDismissPrompt={vi.fn()}
    onDetectPrompt={vi.fn()}
    onDisconnect={vi.fn()}
    onKill={vi.fn()}
    onRefresh={vi.fn()}
    onTogglePromptDetection={vi.fn()}
    promptDetectionEnabled
    agentId="TASK-42"
    agentKind="codex"
    agentActivity="waiting"
  />)

  fireEvent.click(screen.getByText("Skip"))

  expect(onSendKeys).toHaveBeenCalledWith("Down Enter")
})

// A terminal session has no conversation feed, but what its agent shows in
// the side panel can still propose a message.
it("puts a message proposed by the agent's content in the input and does not submit it", async () => {
  mockFetch.mockResolvedValue({ ok: true, json: async () => ({ macros: [] }) })
  const { act } = await import("@testing-library/react")
  const { proposeComposerText } = await import("../../lib/agentUi")
  const onSubmitMessage = vi.fn()
  const onSendKeys = vi.fn()
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
    connected
  />)
  const input = screen.getByRole("textbox") as HTMLTextAreaElement
  fireEvent.change(input, { target: { value: "Thanks." } })

  act(() => proposeComposerText("agent-2", "for another agent"))
  expect(input.value).toBe("Thanks.")

  act(() => proposeComposerText("agent-1", "Show the 7 day forecast"))

  expect(input.value).toBe("Thanks.\nShow the 7 day forecast")
  // The draft is kept like anything typed, and nothing was sent.
  expect(localStorage.getItem("legato:draft:agent-1")).toBe("Thanks.\nShow the 7 day forecast")
  expect(onSubmitMessage).not.toHaveBeenCalled()
  expect(onSendKeys).not.toHaveBeenCalled()
})

it("offers a terminal session's artifacts from its toolbar", () => {
  mockFetch.mockResolvedValue({ ok: true, json: async () => ({ macros: [] }) })
  const onToggle = vi.fn()
  render(<PromptBar
    promptState={null}
    onSendKeys={vi.fn()}
    onSubmitMessage={vi.fn()}
    onDismissPrompt={vi.fn()}
    onDetectPrompt={vi.fn()}
    onDisconnect={vi.fn()}
    onKill={vi.fn()}
    onRefresh={vi.fn()}
    onTogglePromptDetection={vi.fn()}
    promptDetectionEnabled
    agentId="agent-1"
    connected
    artifacts={{ count: 3, fresh: 0, onToggle }}
  />)

  fireEvent.click(screen.getByRole("button", { name: "Artifacts (3)" }))

  expect(onToggle).toHaveBeenCalledTimes(1)
})

// A terminal session takes a message as typed text. An image the person
// pastes is stored by Legato and the message names its path, as it does for
// a native session.
it("puts the path of a pasted image in the message it sends", async () => {
  mockFetch.mockImplementation(async (input: string, init?: RequestInit) => {
    if (init?.method === "POST" && input === "/api/agents/agent-1/image") {
      return new Response(JSON.stringify({ path: "/tmp/legato-1000/pasted-images/agent-1/pasted-1.png" }), { status: 201 })
    }
    return new Response(JSON.stringify({ macros: [] }), { status: 200 })
  })
  URL.createObjectURL = vi.fn(() => "blob:image-1")
  URL.revokeObjectURL = vi.fn()
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
  const box = screen.getByPlaceholderText("Type a message...") as HTMLTextAreaElement
  fireEvent.change(box, { target: { value: "Why is this red?" } })
  const image = new File([new Uint8Array([0x89, 0x50, 0x4e, 0x47])], "image.png", { type: "image/png" })

  fireEvent.paste(box, { clipboardData: { files: [image], getData: () => "" } })

  await waitFor(() => expect(box.value).toBe("Why is this red? /tmp/legato-1000/pasted-images/agent-1/pasted-1.png "))
  expect(screen.getByRole("img", { name: "Pasted image pasted-1.png" })).toBeTruthy()
  // The draft is kept with the path in it, like any other text.
  expect(localStorage.getItem("legato:draft:agent-1")).toBe(box.value)

  fireEvent.keyDown(box, { key: "Enter" })

  await waitFor(() => expect(onSubmitMessage).toHaveBeenCalledWith("Why is this red? /tmp/legato-1000/pasted-images/agent-1/pasted-1.png"))
})

it("says above the message box that the agent works, and interrupts it from there", () => {
  mockFetch.mockResolvedValue({ ok: true, json: async () => ({ macros: [] }) })
  const onSendKeys = vi.fn()

  render(<PromptBar
    promptState={null}
    onSendKeys={onSendKeys}
    onSubmitMessage={vi.fn()}
    onDismissPrompt={vi.fn()}
    onDetectPrompt={vi.fn()}
    onDisconnect={vi.fn()}
    onKill={vi.fn()}
    onRefresh={vi.fn()}
    onTogglePromptDetection={vi.fn()}
    promptDetectionEnabled
    agentId="agent-1"
    agentActivity="working"
  />)

  fireEvent.click(within(screen.getByRole("status", { name: "Agent working" })).getByRole("button", { name: "Interrupt" }))
  expect(onSendKeys).toHaveBeenCalledWith("Escape")
})
