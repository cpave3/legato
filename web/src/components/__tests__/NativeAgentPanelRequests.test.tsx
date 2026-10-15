import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { NativeAgentPanel } from "../NativeAgentPanel"

const send = vi.fn()
let websocketHandler: ((message: unknown) => void) | undefined

vi.mock("../../hooks/useWebSocket", () => ({
  useWebSocket: () => ({
    connected: true,
    send,
    subscribe: (handler: (message: unknown) => void) => {
      websocketHandler = handler
      return () => { websocketHandler = undefined }
    },
  }),
}))

vi.mock("../../hooks/useServer", () => ({
  useServer: () => ({ baseUrl: "" }),
}))

const colorQuestion = {
  id: "q1",
  header: "Color",
  question: "Which color?",
  options: [{ label: "Red" }, { label: "Blue" }],
  multiSelect: false,
  allowOther: true,
}

function mockSnapshot(snapshot: Record<string, unknown>) {
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => String(input).includes("/snapshot")
    ? new Response(JSON.stringify({ id: "task-1", messages: [], toolCalls: [], pendingPermissions: [], ...snapshot }), { status: 200 })
    : new Response("[]", { status: 200 }))
}

beforeEach(() => send.mockReset())

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

describe("NativeAgentPanel approvals", () => {
  const planRequest = {
    requestId: "req-plan",
    tool: "ExitPlanMode",
    target: "host",
    command: "# Add a comment\n\n- Edit README.md",
    cwd: "/repo",
    plan: "# Add a comment\n\n- Edit README.md",
    options: [
      { id: "accept_edits", label: "Approve, and accept edits without asking" },
      { id: "manual", label: "Approve, and ask before each edit" },
    ],
  }

  it("shows a plan as written and approves it the way the person picks", async () => {
    mockSnapshot({ status: "waiting_for_permission", pendingPermissions: [planRequest] })

    render(<NativeAgentPanel agentId="task-1" agentKind="claude-code" />)
    const card = await screen.findByRole("region", { name: "Permission request for ExitPlanMode" })

    // Drawn as the Markdown it is, not as the raw text of a command.
    expect(within(card).getByRole("heading", { name: "Add a comment" })).not.toBeNull()
    expect(within(card).getByText("Plan approval")).not.toBeNull()
    expect(within(card).queryByRole("button", { name: "Allow" })).toBeNull()

    fireEvent.click(within(card).getByRole("button", { name: "Approve, and ask before each edit" }))

    expect(send).toHaveBeenCalledWith({
      type: "agent_permission", agent_id: "task-1", request_id: "req-plan", decision: "allow", option: "manual",
    })
  })

  it("sends a plan back with what should change", async () => {
    mockSnapshot({ status: "waiting_for_permission", pendingPermissions: [planRequest] })

    render(<NativeAgentPanel agentId="task-1" agentKind="claude-code" />)
    const card = await screen.findByRole("region", { name: "Permission request for ExitPlanMode" })
    fireEvent.click(within(card).getByRole("button", { name: "Request changes" }))
    fireEvent.change(within(card).getByLabelText("Feedback for Claude Code"), { target: { value: "Add a test step" } })
    fireEvent.click(within(card).getByRole("button", { name: "Send denial" }))

    expect(send).toHaveBeenCalledWith({
      type: "agent_permission", agent_id: "task-1", request_id: "req-plan", decision: "deny", feedback: "Add a test step",
    })
  })

  it("offers the agent's own ways to allow a tool call and records the one taken", async () => {
    mockSnapshot({
      status: "waiting_for_permission",
      pendingPermissions: [{
        requestId: "req-1", tool: "Bash", target: "host", command: "npm run build", cwd: "/repo",
        options: [
          { id: "once", label: "Allow once" },
          { id: "always", label: "Allow, and don't ask again for Bash(npm run:*) in this project" },
        ],
      }],
    })

    render(<NativeAgentPanel agentId="task-1" agentKind="claude-code" />)
    const card = await screen.findByRole("region", { name: "Permission request for Bash" })
    expect(within(card).getByText("npm run build")).not.toBeNull()
    expect(within(card).getByRole("button", { name: "Allow once" })).not.toBeNull()
    fireEvent.click(within(card).getByRole("button", { name: /don't ask again for Bash\(npm run:\*\)/ }))

    expect(send).toHaveBeenCalledWith({
      type: "agent_permission", agent_id: "task-1", request_id: "req-1", decision: "allow", option: "always",
    })

    act(() => websocketHandler?.({
      type: "chimera_event",
      agent_id: "task-1",
      content: JSON.stringify({ eventId: "resolved-1", type: "permission_resolved", requestId: "req-1", decision: "allow", option: "always" }),
    }))

    expect(within(card).getByText("Allowed · Allow, and don't ask again for Bash(npm run:*) in this project")).not.toBeNull()
  })

  it("keeps the single Allow for a request that lists no options", async () => {
    mockSnapshot({
      status: "waiting_for_permission",
      pendingPermissions: [{ requestId: "req-1", tool: "bash", target: "host", command: "ls", cwd: "/repo" }],
    })

    render(<NativeAgentPanel agentId="task-1" />)
    const card = await screen.findByRole("region", { name: "Permission request for bash" })
    fireEvent.click(within(card).getByRole("button", { name: "Allow" }))

    expect(send).toHaveBeenCalledWith({ type: "agent_permission", agent_id: "task-1", request_id: "req-1", decision: "allow" })
  })
})

describe("NativeAgentPanel for a Codex session", () => {
  it("names Codex wherever it names the tool", async () => {
    mockSnapshot({
      status: "waiting_for_permission",
      pendingPermissions: [{ requestId: "req-1", tool: "bash", target: "host", command: "npm run build", cwd: "/repo" }],
      pendingQuestions: [{ requestId: "req-2", questions: [colorQuestion] }],
    })

    render(<NativeAgentPanel agentId="task-1" agentKind="codex" />)

    expect(await screen.findByRole("region", { name: "Question from Codex" })).not.toBeNull()
    const approval = screen.getByRole("region", { name: "Permission request for bash" })
    expect(within(approval).getByText("Tool approval")).not.toBeNull()
    expect(screen.getByLabelText("Codex, native session")).not.toBeNull()
  })

  it("draws a file change as the diff of each file", async () => {
    mockSnapshot({
      status: "idle",
      messages: [{ role: "user", content: "system" }, { role: "assistant", content: [{ type: "tool-call" }] }],
      toolCalls: [{
        callId: "patch-1", name: "patch", display: { summary: "a.go, new.txt" }, result: { ok: true },
        args: { changes: [
          { path: "/repo/a.go", kind: "update", diff: "@@ -1 +1 @@\n-x := 1\n+x := 2\n" },
          { path: "/repo/new.txt", kind: "add", diff: "hello\n" },
        ] },
      }],
    })

    const view = render(<NativeAgentPanel agentId="task-1" agentKind="codex" />)

    // A change that stands alone is open, as an edit is.
    expect(await within(view.container).findByText("-x := 1")).not.toBeNull()
    expect(within(view.container).getByText("+x := 2").className).toContain("text-green-300")
    expect(within(view.container).getByText("/repo/new.txt")).not.toBeNull()
    // A new file is all additions.
    expect(within(view.container).getByText("+hello").className).toContain("text-green-300")
  })
})

describe("NativeAgentPanel and prompts only the terminal shows", () => {
  const menu = {
    type: "menu",
    context: "Update available · 0.157.1 → 0.162.1",
    actions: [{ label: "Update now", keys: "Enter" }, { label: "Skip", keys: "Down Enter" }],
  }
  const terminalAsks = (prompt: unknown) => act(() => websocketHandler?.({ type: "prompt_state", agent_id: "task-1", prompt }))

  it("asks what the terminal is showing, shows it, and answers with keys", async () => {
    mockSnapshot({ status: "idle" })
    render(<NativeAgentPanel agentId="task-1" agentKind="codex" />)
    await screen.findByText("Session ready — send a message to start")

    expect(send).toHaveBeenCalledWith({ type: "detect_prompt", agent_id: "task-1" })
    terminalAsks(menu)
    const card = screen.getByRole("region", { name: "Codex is asking in its terminal" })
    expect(screen.getByRole("button", { name: "Needs you" })).not.toBeNull()
    send.mockClear()
    fireEvent.click(within(card).getByRole("button", { name: "Skip" }))

    expect(send).toHaveBeenCalledWith({ type: "send_keys", agent_id: "task-1", keys: "Down Enter" })
    // Answered: the card goes at once and the terminal is read again.
    expect(screen.queryByRole("region", { name: "Codex is asking in its terminal" })).toBeNull()
    await waitFor(() => expect(send).toHaveBeenCalledWith({ type: "detect_prompt", agent_id: "task-1" }))
  })

  it("drops the card when the terminal is no longer asking", async () => {
    mockSnapshot({ status: "idle" })
    render(<NativeAgentPanel agentId="task-1" agentKind="codex" />)
    await screen.findByText("Session ready — send a message to start")
    terminalAsks(menu)
    expect(screen.getByRole("region", { name: "Codex is asking in its terminal" })).not.toBeNull()

    terminalAsks({ type: "free_text" })

    expect(screen.queryByRole("region", { name: "Codex is asking in its terminal" })).toBeNull()
    expect(screen.queryByRole("button", { name: "Needs you" })).toBeNull()
  })

  it("leaves a prompt to its own card when the agent raised one", async () => {
    mockSnapshot({
      status: "waiting_for_permission",
      pendingPermissions: [{ requestId: "req-1", tool: "bash", target: "host", command: "npm run build", cwd: "/repo" }],
    })
    render(<NativeAgentPanel agentId="task-1" agentKind="codex" />)
    await screen.findByRole("region", { name: "Permission request for bash" })

    // The terminal shows the same request as a menu of its own.
    terminalAsks({ type: "menu", context: "Would you like to run the following command?", actions: [{ label: "Yes, proceed (y)", keys: "Enter" }, { label: "No (esc)", keys: "Down Enter" }] })

    expect(screen.queryByRole("region", { name: "Codex is asking in its terminal" })).toBeNull()
  })

  it("ignores what another agent's terminal is showing", async () => {
    mockSnapshot({ status: "idle" })
    render(<NativeAgentPanel agentId="task-1" agentKind="codex" />)
    await screen.findByText("Session ready — send a message to start")

    act(() => websocketHandler?.({ type: "prompt_state", agent_id: "task-2", prompt: menu }))

    expect(screen.queryByRole("region", { name: /is asking in its terminal/ })).toBeNull()
  })
})

describe("NativeAgentPanel waiting on the person", () => {
  it("says so in the header and brings the request into view", async () => {
    mockSnapshot({ status: "waiting_for_input", pendingQuestions: [{ requestId: "req-1", questions: [colorQuestion] }] })
    let scrollTop = 0
    vi.spyOn(HTMLElement.prototype, "scrollHeight", "get").mockReturnValue(1200)
    vi.spyOn(HTMLElement.prototype, "clientHeight", "get").mockReturnValue(300)
    vi.spyOn(HTMLElement.prototype, "scrollTop", "get").mockImplementation(() => scrollTop)
    vi.spyOn(HTMLElement.prototype, "scrollTop", "set").mockImplementation((value) => { scrollTop = value })

    render(<NativeAgentPanel agentId="task-1" agentKind="claude-code" />)
    const waiting = await screen.findByRole("button", { name: "Needs you" })
    // The chip says it needs the person, not idle or running.
    expect(waiting.textContent).toContain("Needs you")
    expect(screen.queryByText(/^○ Idle|^● Running/)).toBeNull()

    // The request may be far below what is on screen.
    scrollTop = 100
    fireEvent.click(waiting)

    expect(scrollTop).toBe(1200)
  })

  it("goes back to the session's own state once the request is settled", async () => {
    mockSnapshot({
      status: "waiting_for_permission",
      pendingPermissions: [{ requestId: "req-1", tool: "bash", target: "host", command: "ls", cwd: "/repo" }],
    })

    render(<NativeAgentPanel agentId="task-1" />)
    await screen.findByRole("button", { name: "Needs you" })
    act(() => websocketHandler?.({
      type: "chimera_event",
      agent_id: "task-1",
      content: JSON.stringify({ eventId: "resolved-1", type: "permission_resolved", requestId: "req-1", decision: "allow" }),
    }))

    expect(screen.queryByRole("button", { name: "Needs you" })).toBeNull()
  })
})

describe("NativeAgentPanel questions", () => {

  it("answers a question the agent is waiting on", async () => {
    mockSnapshot({ status: "waiting_for_input", pendingQuestions: [{ requestId: "req-1", questions: [colorQuestion] }] })

    render(<NativeAgentPanel agentId="task-1" agentKind="claude-code" />)
    const card = await screen.findByRole("region", { name: "Question from Claude Code" })
    fireEvent.click(within(card).getByRole("radio", { name: /Blue/ }))
    fireEvent.click(within(card).getByRole("button", { name: "Send answer" }))

    expect(send).toHaveBeenCalledWith({ type: "agent_question", agent_id: "task-1", request_id: "req-1", answers: { q1: ["Blue"] } })
    expect(within(card).getByRole("status").textContent).toBe("Sending…")

    act(() => websocketHandler?.({
      type: "chimera_event",
      agent_id: "task-1",
      content: JSON.stringify({ eventId: "resolved-1", type: "question_resolved", requestId: "req-1", answers: { q1: ["Blue"] } }),
    }))

    expect(within(card).getByText("Answered")).not.toBeNull()
    expect(within(card).queryByRole("radio")).toBeNull()
  })

  it("opens a question asked while the panel is watching and can dismiss it", async () => {
    mockSnapshot({ status: "running" })

    render(<NativeAgentPanel agentId="task-1" />)
    await waitFor(() => expect(websocketHandler).toBeDefined())
    await screen.findByText("Session ready — send a message to start")
    act(() => websocketHandler?.({
      type: "chimera_event",
      agent_id: "task-1",
      content: JSON.stringify({ eventId: "asked-1", type: "question_request", requestId: "req-7", questions: [colorQuestion] }),
    }))

    // Named for whichever agent the panel shows: the card is not Claude's own.
    const card = screen.getByRole("region", { name: "Question from Chimera" })
    fireEvent.click(within(card).getByRole("button", { name: "Dismiss" }))

    expect(send).toHaveBeenCalledWith({ type: "agent_question", agent_id: "task-1", request_id: "req-7", dismissed: true })
  })

  it("reopens a question whose answer could not be delivered", async () => {
    mockSnapshot({ status: "waiting_for_input", pendingQuestions: [{ requestId: "req-1", questions: [colorQuestion] }] })

    render(<NativeAgentPanel agentId="task-1" agentKind="claude-code" />)
    const card = await screen.findByRole("region", { name: "Question from Claude Code" })
    fireEvent.click(within(card).getByRole("radio", { name: /Red/ }))
    fireEvent.click(within(card).getByRole("button", { name: "Send answer" }))
    act(() => websocketHandler?.({ type: "error", agent_id: "task-1", request_id: "req-1", error: "answer question failed: 404 Not Found" }))

    expect(within(card).getByRole("alert").textContent).toContain("404 Not Found")
    const retry = within(card).getByRole("button", { name: "Send answer" }) as HTMLButtonElement
    expect(retry.disabled).toBe(false)
    // What was chosen is still chosen.
    expect((within(card).getByRole("radio", { name: /Red/ }) as HTMLInputElement).checked).toBe(true)
  })
})
