import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { NativeAgentPanel } from "../NativeAgentPanel"

const send = vi.fn(() => true)

vi.mock("../../hooks/useWebSocket", () => ({
  useWebSocket: () => ({
    connected: true,
    send,
    subscribe: () => () => {},
  }),
}))

vi.mock("../../hooks/useServer", () => ({
  useServer: () => ({ baseUrl: "" }),
}))

afterEach(() => {
  cleanup()
  send.mockClear()
  vi.restoreAllMocks()
  localStorage.clear()
})

// A session whose snapshot holds one tool call on its own, after a reply.
function oneCall(call: Record<string, unknown>, status = "idle") {
  return session([
    { role: "assistant", content: "Before." },
    { role: "assistant", content: [{ type: "tool-call", toolCallId: call.callId }] },
    { role: "assistant", content: "After." },
  ], [call], status)
}

function session(messages: unknown[], toolCalls: unknown[] = [], status = "idle") {
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => String(input).includes("/snapshot")
    ? new Response(JSON.stringify({ id: "s", status, messages: [{ role: "user", content: "system" }, ...messages], toolCalls }), { status: 200 })
    : new Response("[]", { status: 200 }))
  return render(<NativeAgentPanel agentId="task-1" />)
}

const conversation = () => screen.findByRole("region", { name: "Conversation" })

describe("NativeAgentPanel tool call cards", () => {
  it("draws an edit as its file, its count of replacements and its diff, the full path when it differs", async () => {
    oneCall({ callId: "e1", name: "edit", args: { path: "src/a/b.ts", old_string: "one", new_string: "two" }, result: { replacements: 1 } })
    const feed = await conversation()

    expect(await within(feed).findByText("b.ts")).toBeTruthy()
    expect(within(feed).getByText("(1 replacement)")).toBeTruthy()
    expect(within(feed).getByText("src/a/b.ts")).toBeTruthy()
    expect(within(feed).getByText("- one")).toBeTruthy()
    expect(within(feed).getByText("+ two")).toBeTruthy()

    fireEvent.click(within(feed).getByRole("button", { name: /^edit/ }))
    expect(within(feed).queryByText("- one")).toBeNull()
  })

  it("draws a write as its file, created and its size, and its content when opened", async () => {
    oneCall({ callId: "w1", name: "write", args: { path: "dir/notes.txt", content: "hello world" }, result: { bytes_written: 11, created: true } })
    const feed = await conversation()

    const row = await within(feed).findByRole("button", { name: /^write/ })
    expect(within(row).getByText("notes.txt")).toBeTruthy()
    expect(within(row).getByText("created")).toBeTruthy()
    expect(within(row).getByText("11B")).toBeTruthy()
    expect(within(feed).queryByText("hello world")).toBeNull()

    fireEvent.click(row)
    expect(within(feed).getByText("hello world")).toBeTruthy()
    expect(within(feed).getByText("dir/notes.txt")).toBeTruthy()
  })

  it("draws a bash call as its command cut at 80 characters and its exit code, and its output when opened", async () => {
    const command = "echo " + "x".repeat(90)
    oneCall({ callId: "b1", name: "bash", args: { command }, result: { stdout: "the output", stderr: "a warning", exit_code: 3 } })
    const feed = await conversation()

    const row = await within(feed).findByRole("button", { name: /^bash/ })
    expect(within(row).getByText(`${command.slice(0, 80)}…`)).toBeTruthy()
    expect(within(row).getByText("exit 3")).toBeTruthy()
    expect(within(feed).queryByText("the output")).toBeNull()

    fireEvent.click(row)
    expect(within(feed).getByText(`$ ${command}`)).toBeTruthy()
    expect(within(feed).getByText("the output")).toBeTruthy()
    expect(within(feed).getByText("a warning")).toBeTruthy()
  })

  it("draws a read as a line naming the file", async () => {
    oneCall({ callId: "r1", name: "read", args: { path: "/repo/docs/README.md" }, result: {} })
    const feed = await conversation()

    expect(await within(feed).findByText("README.md")).toBeTruthy()
    expect(within(feed).getByText("read")).toBeTruthy()
    expect(within(feed).queryByRole("button", { name: /^read/ })).toBeNull()
  })

  it("draws any other tool as its name and summary, with its arguments and result when opened", async () => {
    oneCall({ callId: "x1", name: "grep", args: { pattern: "needle" }, result: { matches: 3 }, display: { summary: "needle in src" } })
    const feed = await conversation()

    const row = await within(feed).findByRole("button", { name: /^grep/ })
    expect(within(row).getByText("needle in src")).toBeTruthy()

    fireEvent.click(row)
    expect(within(feed).getByText("Args")).toBeTruthy()
    expect(within(feed).getByText(/"pattern": "needle"/)).toBeTruthy()
    expect(within(feed).getByText("Result")).toBeTruthy()
    expect(within(feed).getByText(/"matches": 3/)).toBeTruthy()
    expect(within(feed).queryByText(/^MCP tool/)).toBeNull()
  })

  it("names the raw MCP tool when its call is opened", async () => {
    oneCall({ callId: "m1", name: "mcp__github__create_issue", args: {}, result: {} })
    const feed = await conversation()

    const row = await within(feed).findByTitle("mcp__github__create_issue")
    fireEvent.click(row)
    expect(within(feed).getByText(/^MCP tool/).textContent).toBe("MCP tool mcp__github__create_issue")
    expect(within(feed).queryByText("Args")).toBeNull()
  })
})

