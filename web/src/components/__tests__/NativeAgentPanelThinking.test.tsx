import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { NativeAgentPanel } from "../NativeAgentPanel"

let websocketHandler: ((message: unknown) => void) | undefined

vi.mock("../../hooks/useWebSocket", () => ({
  useWebSocket: () => ({
    connected: true,
    send: vi.fn(),
    subscribe: (handler: (message: unknown) => void) => {
      websocketHandler = handler
      return () => { websocketHandler = undefined }
    },
  }),
}))

vi.mock("../../hooks/useServer", () => ({
  useServer: () => ({ baseUrl: "" }),
}))

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

// A session whose snapshot holds these messages after its own instructions.
function session(messages: unknown[], toolCalls: unknown[] = []) {
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => String(input).includes("/snapshot")
    ? new Response(JSON.stringify({
        id: "session-1", status: "idle", messages: [{ role: "user", content: "system" }, ...messages], toolCalls,
      }), { status: 200 })
    : new Response("[]", { status: 200 }))
  return render(<NativeAgentPanel agentId="task-1" />)
}

const arrives = (event: Record<string, unknown>) => act(() => websocketHandler?.({
  type: "chimera_event", agent_id: "task-1", content: JSON.stringify(event),
}))

const thoughts = (container: HTMLElement) => within(container).queryAllByRole("button", { name: /^Thinking/ })

describe("NativeAgentPanel and what an agent thinks", () => {
  it("shows a thought as one line that opens to all of it", async () => {
    const view = session([
      { role: "assistant", content: [
        { type: "reasoning", text: "**Planning the change**\n\nThe flag belongs in list.ts, next to --all." },
        { type: "text", text: "Done." },
      ] },
    ])

    const thought = await within(view.container).findByRole("button", { name: /^Thinking/ })
    expect(thought.textContent).toContain("Planning the change")
    expect(thought.textContent).not.toContain("**")
    expect(within(view.container).queryByText(/next to --all/)).toBeNull()

    fireEvent.click(thought)

    expect(within(view.container).getByText(/next to --all/)).toBeTruthy()
    expect(thought.getAttribute("aria-expanded")).toBe("true")
  })

  it("keeps tool calls in one group when the agent thinks between them", async () => {
    const view = session([
      { role: "assistant", content: [{ type: "reasoning", text: "Read the file first." }, { type: "tool-call" }] },
      { role: "assistant", content: [{ type: "reasoning", text: "Now run the tests." }, { type: "tool-call" }] },
    ], [
      { callId: "c1", name: "read", args: { path: "list.ts" }, result: { content: "" } },
      { callId: "c2", name: "bash", args: { command: "pnpm vitest" }, result: { stdout: "ok", exit_code: 0 } },
    ])

    const group = await within(view.container).findByRole("group", { name: "2 tool calls" })
    expect(thoughts(group).map((thought) => thought.textContent)).toEqual([
      expect.stringContaining("Read the file first."),
      expect.stringContaining("Now run the tests."),
    ])
  })

  it("still opens a lone edit's diff when a thought comes before it", async () => {
    const view = session([
      { role: "assistant", content: [{ type: "reasoning", text: "The old line is wrong." }, { type: "tool-call" }] },
    ], [
      { callId: "edit-1", name: "edit", args: { path: "main.go", old_string: "old line", new_string: "new line" } },
    ])

    expect(await within(view.container).findByText("- old line")).toBeTruthy()
    expect(thoughts(view.container)).toHaveLength(1)
  })

  it("folds the thoughts of a long run with the tool calls they led to", async () => {
    const steps = Array.from({ length: 8 }, (_, index) => index + 1)
    const view = session(
      steps.map((step) => ({ role: "assistant", content: [{ type: "reasoning", text: `Thought ${step}.` }, { type: "tool-call" }] })),
      steps.map((step) => ({ callId: `c${step}`, name: "bash", args: { command: `step ${step}` }, result: { stdout: "", exit_code: 0 } })),
    )

    const group = await within(view.container).findByRole("group", { name: "8 tool calls" })
    const earlier = within(group).getByRole("button", { name: /3 earlier tool calls/ })
    expect(thoughts(group).map((thought) => thought.textContent?.match(/Thought \d/)?.[0])).toEqual([
      "Thought 4", "Thought 5", "Thought 6", "Thought 7", "Thought 8",
    ])

    fireEvent.click(earlier)

    expect(thoughts(group)).toHaveLength(8)
  })

  it("shows a thought as it arrives, then settles on how it began", async () => {
    const view = session([])
    await screen.findByText("Session ready — send a message to start")

    arrives({ eventId: "1", type: "reasoning_text_delta", id: "r1", delta: "Checking the tests.\n" })
    arrives({ eventId: "2", type: "reasoning_text_delta", id: "r1", delta: "Now the build." })

    // While it thinks, the row follows the thought.
    const [thinking] = thoughts(view.container)
    expect(thinking.textContent).toContain("Now the build.")
    expect(thinking.textContent).not.toContain("Checking the tests.")

    arrives({ eventId: "3", type: "reasoning_text_done", id: "r1", text: "Checking the tests.\nNow the build." })

    const [thought] = thoughts(view.container)
    expect(thought.textContent).toContain("Checking the tests.")
    expect(thought.textContent).not.toContain("Now the build.")
  })

  it("draws a session that says nothing of its thinking as before", async () => {
    const view = session([
      { role: "assistant", content: [{ type: "reasoning", text: "" }, { type: "text", text: "Done." }] },
    ])

    await within(view.container).findByText("Done.")
    expect(thoughts(view.container)).toHaveLength(0)
  })
})
