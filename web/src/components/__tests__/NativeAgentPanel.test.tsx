import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { NativeAgentPanel } from "../NativeAgentPanel"

const send = vi.fn()
let websocketHandler: ((message: unknown) => void) | undefined
let websocketConnected = true

vi.mock("../../hooks/useWebSocket", () => ({
  useWebSocket: () => ({
    connected: websocketConnected,
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

describe("NativeAgentPanel", () => {
  beforeEach(() => {
    send.mockClear()
    websocketConnected = true
  })

  afterEach(() => {
    cleanup()
    vi.restoreAllMocks()
  })

  it("marks only the first snapshot as initial hydration", async () => {
    const snapshotRequests: string[] = []
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const path = String(input)
      if (!path.includes("/snapshot")) return new Response("[]", { status: 200 })
      snapshotRequests.push(path)
      return new Response(JSON.stringify({ id: "session-1", status: "idle", messages: [], toolCalls: [] }), { status: 200 })
    })

    const view = render(<NativeAgentPanel agentId="task-1" />)
    await within(view.container).findByText("Session ready — send a message to start")
    expect(snapshotRequests[0]).toBe("/api/agents/task-1/chimera/snapshot?initial=true")

    fireEvent.click(within(view.container).getByTitle("More actions"))
    fireEvent.click(within(view.container).getByRole("button", { name: "Refresh from snapshot" }))
    await waitFor(() => expect(snapshotRequests).toHaveLength(2))
    expect(snapshotRequests[1]).toBe("/api/agents/task-1/chimera/snapshot")
  })

  it("offers a manual downgrade action and surfaces failures", async () => {
    const onDowngradeChimera = vi.fn().mockRejectedValue(new Error("downgrade unavailable"))
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => String(input).includes("/snapshot")
      ? new Response(JSON.stringify({ id: "session-1", status: "idle", messages: [], toolCalls: [] }), { status: 200 })
      : new Response("[]", { status: 200 }))

    const view = render(<NativeAgentPanel agentId="task-1" onDowngradeChimera={onDowngradeChimera} />)
    await within(view.container).findByText("Session ready — send a message to start")
    fireEvent.click(within(view.container).getByTitle("More actions"))
    fireEvent.click(within(view.container).getByRole("button", { name: "Downgrade to tmux" }))

    expect(onDowngradeChimera).toHaveBeenCalledOnce()
    expect(await within(view.container).findByText("downgrade unavailable")).not.toBeNull()
  })

  it("renders and resolves a structured host permission request", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => String(input).includes("/snapshot")
      ? new Response(JSON.stringify({ id: "session-1", status: "waiting_for_permission", messages: [], toolCalls: [] }), { status: 200 })
      : new Response("[]", { status: 200 }))

    render(<NativeAgentPanel agentId="task-1" />)
    await screen.findByText("Session ready — send a message to start")
    act(() => websocketHandler?.({
      type: "chimera_event",
      agent_id: "task-1",
      content: JSON.stringify({
        eventId: "event-1",
        type: "permission_request",
        requestId: "req-1",
        tool: "bash",
        target: "host",
        command: "rm -rf /tmp/build-cache",
        reason: "Runs outside the sandbox",
        policyAnalysis: {
          version: 1,
          outcome: "approval_required",
          stages: [],
          findings: [{
            kind: "path",
            stageId: "paths",
            operation: "delete",
            path: "/tmp/build-cache",
            requiredGrant: "write",
            message: "Deletes outside configured roots",
          }],
        },
      }),
    }))

    const card = screen.getByRole("region", { name: "Permission request for bash" })
    expect(within(card).getByText("bash · host")).not.toBeNull()
    expect(within(card).getByText("rm -rf /tmp/build-cache")).not.toBeNull()
    expect(within(card).getByText("Runs outside the sandbox")).not.toBeNull()
    expect(within(card).getByText("Deletes outside configured roots")).not.toBeNull()

    fireEvent.click(within(card).getByRole("button", { name: "Allow" }))
    expect(send).toHaveBeenCalledWith({
      type: "agent_permission",
      agent_id: "task-1",
      request_id: "req-1",
      decision: "allow",
    })
    expect((within(card).getByRole("button", { name: "Allow" }) as HTMLButtonElement).disabled).toBe(true)

    act(() => websocketHandler?.({
      type: "chimera_event",
      agent_id: "task-1",
      content: JSON.stringify({ eventId: "event-2", type: "permission_resolved", requestId: "req-1", decision: "allow", remembered: false }),
    }))
    expect(within(card).getByText("Allowed")).not.toBeNull()
    expect(within(card).queryByRole("button", { name: "Allow" })).toBeNull()
  })

  it("requires and atomically sends trimmed denial feedback", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => String(input).includes("/snapshot")
      ? new Response(JSON.stringify({ id: "session-1", status: "idle", messages: [], toolCalls: [] }), { status: 200 })
      : new Response("[]", { status: 200 }))

    render(<NativeAgentPanel agentId="task-1" />)
    await screen.findByText("Session ready — send a message to start")
    act(() => websocketHandler?.({
      type: "chimera_event",
      agent_id: "task-1",
      content: JSON.stringify({ eventId: "event-1", type: "permission_request", requestId: "req-1", tool: "bash", target: "host", command: "dangerous command" }),
    }))

    const card = screen.getByRole("region", { name: "Permission request for bash" })
    fireEvent.click(within(card).getByRole("button", { name: "Deny with feedback" }))
    const submit = within(card).getByRole("button", { name: "Send denial" })
    expect((submit as HTMLButtonElement).disabled).toBe(true)
    fireEvent.change(within(card).getByLabelText("Feedback for Chimera"), { target: { value: "  Use the sandbox instead  " } })
    fireEvent.click(submit)

    expect(send).toHaveBeenCalledWith({
      type: "agent_permission",
      agent_id: "task-1",
      request_id: "req-1",
      decision: "deny",
      feedback: "Use the sandbox instead",
    })
  })

  it("shows a correlated permission failure and permits retry", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => String(input).includes("/snapshot")
      ? new Response(JSON.stringify({ id: "session-1", status: "idle", messages: [], toolCalls: [] }), { status: 200 })
      : new Response("[]", { status: 200 }))

    render(<NativeAgentPanel agentId="task-1" />)
    await screen.findByText("Session ready — send a message to start")
    act(() => websocketHandler?.({
      type: "chimera_event", agent_id: "task-1",
      content: JSON.stringify({ eventId: "event-1", type: "permission_request", requestId: "req-1", tool: "bash", target: "host", command: "host command" }),
    }))
    const card = screen.getByRole("region", { name: "Permission request for bash" })
    fireEvent.click(within(card).getByRole("button", { name: "Deny" }))

    act(() => websocketHandler?.({ type: "error", agent_id: "task-1", request_id: "other", error: "unrelated" }))
    expect(within(card).queryByText("unrelated")).toBeNull()
    act(() => websocketHandler?.({ type: "error", agent_id: "task-1", request_id: "req-1", error: "resolve permission failed: 409 Conflict" }))

    expect(within(card).getByRole("alert").textContent).toContain("409 Conflict")
    expect((within(card).getByRole("button", { name: "Deny" }) as HTMLButtonElement).disabled).toBe(false)
    fireEvent.click(within(card).getByRole("button", { name: "Deny" }))
    expect(send.mock.calls.filter(([message]) => message.type === "agent_permission")).toHaveLength(2)
  })

  it("executes a bang macro instead of sending it as a literal message", async () => {
    const macro = "!legato task show $LEGATO_TASK_ID --format full"
    const requests: string[] = []
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const url = String(input)
      requests.push(url)
      if (url.includes("/snapshot")) {
        return new Response(JSON.stringify({ id: "session-1", status: "idle", messages: [], toolCalls: [] }), { status: 200 })
      }
      if (url.endsWith("/api/macros")) {
        return new Response(JSON.stringify({ macros: [{ name: "Show task", keys: macro }] }), { status: 200 })
      }
      if (url.includes("/chimera/bang")) {
        return new Response(JSON.stringify({ stdout: "task", exit_code: 0 }), { status: 200 })
      }
      return new Response("[]", { status: 200 })
    })

    render(<NativeAgentPanel agentId="task-1" />)
    await screen.findByText("Session ready — send a message to start")
    fireEvent.click(screen.getByRole("button", { name: /macros/i }))
    fireEvent.click(await screen.findByRole("button", { name: /show task/i }))

    await waitFor(() => expect(requests.some((url) => url.includes("/api/agents/task-1/chimera/bang"))).toBe(true))
    expect(send).not.toHaveBeenCalledWith(expect.objectContaining({ type: "agent_message", content: macro }))
  })

  it("opens macros with M and runs the numbered bang macro through native dispatch", async () => {
    const macros = [
      { name: "First macro", keys: "first" },
      { name: "Second macro", keys: "second" },
      { name: "Third macro", keys: "!echo third" },
      { name: "Fourth macro", keys: "fourth" },
    ]
    const requests: string[] = []
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const url = String(input)
      requests.push(url)
      if (url.includes("/snapshot")) {
        return new Response(JSON.stringify({ id: "session-1", status: "idle", messages: [], toolCalls: [] }), { status: 200 })
      }
      if (url.endsWith("/api/macros")) {
        return new Response(JSON.stringify({ macros }), { status: 200 })
      }
      if (url.includes("/chimera/bang")) {
        return new Response(JSON.stringify({ stdout: "third", exit_code: 0 }), { status: 200 })
      }
      return new Response("[]", { status: 200 })
    })

    render(<NativeAgentPanel agentId="task-1" />)
    const composer = await screen.findByPlaceholderText("Send a message...")
    await waitFor(() => expect(requests).toContain("/api/macros"))
    composer.focus()
    composer.blur()

    fireEvent.keyDown(window, { key: "M" })
    const thirdMacro = screen.getByRole("button", { name: /3.*third macro/i })
    expect(thirdMacro.getAttribute("aria-keyshortcuts")).toBe("3")

    fireEvent.keyDown(window, { key: "3" })

    await waitFor(() => expect(requests.some((url) => url.includes("/api/agents/task-1/chimera/bang"))).toBe(true))
    expect(send).not.toHaveBeenCalledWith(expect.objectContaining({ type: "agent_message", content: "!echo third" }))
    expect(screen.queryByRole("button", { name: /third macro/i })).toBeNull()
  })

  it("guards native macro hotkeys and closes the menu with Escape", async () => {
    const macros = [{ name: "First macro", keys: "first" }]
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const url = String(input)
      if (url.includes("/snapshot")) {
        return new Response(JSON.stringify({ id: "session-1", status: "idle", messages: [], toolCalls: [] }), { status: 200 })
      }
      if (url.endsWith("/api/macros")) {
        return new Response(JSON.stringify({ macros }), { status: 200 })
      }
      return new Response("[]", { status: 200 })
    })

    render(<NativeAgentPanel agentId="task-1" />)
    const composer = await screen.findByPlaceholderText("Send a message...")
    await waitFor(() => expect(screen.getByTitle("Macros (M)").getAttribute("aria-keyshortcuts")).toBe("m"))

    fireEvent.keyDown(composer, { key: "m" })
    expect(screen.queryByRole("button", { name: /first macro/i })).toBeNull()
    composer.blur()

    const contentEditable = document.createElement("div")
    contentEditable.setAttribute("contenteditable", "true")
    const editableTargets = [
      document.createElement("input"),
      document.createElement("select"),
      contentEditable,
    ]
    for (const target of editableTargets) {
      document.body.appendChild(target)
      fireEvent.keyDown(target, { key: "m" })
      target.remove()
    }
    for (const modifier of ["ctrlKey", "altKey", "metaKey", "shiftKey"] as const) {
      fireEvent.keyDown(window, { key: "m", [modifier]: true })
    }
    fireEvent.keyDown(window, { key: "m", repeat: true })
    const handled = new KeyboardEvent("keydown", { key: "m", cancelable: true })
    handled.preventDefault()
    window.dispatchEvent(handled)
    expect(screen.queryByRole("button", { name: /first macro/i })).toBeNull()

    fireEvent.keyDown(window, { key: "m" })
    expect(screen.getByRole("button", { name: /first macro/i })).not.toBeNull()
    fireEvent.keyDown(window, { key: "9" })
    expect(send).not.toHaveBeenCalledWith(expect.objectContaining({ type: "agent_message", content: "first" }))
    expect(screen.getByRole("button", { name: /first macro/i })).not.toBeNull()
    fireEvent.keyDown(window, { key: "Escape" })
    expect(screen.queryByRole("button", { name: /first macro/i })).toBeNull()
  })

  it("keeps failed macro loading empty and digit selection inert", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const url = String(input)
      if (url.includes("/snapshot")) {
        return new Response(JSON.stringify({ id: "session-1", status: "idle", messages: [], toolCalls: [] }), { status: 200 })
      }
      if (url.endsWith("/api/macros")) throw new Error("unavailable")
      return new Response("[]", { status: 200 })
    })

    render(<NativeAgentPanel agentId="task-1" />)
    const composer = await screen.findByPlaceholderText("Send a message...")
    composer.blur()
    fireEvent.keyDown(window, { key: "m" })
    expect(screen.getByText("(no macros configured)")).not.toBeNull()
    fireEvent.keyDown(window, { key: "1" })
    expect(send).not.toHaveBeenCalledWith(expect.objectContaining({ type: "agent_message" }))
  })

  it("keeps queue and interrupt controls in one bounded equal-width composer rail", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => String(input).includes("/snapshot")
      ? new Response(JSON.stringify({ id: "session-1", status: "running", messages: [], toolCalls: [] }), { status: 200 })
      : new Response("[]", { status: 200 }))

    const view = render(<NativeAgentPanel agentId="task-1" />)
    const composer = await within(view.container).findByTestId("agent-composer")
    const rail = within(composer).getByTestId("composer-actions")
    fireEvent.change(within(composer).getByPlaceholderText("Queue a message..."), { target: { value: "Next" } })
    const queue = within(rail).getByRole("button", { name: "Queue" })
    const interrupt = within(rail).getByRole("button", { name: "Interrupt" })

    expect(composer.className).toContain("items-stretch")
    expect(within(composer).getByPlaceholderText("Queue a message...").className).toContain("min-h-24")
    expect(rail.className).toContain("w-24")
    expect(rail.className).toContain("grid-rows-2")
    expect(queue.className).toContain("w-full")
    expect(interrupt.className).toContain("w-full")
  })

  it("expands edit diffs by default without expanding other tool calls", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => String(input).includes("/snapshot")
      ? new Response(JSON.stringify({
          id: "session-1",
          status: "idle",
          messages: [
            { role: "user", content: "system" },
            { role: "assistant", content: [{ type: "tool-call" }, { type: "tool-call" }] },
          ],
          toolCalls: [
            { callId: "edit-1", name: "edit", args: { path: "main.go", old_string: "old line", new_string: "new line" } },
            { callId: "bash-1", name: "bash", args: { command: "go test ./..." }, result: { stdout: "hidden output" } },
          ],
        }), { status: 200 })
      : new Response("[]", { status: 200 }))

    const view = render(<NativeAgentPanel agentId="task-1" />)

    expect(await within(view.container).findByText("- old line")).not.toBeNull()
    expect(within(view.container).getByText("+ new line")).not.toBeNull()
    expect(within(view.container).queryByText("hidden output")).toBeNull()
  })

  it("preserves indentation in edit diff lines", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => String(input).includes("/snapshot")
      ? new Response(JSON.stringify({
          id: "session-1",
          status: "idle",
          messages: [
            { role: "user", content: "system" },
            { role: "assistant", content: [{ type: "tool-call" }] },
          ],
          toolCalls: [
            { callId: "edit-1", name: "edit", args: { path: "main.go", old_string: "\tif x {\n\t\treturn 1\n\t}", new_string: "\tif x {\n\t\treturn 2\n\t}" } },
          ],
        }), { status: 200 })
      : new Response("[]", { status: 200 }))

    const view = render(<NativeAgentPanel agentId="task-1" />)
    await within(view.container).findByText("edit")
    const diffContainer = view.container.querySelector(".font-mono.whitespace-pre")
    expect(diffContainer).not.toBeNull()
    // whitespace-pre preserves leading tabs — the rendered text should contain them
    expect(diffContainer!.textContent).toContain("\tif x {")
    expect(diffContainer!.textContent).toContain("\t\treturn 2")
  })

  it("scrolls to the latest message after hydration and after sending", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => String(input).includes("/snapshot")
      ? new Response(JSON.stringify({
          id: "session-1",
          status: "idle",
          messages: [
            { role: "user", content: "system" },
            { role: "assistant", content: [{ type: "text", text: "Latest reply" }] },
          ],
          toolCalls: [],
        }), { status: 200 })
      : new Response("[]", { status: 200 }))

    let scrollTop = 0
    vi.spyOn(HTMLElement.prototype, "scrollHeight", "get").mockReturnValue(1200)
    vi.spyOn(HTMLElement.prototype, "clientHeight", "get").mockReturnValue(300)
    vi.spyOn(HTMLElement.prototype, "scrollTop", "get").mockImplementation(() => scrollTop)
    vi.spyOn(HTMLElement.prototype, "scrollTop", "set").mockImplementation((value) => { scrollTop = value })

    const view = render(<NativeAgentPanel agentId="task-1" />)
    await within(view.container).findByText("Latest reply")
    await waitFor(() => expect(scrollTop).toBe(1200))
    scrollTop = 100
    const composer = within(view.container).getByPlaceholderText("Send a message...")
    fireEvent.change(composer, { target: { value: "Next message" } })
    fireEvent.click(within(view.container).getByRole("button", { name: "Send" }))

    await waitFor(() => expect(scrollTop).toBe(1200))
  })

  it("auto-scrolls when near the bottom on new content, but not when scrolled up", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => String(input).includes("/snapshot")
      ? new Response(JSON.stringify({
          id: "session-1",
          status: "running",
          messages: [
            { role: "user", content: "system" },
            { role: "assistant", content: [{ type: "text", text: "First reply" }] },
          ],
          toolCalls: [],
        }), { status: 200 })
      : new Response("[]", { status: 200 }))

    let scrollTop = 0
    vi.spyOn(HTMLElement.prototype, "scrollHeight", "get").mockReturnValue(1200)
    vi.spyOn(HTMLElement.prototype, "clientHeight", "get").mockReturnValue(300)
    vi.spyOn(HTMLElement.prototype, "scrollTop", "get").mockImplementation(() => scrollTop)
    vi.spyOn(HTMLElement.prototype, "scrollTop", "set").mockImplementation((value) => { scrollTop = value })

    const view = render(<NativeAgentPanel agentId="task-1" />)
    await within(view.container).findByText("First reply")
    await waitFor(() => expect(scrollTop).toBe(1200))

    // Simulate user near the bottom (within threshold)
    scrollTop = 950 // 1200 - 950 - 300 = -50 < 60 → at bottom
    act(() => websocketHandler?.({
      type: "chimera_event",
      agent_id: "task-1",
      content: JSON.stringify({ eventId: "ev1", type: "assistant_text", text: "streaming content" }),
    }))
    await waitFor(() => expect(scrollTop).toBe(1200))

    // Simulate user deliberately scrolled up beyond threshold
    scrollTop = 100 // 1200 - 100 - 300 = 800 > 60 → not at bottom
    act(() => websocketHandler?.({
      type: "chimera_event",
      agent_id: "task-1",
      content: JSON.stringify({ eventId: "ev2", type: "assistant_text", text: "more content" }),
    }))
    // Should not auto-scroll
    expect(scrollTop).toBe(100)
  })

  it("shows the activated skill on its read tool card", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => String(input).includes("/snapshot")
      ? new Response(JSON.stringify({ id: "session-1", status: "running", messages: [], toolCalls: [] }), { status: 200 })
      : new Response("[]", { status: 200 }))

    const view = render(<NativeAgentPanel agentId="task-1" />)
    await waitFor(() => expect(websocketHandler).toBeDefined())
    act(() => {
      websocketHandler?.({
        type: "chimera_event",
        agent_id: "task-1",
        content: JSON.stringify({
          eventId: "read-1",
          type: "tool_call_start",
          callId: "call-1",
          name: "read",
          args: { path: "/skills/coworker-walkthrough/SKILL.md" },
        }),
      })
      websocketHandler?.({
        type: "chimera_event",
        agent_id: "task-1",
        content: JSON.stringify({
          eventId: "skill-1",
          type: "skill_activated",
          skillName: "coworker-walkthrough",
          source: "user",
        }),
      })
    })

    expect(await within(view.container).findByText("📚 skill: coworker-walkthrough")).not.toBeNull()
  })

  it("intercepts /model and calls the model API instead of sending a message", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const url = String(input)
      if (url.includes("/snapshot")) return new Response(JSON.stringify({ id: "session-1", status: "idle", messages: [], toolCalls: [] }), { status: 200 })
      if (url.endsWith("/chimera/model")) return new Response(null, { status: 204 })
      return new Response("[]", { status: 200 })
    })

    const view = render(<NativeAgentPanel agentId="task-1" />)
    await waitFor(() => expect(websocketHandler).toBeDefined())
    const composer = within(view.container).getByPlaceholderText("Send a message...")
    fireEvent.change(composer, { target: { value: "/model manticore/glm" } })
    fireEvent.click(within(view.container).getByRole("button", { name: "Send" }))

    await waitFor(() => {
      expect(fetchSpy).toHaveBeenCalledWith(expect.stringContaining("/chimera/model"), expect.objectContaining({ method: "POST" }))
    })
    expect(send).not.toHaveBeenCalledWith(expect.objectContaining({ type: "agent_message" }))
  })

  it("intercepts /mode and calls the modes API instead of sending a message", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const url = String(input)
      if (url.includes("/snapshot")) return new Response(JSON.stringify({ id: "session-1", status: "idle", messages: [], toolCalls: [] }), { status: 200 })
      if (url.endsWith("/chimera/modes")) return new Response(null, { status: 204 })
      return new Response("[]", { status: 200 })
    })

    const view = render(<NativeAgentPanel agentId="task-1" />)
    await waitFor(() => expect(websocketHandler).toBeDefined())
    const composer = within(view.container).getByPlaceholderText("Send a message...")
    fireEvent.change(composer, { target: { value: "/mode plan" } })
    fireEvent.click(within(view.container).getByRole("button", { name: "Send" }))

    await waitFor(() => {
      expect(fetchSpy).toHaveBeenCalledWith(expect.stringContaining("/chimera/modes"), expect.objectContaining({ method: "POST" }))
    })
    expect(send).not.toHaveBeenCalledWith(expect.objectContaining({ type: "agent_message" }))
  })

  it("intercepts a leading bang command and executes it via the bang endpoint", async () => {
    let snapshotMessages: unknown[] = []
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const url = String(input)
      if (url.includes("/snapshot")) {
        return new Response(JSON.stringify({ id: "session-1", status: "idle", messages: snapshotMessages, toolCalls: [] }), { status: 200 })
      }
      if (url.includes("/chimera/bang")) {
        snapshotMessages = [
          { role: "user", content: "system" },
          { role: "assistant", content: [{ type: "text", text: "!echo hello (exit 0)\nhello" }] },
        ]
        return new Response(JSON.stringify({ stdout: "hello\n", stderr: "", exit_code: 0, timed_out: false }), { status: 200 })
      }
      return new Response("[]", { status: 200 })
    })

    const view = render(<NativeAgentPanel agentId="task-1" />)
    await waitFor(() => expect(websocketHandler).toBeDefined())
    const composer = within(view.container).getByPlaceholderText("Send a message...")
    fireEvent.change(composer, { target: { value: "!echo hello" } })
    fireEvent.click(within(view.container).getByRole("button", { name: "Run" }))

    await waitFor(() => {
      expect(send).not.toHaveBeenCalledWith(expect.objectContaining({ type: "agent_message" }))
    })
    // The bang card shows the command and its stdout output
    expect(await within(view.container).findByText("echo hello")).not.toBeNull()
    expect(within(view.container).getByText("exit 0")).not.toBeNull()
    expect(within(view.container).getByText("hello")).not.toBeNull()
  })

  it("shows a bash-mode indicator in the composer when the draft starts with !", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => String(input).includes("/snapshot")
      ? new Response(JSON.stringify({ id: "session-1", status: "idle", messages: [], toolCalls: [] }), { status: 200 })
      : new Response("[]", { status: 200 }))

    const view = render(<NativeAgentPanel agentId="task-1" />)
    await waitFor(() => expect(websocketHandler).toBeDefined())
    const composer = within(view.container).getByPlaceholderText("Send a message...") as HTMLTextAreaElement

    // No indicator when draft is empty or normal text
    expect(within(view.container).queryByText("bash")).toBeNull()
    fireEvent.change(composer, { target: { value: "hello" } })
    expect(within(view.container).queryByText("bash")).toBeNull()

    // Indicator appears when draft starts with !
    fireEvent.change(composer, { target: { value: "!ls" } })
    expect(within(view.container).getByText("bash")).not.toBeNull()

    // Border changes to amber
    expect(composer.className).toContain("border-amber-500/60")

    // Indicator disappears when ! is removed
    fireEvent.change(composer, { target: { value: "hello" } })
    expect(within(view.container).queryByText("bash")).toBeNull()
  })

  it("queues a bang command while the agent is busy and runs it after run_finished", async () => {
    let snapshotStatus = "running"
    let snapshotMessages: unknown[] = []
    let bangExecuted = false
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const url = String(input)
      if (url.includes("/snapshot")) {
        return new Response(JSON.stringify({ id: "session-1", status: snapshotStatus, messages: snapshotMessages, toolCalls: [] }), { status: 200 })
      }
      if (url.includes("/chimera/bang")) {
        bangExecuted = true
        snapshotStatus = "idle"
        snapshotMessages = [
          { role: "user", content: "system" },
          { role: "assistant", content: [{ type: "text", text: "!echo done (exit 0)\ndone" }] },
        ]
        return new Response(JSON.stringify({ stdout: "done\n", stderr: "", exit_code: 0, timed_out: false }), { status: 200 })
      }
      return new Response("[]", { status: 200 })
    })

    const view = render(<NativeAgentPanel agentId="task-1" />)
    await waitFor(() => expect(websocketHandler).toBeDefined())
    const composer = within(view.container).getByPlaceholderText("Queue a message...")
    fireEvent.change(composer, { target: { value: "!echo done" } })
    fireEvent.click(within(view.container).getByRole("button", { name: "Run" }))

    await waitFor(() => expect(within(view.container).getByText("Queued: !echo done")).not.toBeNull())
    expect(bangExecuted).toBe(false)

    act(() => websocketHandler?.({
      type: "chimera_event",
      agent_id: "task-1",
      content: JSON.stringify({ eventId: "finish-bang", type: "run_finished" }),
    }))

    await waitFor(() => expect(bangExecuted).toBe(true))
    // The bang card should be visible with the command and output
    expect(await within(view.container).findByText("echo done")).not.toBeNull()
    expect(within(view.container).getByText("exit 0")).not.toBeNull()
  })

  it("anchors the scroll-to-bottom action inside the conversation viewport", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => String(input).includes("/snapshot")
      ? new Response(JSON.stringify({
          id: "session-1",
          status: "idle",
          messages: [
            { role: "user", content: "system" },
            { role: "assistant", content: [{ type: "text", text: "A long conversation" }] },
          ],
          toolCalls: [],
        }), { status: 200 })
      : new Response("[]", { status: 200 }))

    const view = render(<NativeAgentPanel agentId="task-1" />)
    await within(view.container).findByText("A long conversation")
    const viewport = within(view.container).getByRole("region", { name: "Conversation" })
    const scrollArea = viewport.querySelector("[data-conversation-scroll]") as HTMLDivElement
    Object.defineProperties(scrollArea, {
      scrollHeight: { value: 1000, configurable: true },
      clientHeight: { value: 300, configurable: true },
      scrollTop: { value: 100, writable: true, configurable: true },
    })
    fireEvent.scroll(scrollArea)

    expect(within(viewport).getByTitle("Scroll to bottom")).not.toBeNull()
  })

  it("queues a message through Chimera while the agent is working", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => String(input).includes("/snapshot")
      ? new Response(JSON.stringify({ id: "session-1", status: "running", messages: [], toolCalls: [] }), { status: 200 })
      : new Response("[]", { status: 200 }))

    const view = render(<NativeAgentPanel agentId="task-1" />)
    const composer = await within(view.container).findByPlaceholderText("Queue a message...")
    expect((composer as HTMLTextAreaElement).disabled).toBe(false)
    fireEvent.change(composer, { target: { value: "Check the tests next" } })
    fireEvent.click(within(view.container).getByRole("button", { name: "Queue" }))

    expect(send).toHaveBeenCalledWith({
      type: "agent_message",
      agent_id: "task-1",
      content: "Check the tests next",
      queue: true,
    })
  })

  it("shows current context usage instead of cumulative session tokens", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const url = String(input)
      if (url.includes("/snapshot")) {
        return new Response(JSON.stringify({
          id: "session-1",
          cwd: "/repo",
          mode: "build",
          status: "idle",
          messages: [],
          toolCalls: [],
          usage: {
            inputTokens: 3_700_000,
            outputTokens: 100_000,
            totalTokens: 3_800_000,
            stepCount: 30,
            lastStep: { inputTokens: 128_000, outputTokens: 2_000, totalTokens: 130_000 },
          },
          contextWindow: 256_000,
          usedContextTokens: 128_000,
        }), { status: 200 })
      }
      return new Response("[]", { status: 200 })
    })

    render(<NativeAgentPanel agentId="task-1" />)

    const initial = await screen.findByText("128k / 256k (50%)")
    expect(initial).not.toBeNull()
    expect(screen.queryByText(/3,800,000 tokens/)).toBeNull()
    // 50% should be amber (Steam Deck pressure: <50% green, 50-75% amber, >=75% red)
    expect(initial.closest("div")!.querySelector(".bg-amber-500")).not.toBeNull()

    act(() => websocketHandler?.({
      type: "chimera_event",
      agent_id: "task-1",
      content: JSON.stringify({
        eventId: "event-1",
        sessionId: "session-1",
        ts: 1,
        type: "usage_updated",
        contextWindow: 256_000,
        usedContextTokens: 192_000,
      }),
    }))

    const updated = screen.getByText("192k / 256k (75%)")
    expect(updated).not.toBeNull()
    // 75% should be red
    expect(updated.closest("div")!.querySelector(".bg-red-500")).not.toBeNull()
  })

  it("does not lose a live reply when the initial snapshot resolves late", async () => {
    let releaseSnapshot!: (response: Response) => void
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const url = String(input)
      if (url.includes("/snapshot")) {
        return new Promise<Response>((resolve) => { releaseSnapshot = resolve })
      }
      return new Response("[]", { status: 200 })
    })

    render(<NativeAgentPanel agentId="task-1" />)
    await waitFor(() => expect(websocketHandler).toBeDefined())
    act(() => websocketHandler?.({
      type: "chimera_event",
      agent_id: "task-1",
      content: JSON.stringify({
        eventId: "event-live",
        sessionId: "session-1",
        ts: 1,
        type: "assistant_text_done",
        id: "reply-1",
        text: "Reply that arrived during hydration",
      }),
    }))
    releaseSnapshot(new Response(JSON.stringify({
      id: "session-1",
      mode: "build",
      status: "running",
      messages: [{ role: "user", content: "system prompt" }],
      toolCalls: [],
    }), { status: 200 }))

    expect(await screen.findByText("Reply that arrived during hydration")).not.toBeNull()
    expect(screen.getAllByText("Reply that arrived during hydration")).toHaveLength(1)
  })

  it("resubscribes with the last accepted cursor after reconnect", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => String(input).includes("/snapshot")
      ? new Response(JSON.stringify({ id: "session-1", status: "idle", messages: [], toolCalls: [] }), { status: 200 })
      : new Response("[]", { status: 200 }))

    const view = render(<NativeAgentPanel agentId="task-1" />)
    await waitFor(() => expect(websocketHandler).toBeDefined())
    act(() => websocketHandler?.({
      type: "chimera_event",
      agent_id: "task-1",
      content: JSON.stringify({ eventId: "cursor-1", type: "assistant_text_done", id: "reply", text: "Received" }),
    }))

    websocketConnected = false
    view.rerender(<NativeAgentPanel agentId="task-1" />)
    websocketConnected = true
    view.rerender(<NativeAgentPanel agentId="task-1" />)

    expect(send).toHaveBeenLastCalledWith({ type: "subscribe_agent", agent_id: "task-1", cursor: "cursor-1" })
  })

  it("repairs from snapshot when the server cannot replay its cursor", async () => {
    let snapshotText = "Before repair"
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => String(input).includes("/snapshot")
      ? new Response(JSON.stringify({
          id: "session-1",
          status: "idle",
          messages: [
            { role: "user", content: "system" },
            { role: "assistant", content: [{ type: "text", text: snapshotText }] },
          ],
          toolCalls: [],
        }), { status: 200 })
      : new Response("[]", { status: 200 }))

    render(<NativeAgentPanel agentId="task-1" />)
    expect(await screen.findByText("Before repair")).not.toBeNull()
    snapshotText = "After repair"
    act(() => websocketHandler?.({ type: "chimera_sync_required", agent_id: "task-1" }))

    expect(await screen.findByText("After repair")).not.toBeNull()
  })

  it("keeps the feed mounted and preserves history position during post-run repair", async () => {
    let snapshotCalls = 0
    let releaseRepair!: (response: Response) => void
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      if (!String(input).includes("/snapshot")) return new Response("[]", { status: 200 })
      snapshotCalls++
      if (snapshotCalls === 1) {
        return new Response(JSON.stringify({
          id: "session-1",
          status: "running",
          messages: [
            { role: "user", content: "system" },
            { role: "assistant", content: [{ type: "text", text: "Existing history" }] },
          ],
          toolCalls: [],
        }), { status: 200 })
      }
      return new Promise<Response>((resolve) => { releaseRepair = resolve })
    })

    const view = render(<NativeAgentPanel agentId="task-1" />)
    await within(view.container).findByText("Existing history")
    act(() => websocketHandler?.({
      type: "chimera_event",
      agent_id: "task-1",
      content: JSON.stringify({ eventId: "finish-mounted", type: "run_finished" }),
    }))
    await waitFor(() => expect(snapshotCalls).toBe(2))

    // While repair is in flight, the feed stays mounted (no loading placeholder)
    expect(within(view.container).queryByText("Loading Chimera session...")).toBeNull()
    expect(within(view.container).getByText("Existing history")).not.toBeNull()
    expect(within(view.container).getByText("Syncing…")).not.toBeNull()

    releaseRepair(new Response(JSON.stringify({
      id: "session-1",
      status: "idle",
      messages: [
        { role: "user", content: "system" },
        { role: "assistant", content: [{ type: "text", text: "Repaired history" }] },
      ],
      toolCalls: [],
    }), { status: 200 }))
    expect(await within(view.container).findByText("Repaired history", {}, { timeout: 3000 })).not.toBeNull()
  })

  it("repairs from the authoritative snapshot after a run finishes", async () => {
    let snapshot: { id: string; status: string; messages: unknown[]; toolCalls: unknown[] } = {
      id: "session-1", status: "running", messages: [], toolCalls: [],
    }
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => String(input).includes("/snapshot")
      ? new Response(JSON.stringify(snapshot), { status: 200 })
      : new Response("[]", { status: 200 }))

    render(<NativeAgentPanel agentId="task-1" />)
    await waitFor(() => expect(websocketHandler).toBeDefined())
    snapshot = {
      id: "session-1",
      status: "idle",
      messages: [
        { role: "user", content: "system" },
        { role: "assistant", content: [{ type: "text", text: "Authoritative final reply" }] },
      ],
      toolCalls: [],
    }
    act(() => websocketHandler?.({
      type: "chimera_event",
      agent_id: "task-1",
      content: JSON.stringify({ eventId: "finish-1", type: "run_finished" }),
    }))

    expect(await screen.findByText("Authoritative final reply", {}, { timeout: 1000 })).not.toBeNull()
  })

  it("refreshes from snapshot from the overflow menu and retains content on failure", async () => {
    let refreshFails = false
    let snapshotText = "Initial reply"
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      if (!String(input).includes("/snapshot")) return new Response("[]", { status: 200 })
      if (refreshFails) return new Response("failed", { status: 502 })
      return new Response(JSON.stringify({
        id: "session-1",
        status: "idle",
        messages: [
          { role: "user", content: "system" },
          { role: "assistant", content: [{ type: "text", text: snapshotText }] },
        ],
        toolCalls: [],
      }), { status: 200 })
    })

    const view = render(<NativeAgentPanel agentId="task-1" />)
    expect(await within(view.container).findByText("Initial reply")).not.toBeNull()
    snapshotText = "Refreshed reply"
    fireEvent.click(within(view.container).getByTitle("More actions"))
    fireEvent.click(within(view.container).getByRole("button", { name: "Refresh from snapshot" }))
    expect(await within(view.container).findByText("Refreshed reply")).not.toBeNull()

    refreshFails = true
    fireEvent.click(within(view.container).getByTitle("More actions"))
    fireEvent.click(within(view.container).getByRole("button", { name: "Refresh from snapshot" }))
    act(() => websocketHandler?.({
      type: "chimera_event",
      agent_id: "task-1",
      content: JSON.stringify({ eventId: "during-failure", type: "assistant_text_done", id: "live", text: "Live during failed refresh" }),
    }))
    expect(await within(view.container).findByText("snapshot: 502")).not.toBeNull()
    expect(within(view.container).getByText("Refreshed reply")).not.toBeNull()
    expect(within(view.container).getByText("Live during failed refresh")).not.toBeNull()
  })

  it("captures replayed subagents immediately when loading during an active run", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => String(input).includes("/snapshot")
      ? new Response(JSON.stringify({ id: "session-1", status: "running", messages: [], toolCalls: [] }), { status: 200 })
      : new Response("[]", { status: 200 }))
    send.mockImplementation((message) => {
      if (message.type !== "subscribe_agent") return
      websocketHandler?.({
        type: "chimera_event",
        agent_id: "task-1",
        content: JSON.stringify({
          eventId: "replayed-call",
          type: "tool_call_start",
          callId: "call-active",
          name: "spawn_agent",
          args: { purpose: "Already running", prompt: "Inspect the active work" },
        }),
      })
      websocketHandler?.({
        type: "chimera_event",
        agent_id: "task-1",
        content: JSON.stringify({
          eventId: "replayed-spawn",
          type: "subagent_spawned",
          subagentId: "sub-active",
          parentCallId: "call-active",
          purpose: "Already running",
        }),
      })
    })

    const view = render(<NativeAgentPanel agentId="task-1" />)

    const card = await within(view.container).findByTestId("subagent-card-call-active")
    expect(within(card).getByText("Already running")).not.toBeNull()
    expect(within(card).getByText("running")).not.toBeNull()
  })

  it("labels completed subagents without captured events accurately", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => String(input).includes("/snapshot")
      ? new Response(JSON.stringify({
          id: "session-1",
          status: "idle",
          messages: [
            { role: "user", content: "system" },
            { role: "assistant", content: [{ type: "tool-call" }] },
          ],
          toolCalls: [{
            callId: "finished-call",
            name: "spawn_agent",
            args: { purpose: "Completed review", prompt: "Review it" },
            result: { reason: "stop", steps: 2, tool_calls_count: 1 },
          }],
        }), { status: 200 })
      : new Response("[]", { status: 200 }))

    const view = render(<NativeAgentPanel agentId="task-1" />)

    const card = await within(view.container).findByTestId("subagent-card-finished-call")
    expect(within(card).getByText("finished")).not.toBeNull()
    expect(within(card).getByText("No live activity captured")).not.toBeNull()
    expect(within(card).queryByText("Waiting for activity…")).toBeNull()
  })

  it("renders live subagent metadata and activity inside its spawn card", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => String(input).includes("/snapshot")
      ? new Response(JSON.stringify({ id: "session-1", status: "running", messages: [], toolCalls: [] }), { status: 200 })
      : new Response("[]", { status: 200 }))

    const view = render(<NativeAgentPanel agentId="task-1" />)
    await waitFor(() => expect(screen.queryByText("Loading Chimera session...")).toBeNull())

    act(() => websocketHandler?.({
      type: "chimera_event",
      agent_id: "task-1",
      content: JSON.stringify({
        eventId: "spawn-call",
        type: "tool_call_start",
        callId: "call-1",
        name: "spawn_agent",
        args: {
          purpose: "Review correctness",
          prompt: "Inspect the current changes for regressions.",
          agent: "review-correctness",
          model: "manticore/gpt",
        },
      }),
    }))
    act(() => websocketHandler?.({
      type: "chimera_event",
      agent_id: "task-1",
      content: JSON.stringify({
        eventId: "spawned",
        type: "subagent_spawned",
        subagentId: "sub-1",
        parentCallId: "call-1",
        childSessionId: "child-1",
        purpose: "Review correctness",
      }),
    }))
    act(() => websocketHandler?.({
      type: "chimera_event",
      agent_id: "task-1",
      content: JSON.stringify({
        eventId: "child-read",
        type: "subagent_event",
        subagentId: "sub-1",
        event: {
          type: "tool_call_start",
          callId: "child-call-1",
          name: "read",
          args: { path: "internal/service/agent.go" },
          display: { summary: "internal/service/agent.go" },
        },
      }),
    }))

    const card = within(view.container).getByTestId("subagent-card-call-1")
    expect(within(card).getByText("Review correctness")).not.toBeNull()
    expect(within(card).getByText("review-correctness")).not.toBeNull()
    expect(within(card).getByText("manticore/gpt")).not.toBeNull()
    expect(within(card).getByText("Inspect the current changes for regressions.")).not.toBeNull()
    expect(within(card).getByText("read: internal/service/agent.go")).not.toBeNull()
    expect(within(card).getByText("running")).not.toBeNull()

    act(() => websocketHandler?.({
      type: "chimera_event",
      agent_id: "task-1",
      content: JSON.stringify({
        eventId: "finished",
        type: "subagent_finished",
        subagentId: "sub-1",
        parentCallId: "call-1",
        result: "No regressions found.",
        reason: "stop",
      }),
    }))
    act(() => websocketHandler?.({
      type: "chimera_event",
      agent_id: "task-1",
      content: JSON.stringify({
        eventId: "spawn-result",
        type: "tool_call_result",
        callId: "call-1",
        result: { reason: "stop", steps: 3, tool_calls_count: 1 },
      }),
    }))

    expect(within(card).getByText("finished")).not.toBeNull()
    expect(within(card).getByText("3 steps · 1 tool call")).not.toBeNull()
  })

  it("keeps parallel subagent activity in the matching spawn cards", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => String(input).includes("/snapshot")
      ? new Response(JSON.stringify({ id: "session-1", status: "running", messages: [], toolCalls: [] }), { status: 200 })
      : new Response("[]", { status: 200 }))

    const view = render(<NativeAgentPanel agentId="task-1" />)
    await waitFor(() => expect(screen.queryByText("Loading Chimera session...")).toBeNull())

    for (const [callId, purpose] of [["call-a", "Correctness"], ["call-b", "Tests and docs"]]) {
      act(() => websocketHandler?.({
        type: "chimera_event",
        agent_id: "task-1",
        content: JSON.stringify({ eventId: `call-${callId}`, type: "tool_call_start", callId, name: "spawn_agent", args: { purpose, prompt: purpose } }),
      }))
      act(() => websocketHandler?.({
        type: "chimera_event",
        agent_id: "task-1",
        content: JSON.stringify({ eventId: `spawn-${callId}`, type: "subagent_spawned", subagentId: `sub-${callId}`, parentCallId: callId, purpose }),
      }))
    }
    act(() => websocketHandler?.({
      type: "chimera_event",
      agent_id: "task-1",
      content: JSON.stringify({
        eventId: "activity-b",
        type: "subagent_event",
        subagentId: "sub-call-b",
        event: { type: "assistant_text_done", text: "Documentation needs an update" },
      }),
    }))

    const correctness = within(view.container).getByTestId("subagent-card-call-a")
    const testsDocs = within(view.container).getByTestId("subagent-card-call-b")
    expect(within(correctness).queryByText("Documentation needs an update")).toBeNull()
    expect(within(testsDocs).getByText("Documentation needs an update")).not.toBeNull()
  })

  it("renders a snapshot tool call only at the assistant message that made it", async () => {
    const snapshot = {
      id: "session-1",
      cwd: "/repo",
      mode: "build",
      status: "idle",
      messages: [
        { role: "user", content: "system prompt" },
        { role: "assistant", content: [{ type: "text", text: "First update" }] },
        { role: "assistant", content: [{ type: "text", text: "Second update" }] },
        {
          role: "assistant",
          content: [
            { type: "text", text: "Writing the file" },
            {
              type: "tool-call",
              toolCallId: "provider-call-1",
              toolName: "write",
              input: { path: "/tmp/result.txt", content: "done" },
            },
          ],
        },
        {
          role: "tool",
          content: [{
            type: "tool-result",
            toolCallId: "provider-call-1",
            toolName: "write",
            output: { type: "json", value: { bytes_written: 4, created: true } },
          }],
        },
        { role: "assistant", content: [{ type: "text", text: "Fourth update" }] },
        { role: "assistant", content: [{ type: "text", text: "Fifth update" }] },
      ],
      toolCalls: [{
        callId: "chimera-call-1",
        name: "write",
        args: { path: "/tmp/result.txt", content: "done" },
        result: { bytes_written: 4, created: true },
      }],
    }

    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const url = String(input)
      if (url.includes("/snapshot")) {
        return new Response(JSON.stringify(snapshot), { status: 200 })
      }
      return new Response("[]", { status: 200 })
    })

    render(<NativeAgentPanel agentId="task-1" />)

    await waitFor(() => expect(screen.queryByText("Loading Chimera session...")).toBeNull())

    expect(screen.getAllByText("write", { selector: "span" })).toHaveLength(1)
    expect(screen.getByText("result.txt")).not.toBeNull()
    for (const text of ["First update", "Second update", "Writing the file", "Fourth update", "Fifth update"]) {
      expect(screen.getByText(text)).not.toBeNull()
    }
  })

  it("renders a model dropdown with models from the models endpoint", async () => {
    const modelsData = [
      { ref: "manticore/glm", contextWindow: 256000, isDefault: true },
      { ref: "manticore/gpt", contextWindow: 256000, isDefault: false },
      { ref: "manticore/kimi", contextWindow: 256000, vision: true, isDefault: false },
    ]
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const url = String(input)
      if (url.includes("/snapshot")) return new Response(JSON.stringify({ id: "session-1", status: "idle", model: { providerId: "manticore", modelId: "glm" }, messages: [], toolCalls: [] }), { status: 200 })
      if (url.endsWith("/chimera/models")) return new Response(JSON.stringify(modelsData), { status: 200 })
      return new Response("[]", { status: 200 })
    })

    render(<NativeAgentPanel agentId="task-1" />)
    await waitFor(() => expect(screen.queryByText("Loading Chimera session...")).toBeNull())

    const modelButton = screen.getByTitle("Change model")
    expect(modelButton).not.toBeNull()
    expect(within(modelButton as HTMLElement).getByText("manticore/glm")).not.toBeNull()

    fireEvent.click(modelButton)
    expect(await screen.findByText("manticore/gpt")).not.toBeNull()
    expect(screen.getByText("manticore/kimi")).not.toBeNull()
    // The "default" badge appears on the default model entry
    const defaultBadges = screen.getAllByText("default")
    expect(defaultBadges.length).toBeGreaterThanOrEqual(1)
    expect(screen.getByText("👁")).not.toBeNull()
  })

  it("POSTs to the model endpoint when selecting a different model from the dropdown", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const url = String(input)
      if (url.includes("/snapshot")) return new Response(JSON.stringify({ id: "session-1", status: "idle", model: { providerId: "manticore", modelId: "glm" }, messages: [], toolCalls: [] }), { status: 200 })
      if (url.endsWith("/chimera/models")) return new Response(JSON.stringify([
        { ref: "manticore/glm", isDefault: true },
        { ref: "manticore/gpt", isDefault: false },
      ]), { status: 200 })
      if (url.endsWith("/chimera/model")) return new Response(null, { status: 204 })
      return new Response("[]", { status: 200 })
    })

    render(<NativeAgentPanel agentId="task-1" />)
    await waitFor(() => expect(screen.queryByText("Loading Chimera session...")).toBeNull())

    fireEvent.click(screen.getByTitle("Change model"))
    const dropdownItems = await screen.findAllByText("manticore/gpt")
    fireEvent.click(dropdownItems[0])

    await waitFor(() => {
      expect(fetchSpy).toHaveBeenCalledWith(expect.stringContaining("/chimera/model"), expect.objectContaining({ method: "POST" }))
    })
    expect(screen.getByTitle("Change model").textContent).toContain("manticore/gpt")
  })

  it("does not POST when clicking the already-active model", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const url = String(input)
      if (url.includes("/snapshot")) return new Response(JSON.stringify({ id: "session-1", status: "idle", model: { providerId: "manticore", modelId: "glm" }, messages: [], toolCalls: [] }), { status: 200 })
      if (url.endsWith("/chimera/models")) return new Response(JSON.stringify([
        { ref: "manticore/glm", isDefault: true },
        { ref: "manticore/gpt", isDefault: false },
      ]), { status: 200 })
      return new Response("[]", { status: 200 })
    })

    render(<NativeAgentPanel agentId="task-1" />)
    await waitFor(() => expect(screen.queryByText("Loading Chimera session...")).toBeNull())

    const modelCallsBefore = fetchSpy.mock.calls.filter((c) => String(c[0]).includes("/chimera/model"))
    fireEvent.click(screen.getByTitle("Change model"))
    // The dropdown item for the active model is the second match (first is the button label)
    const dropdownItems = await screen.findAllByText("manticore/glm")
    fireEvent.click(dropdownItems[dropdownItems.length - 1])
    const modelCallsAfter = fetchSpy.mock.calls.filter((c) => String(c[0]).includes("/chimera/model"))
    expect(modelCallsAfter.length).toBe(modelCallsBefore.length)
  })

  it("hides the model ref text and mode button below the sm breakpoint", async () => {
    const modelsData = [{ ref: "manticore/glm", isDefault: true }]
    const modesData = [{ name: "build", description: "Build mode", cycle: false }]
    const macros = [{ name: "Show task", keys: "!legato task show $LEGATO_TASK_ID" }]
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const url = String(input)
      if (url.includes("/snapshot")) return new Response(JSON.stringify({ id: "session-1", status: "idle", model: { providerId: "manticore", modelId: "glm" }, messages: [], toolCalls: [] }), { status: 200 })
      if (url.endsWith("/chimera/models")) return new Response(JSON.stringify(modelsData), { status: 200 })
      if (url.endsWith("/chimera/modes")) return new Response(JSON.stringify(modesData), { status: 200 })
      if (url.endsWith("/api/macros")) return new Response(JSON.stringify({ macros }), { status: 200 })
      return new Response("[]", { status: 200 })
    })

    const view = render(<NativeAgentPanel agentId="task-1" />)
    await waitFor(() => expect(screen.queryByText("Loading Chimera session...")).toBeNull())

    // Model ref span collapses below sm
    const modelButton = screen.getByTitle("Change model")
    const modelRefSpan = within(modelButton as HTMLElement).getByText("manticore/glm")
    expect(modelRefSpan.className).toContain("hidden")
    expect(modelRefSpan.className).toContain("sm:inline")

    // Mode selector wrapper is hidden below sm
    const modeButton = screen.getByTitle("Change mode")
    expect(modeButton.parentElement!.className).toContain("hidden")
    expect(modeButton.parentElement!.className).toContain("sm:block")

    // Macros stays visible (not collapsed)
    const macrosButton = screen.getByTitle("Macros (M)")
    expect(macrosButton.parentElement!.className).not.toContain("hidden")

    // Dot menu is always present
    expect(view.container.querySelector('[title="More actions"]')).not.toBeNull()
  })

  it("exposes mode options inside the overflow menu on mobile", async () => {
    const modelsData = [{ ref: "manticore/glm", isDefault: true }]
    const modesData = [
      { name: "build", description: "Build mode", cycle: false },
      { name: "plan", description: "Plan mode", cycle: false },
    ]
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const url = String(input)
      if (url.includes("/snapshot")) return new Response(JSON.stringify({ id: "session-1", status: "idle", model: { providerId: "manticore", modelId: "glm" }, mode: "build", messages: [], toolCalls: [] }), { status: 200 })
      if (url.endsWith("/chimera/models")) return new Response(JSON.stringify(modelsData), { status: 200 })
      if (url.endsWith("/chimera/modes")) return new Response(JSON.stringify(modesData), { status: 200 })
      return new Response("[]", { status: 200 })
    })

    const view = render(<NativeAgentPanel agentId="task-1" />)
    await waitFor(() => expect(screen.queryByText("Loading Chimera session...")).toBeNull())

    fireEvent.click(within(view.container).getByTitle("More actions"))

    // Mode options appear inline in the overflow menu
    const buildButtons = await within(view.container).findAllByRole("button", { name: /build/i })
    expect(buildButtons.length).toBeGreaterThanOrEqual(1)
    expect(within(view.container).getByText("plan")).not.toBeNull()
  })

  it("switches mode from the overflow menu entry on mobile", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const url = String(input)
      if (url.includes("/snapshot")) return new Response(JSON.stringify({ id: "session-1", status: "idle", model: { providerId: "manticore", modelId: "glm" }, mode: "build", messages: [], toolCalls: [] }), { status: 200 })
      if (url.endsWith("/chimera/models")) return new Response(JSON.stringify([{ ref: "manticore/glm", isDefault: true }]), { status: 200 })
      if (url.endsWith("/chimera/modes")) return new Response(JSON.stringify([
        { name: "build", description: "Build mode", cycle: false },
        { name: "plan", description: "Plan mode", cycle: false },
      ]), { status: 200 })
      if (url.endsWith("/chimera/modes") === false && url.includes("/chimera/modes")) return new Response(null, { status: 204 })
      return new Response("[]", { status: 200 })
    })

    const view = render(<NativeAgentPanel agentId="task-1" />)
    await waitFor(() => expect(screen.queryByText("Loading Chimera session...")).toBeNull())

    fireEvent.click(within(view.container).getByTitle("More actions"))
    // Click "plan" from the overflow menu mode list
    fireEvent.click(await within(view.container).findByRole("button", { name: /plan/i }))

    await waitFor(() => {
      expect(fetchSpy).toHaveBeenCalledWith(expect.stringContaining("/chimera/modes"), expect.objectContaining({ method: "POST" }))
    })
  })
})