describe("NativeAgentPanel subagent card", () => {
  it("shows the subagent's settings, prompt and the steps it took", async () => {
    oneCall({
      callId: "s1", name: "spawn_agent",
      args: { purpose: "Look around", prompt: "Find the tests", agent: "explore", model: "small", mode: "fast" },
      result: { steps: 1, tool_calls_count: 4 },
    })
    const card = await screen.findByTestId("subagent-card-s1")

    expect(within(card).getByText("Look around")).toBeTruthy()
    expect(within(card).getByText("explore")).toBeTruthy()
    expect(within(card).getByText("small")).toBeTruthy()
    expect(within(card).getByText("fast")).toBeTruthy()
    expect(within(card).getByText("Find the tests")).toBeTruthy()
    expect(within(card).getByText("1 step · 4 tool calls")).toBeTruthy()

    fireEvent.click(within(card).getByRole("button", { name: /Look around/ }))
    expect(within(card).queryByText("Find the tests")).toBeNull()
  })

  it("falls back to the defaults for what the call leaves out", async () => {
    oneCall({ callId: "s2", name: "spawn_agent", args: {}, result: { steps: 2 } })
    const card = await screen.findByTestId("subagent-card-s2")

    expect(within(card).getByText("Subagent")).toBeTruthy()
    // Agent and mode both fall back to "default".
    expect(within(card).getAllByText("default")).toHaveLength(2)
    expect(within(card).getByText("inherited")).toBeTruthy()
    expect(within(card).getByText("2 steps")).toBeTruthy()
  })
})

describe("NativeAgentPanel shell command results", () => {
  it("shows a command that failed with its exit code, a timeout and its output", async () => {
    session([{ role: "assistant", content: [{ type: "text", text: "!make test (exit 2, timed out)\nboom" }] }])
    const feed = await conversation()

    expect(await within(feed).findByText("make test")).toBeTruthy()
    expect(within(feed).getByText("exit 2 (timed out)")).toBeTruthy()
    expect(within(feed).getByText("boom")).toBeTruthy()
  })
})

describe("NativeAgentPanel message box keys", () => {
  it("sends on Enter and leaves Shift+Enter to the box", async () => {
    session([])
    const box = await screen.findByRole("textbox", { name: "Message" })
    await waitFor(() => expect(send).toHaveBeenCalled())
    send.mockClear()

    fireEvent.change(box, { target: { value: "hello" } })
    fireEvent.keyDown(box, { key: "Enter", shiftKey: true })
    expect(send).not.toHaveBeenCalledWith(expect.objectContaining({ type: "agent_message" }))

    fireEvent.keyDown(box, { key: "Enter" })
    expect(send).toHaveBeenCalledWith(expect.objectContaining({ type: "agent_message", content: "hello", queue: false }))
    expect((box as HTMLTextAreaElement).value).toBe("")
  })

  it("sends nothing for a box of only spaces", async () => {
    session([])
    const box = await screen.findByRole("textbox", { name: "Message" })
    await waitFor(() => expect(send).toHaveBeenCalled())
    send.mockClear()

    fireEvent.change(box, { target: { value: "   " } })
    fireEvent.keyDown(box, { key: "Enter" })
    expect(send).not.toHaveBeenCalledWith(expect.objectContaining({ type: "agent_message" }))
    expect((box as HTMLTextAreaElement).value).toBe("   ")
  })
})
