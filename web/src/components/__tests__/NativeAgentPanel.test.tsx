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
    // Reset, not clear: a test may give send a behaviour of its own.
    send.mockReset()
    websocketConnected = true
  })

  afterEach(() => {
    cleanup()
    vi.restoreAllMocks()
  })

  it("fills its host when snapshot history contains only an assistant message", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => String(input).includes("/snapshot")
      ? new Response(JSON.stringify({
          id: "session-1",
          status: "idle",
          messages: [
            { role: "user", content: "Launch prompt" },
            { role: "assistant", content: "Ready" },
          ],
          toolCalls: [],
        }), { status: 200 })
      : new Response("[]", { status: 200 }))

    const view = render(<NativeAgentPanel agentId="agent-1" />)
    await within(view.container).findByText("Ready")
    const panel = view.container.firstElementChild as HTMLElement

    expect(panel.className).toContain("w-full")
    expect(panel.className).toContain("flex-1")
  })

  it("can hide its composer when embedded in the swarm command center", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const path = String(input)
      if (path.includes("/snapshot")) return new Response(JSON.stringify({ id: "session-1", status: "idle", messages: [], toolCalls: [] }), { status: 200 })
      return new Response("[]", { status: 200 })
    })
    render(<NativeAgentPanel agentId="agent-1" composerEnabled={false} />)
    await waitFor(() => expect(screen.queryByText("Loading Chimera session…")).toBeNull())
    expect(screen.queryByTestId("agent-composer")).toBeNull()
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
    fireEvent.click(within(view.container).getByRole("menuitem", { name: "Refresh from snapshot" }))
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
    fireEvent.click(within(view.container).getByRole("menuitem", { name: "Downgrade to tmux" }))

    expect(onDowngradeChimera).toHaveBeenCalledOnce()
    expect(await within(view.container).findByText("downgrade unavailable")).not.toBeNull()
  })

  it("offers to reload the agent when it can be reloaded", async () => {
    const onReload = vi.fn()
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => String(input).includes("/snapshot")
      ? new Response(JSON.stringify({ id: "session-1", status: "idle", messages: [], toolCalls: [] }), { status: 200 })
      : new Response("[]", { status: 200 }))

    const view = render(<NativeAgentPanel agentId="task-1" onReload={onReload} />)
    await within(view.container).findByText("Session ready — send a message to start")
    fireEvent.click(within(view.container).getByTitle("More actions"))
    fireEvent.click(within(view.container).getByRole("menuitem", { name: "Reload agent" }))

    expect(onReload).toHaveBeenCalledOnce()
  })

  it("renders and resolves a permission request from snapshot hydration", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => String(input).includes("/snapshot")
      ? new Response(JSON.stringify({
          id: "session-1",
          status: "waiting_for_permission",
          messages: [],
          toolCalls: [],
          pendingPermissions: [{
            requestId: "req-snapshot",
            tool: "bash",
            target: "host",
            command: "rm -rf /tmp/build-cache",
            reason: "Runs outside the sandbox",
            cwd: "/tmp",
            policyAnalysis: {
              version: 1,
              outcome: "approval_required",
              stages: [],
              findings: [{
                kind: "risk",
                stageId: "risk",
                level: "danger",
                message: "Deletes outside configured roots",
              }],
            },
          }],
        }), { status: 200 })
      : new Response("[]", { status: 200 }))

    render(<NativeAgentPanel agentId="task-1" />)
    const card = await screen.findByRole("region", { name: "Permission request for bash" })
    expect(within(card).getByText("rm -rf /tmp/build-cache")).not.toBeNull()
    expect(within(card).getByText("Runs outside the sandbox")).not.toBeNull()
    expect(within(card).getByText("Deletes outside configured roots")).not.toBeNull()

    fireEvent.click(within(card).getByRole("button", { name: "Deny" }))
    expect(send).toHaveBeenCalledWith({
      type: "agent_permission",
      agent_id: "task-1",
      request_id: "req-snapshot",
      decision: "deny",
    })
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

  it("speaks of Claude Code in a Claude Code session's permission prompt", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => String(input).includes("/snapshot")
      ? new Response(JSON.stringify({ id: "task-1", status: "idle", messages: [], toolCalls: [], pendingPermissions: [] }), { status: 200 })
      : new Response("[]", { status: 200 }))

    render(<NativeAgentPanel agentId="task-1" agentKind="claude-code" />)
    await screen.findByText("Session ready — send a message to start")
    act(() => websocketHandler?.({
      type: "chimera_event",
      agent_id: "task-1",
      content: JSON.stringify({ eventId: "event-1", type: "permission_request", requestId: "req-1", tool: "Edit", target: "host", command: "Edit /work/a.go" }),
    }))

    const card = screen.getByRole("region", { name: "Permission request for Edit" })
    // A Claude Code prompt covers any tool, not only commands run on the host.
    expect(within(card).getByText("Tool approval")).not.toBeNull()
    expect(within(card).queryByText("Host command approval")).toBeNull()
    fireEvent.click(within(card).getByRole("button", { name: "Deny with feedback" }))
    expect(within(card).getByLabelText("Feedback for Claude Code")).not.toBeNull()
  })

  it("sets messages Legato sent apart from what the person typed", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => String(input).includes("/snapshot")
      ? new Response(JSON.stringify({
          id: "task-1", status: "idle", toolCalls: [], pendingPermissions: [],
          messages: [
            { role: "system", content: "" },
            { role: "user", content: "[legato coordination] Agents already working in this Git worktree: 4wzjxm22 (ex-cs7ntyx6)." },
            { role: "user", content: "please rebase first" },
          ],
        }), { status: 200 })
      : new Response("[]", { status: 200 }))

    render(<NativeAgentPanel agentId="task-1" agentKind="claude-code" />)

    const notice = await screen.findByLabelText("Message from Legato: coordination")
    // The tag becomes the label; the body is what is left.
    expect(within(notice).getByText("Legato · coordination")).not.toBeNull()
    expect(within(notice).getByText("Agents already working in this Git worktree: 4wzjxm22 (ex-cs7ntyx6).")).not.toBeNull()
    expect(notice.className).not.toContain("bg-lw-selected")

    const typed = screen.getByText("please rebase first")
    expect(typed.className).toContain("bg-lw-selected")
    expect(screen.getAllByLabelText(/^Message from Legato/)).toHaveLength(1)

    // One that arrives while the panel is open is told apart the same way.
    act(() => websocketHandler?.({
      type: "chimera_event",
      agent_id: "task-1",
      content: JSON.stringify({ eventId: "event-1", type: "user_message", content: "[legato review] Question on step 2: why?" }),
    }))
    expect(screen.getByLabelText("Message from Legato: review")).not.toBeNull()
  })

  it("sets another agent's messages apart from Legato's and from what the person typed", async () => {
    const footer = "\n\n---\nAgent-to-agent message from `pm4d4m4e`. Reply with `legato agent message pm4d4m4e \"<reply>\"`."
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => String(input).includes("/snapshot")
      ? new Response(JSON.stringify({
          id: "task-1", status: "idle", toolCalls: [], pendingPermissions: [],
          messages: [
            { role: "system", content: "" },
            { role: "user", content: "[legato coordination] Agent pm4d4m4e (ex-1) joined Git worktree /repo." },
            { role: "user", content: `[legato peer pm4d4m4e] Please leave PromptBar.tsx alone.${footer}` },
            { role: "user", content: "please rebase first" },
          ],
        }), { status: 200 })
      : new Response("[]", { status: 200 }))

    render(<NativeAgentPanel agentId="task-1" agentKind="claude-code" />)

    const peer = await screen.findByLabelText("Message from agent pm4d4m4e")
    // The tag becomes the label, with the sender's ID as it is written, and
    // the reply instructions meant for the agent are left out.
    expect(within(peer).getByText("pm4d4m4e")).not.toBeNull()
    expect(within(peer).getByText("Please leave PromptBar.tsx alone.")).not.toBeNull()
    expect(screen.queryByText(/Agent-to-agent message/)).toBeNull()

    // Three writers, three looks.
    const notice = screen.getByLabelText("Message from Legato: coordination")
    const typed = screen.getByText("please rebase first")
    expect(screen.getAllByLabelText(/^Message from Legato/)).toHaveLength(1)
    expect(peer.className).not.toBe(notice.className)
    expect(peer.className).not.toContain("bg-lw-selected")
    expect(typed.className).toContain("bg-lw-selected")

    // A sender that did not name itself is still another agent.
    act(() => websocketHandler?.({
      type: "chimera_event",
      agent_id: "task-1",
      content: JSON.stringify({ eventId: "event-1", type: "user_message", content: "[legato peer] hello from a shell\n\n---\nAgent-to-agent message. Reply with `legato agent message <task-id> \"<reply>\"`." }),
    }))
    const unnamed = screen.getByLabelText("Message from another agent")
    expect(within(unnamed).getByText("hello from a shell")).not.toBeNull()
    expect(unnamed.className).toBe(peer.className)
  })

  it("shows the effort in force beside the model", async () => {
    const capabilities = {
      schemaVersion: 1, modelId: "claude-opus-5-5", profile: "claude-code", source: "test", fingerprint: "f",
      settings: { reasoningEffort: { status: "supported", type: "enum", values: ["low", "medium", "high", "xhigh", "max"] } },
    }
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const path = String(input)
      if (path.includes("/snapshot")) return new Response(JSON.stringify({
        id: "task-1", status: "idle", messages: [], toolCalls: [], pendingPermissions: [],
        model: { providerId: "anthropic", modelId: "claude-opus-5-5", settings: { reasoningEffort: "xhigh" }, settingsDefaults: { reasoningEffort: "medium" }, capabilities },
      }), { status: 200 })
      if (path.endsWith("/chimera/models")) return new Response(JSON.stringify([{ ref: "anthropic/claude-opus-5-5", isDefault: true }]), { status: 200 })
      return new Response("[]", { status: 200 })
    })

    render(<NativeAgentPanel agentId="task-1" agentKind="claude-code" />)
    const modelButton = await screen.findByTitle("Change model")

    expect(within(modelButton).getByText("xhigh")).not.toBeNull()

    // A choice made elsewhere (another client, the same panel's dialog) follows.
    act(() => websocketHandler?.({
      type: "chimera_event",
      agent_id: "task-1",
      content: JSON.stringify({
        eventId: "event-1", type: "model_settings_changed",
        settingsState: { model: "anthropic/claude-opus-5-5", overrides: { reasoningEffort: "low" }, defaults: { reasoningEffort: "medium" }, effective: { reasoningEffort: "low" }, capabilities },
      }),
    }))
    expect(within(screen.getByTitle("Change model")).getByText("low")).not.toBeNull()
  })

  it("sets the effort from the model menu, where the session's other settings also are", async () => {
    const capabilities = {
      schemaVersion: 1, modelId: "claude-opus-5-5", profile: "claude-code", source: "test", fingerprint: "f",
      settings: { reasoningEffort: { status: "supported", type: "enum", values: ["low", "medium", "high"] } },
    }
    const settingsState = { model: "anthropic/claude-opus-5-5", overrides: {}, defaults: { reasoningEffort: "medium" }, effective: { reasoningEffort: "medium" }, capabilities }
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
      const path = String(input)
      if (path.includes("/snapshot")) return new Response(JSON.stringify({
        id: "task-1", status: "idle", messages: [], toolCalls: [],
        model: { providerId: "anthropic", modelId: "claude-opus-5-5", settings: {}, settingsDefaults: { reasoningEffort: "medium" }, capabilities },
      }), { status: 200 })
      if (path.endsWith("/chimera/models")) return new Response(JSON.stringify([{ ref: "anthropic/claude-opus-5-5", isDefault: true }]), { status: 200 })
      if (path.endsWith("/chimera/model") && init?.method === "POST") return new Response(JSON.stringify({ settingsState: { ...settingsState, overrides: { reasoningEffort: "high" }, effective: { reasoningEffort: "high" } } }), { status: 200 })
      if (path.endsWith("/chimera/model")) return new Response(JSON.stringify(settingsState), { status: 200 })
      return new Response("[]", { status: 200 })
    })

    const view = render(<NativeAgentPanel agentId="task-1" agentKind="claude-code" />)
    fireEvent.click(await screen.findByTitle("Change model"))
    // The menu took the place of a settings button of its own.
    expect(within(view.container).queryByRole("button", { name: "Model settings" })).toBeNull()

    fireEvent.click(screen.getByRole("button", { name: "high" }))
    await waitFor(() => {
      const post = fetchSpy.mock.calls.find(([url, init]) => String(url).endsWith("/chimera/model") && init?.method === "POST")
      expect(JSON.parse(String(post?.[1]?.body))).toEqual({ expectedModel: "anthropic/claude-opus-5-5", settings: { reasoningEffort: "high" } })
    })
    await waitFor(() => expect(within(screen.getByTitle("Change model")).getByText("high")).not.toBeNull())

    fireEvent.click(screen.getByTitle("Change model"))
    fireEvent.click(screen.getByRole("button", { name: "More session settings…" }))
    expect(await screen.findByRole("dialog", { name: "Model settings" })).not.toBeNull()
  })

  it("shows which agent tool the native session belongs to", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => String(input).includes("/snapshot")
      ? new Response(JSON.stringify({ id: "task-1", status: "idle", messages: [], toolCalls: [], pendingPermissions: [] }), { status: 200 })
      : new Response("[]", { status: 200 }))

    render(<NativeAgentPanel agentId="task-1" agentKind="claude-code" />)
    await screen.findByText("Session ready — send a message to start")

    expect(screen.getByLabelText("Claude Code, native session")).not.toBeNull()
  })

  it("leaves the agent tool out of the status bar where the mobile agent picker shows it", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => String(input).includes("/snapshot")
      ? new Response(JSON.stringify({ id: "task-1", status: "idle", messages: [], toolCalls: [], pendingPermissions: [] }), { status: 200 })
      : new Response("[]", { status: 200 }))

    render(<NativeAgentPanel agentId="task-1" agentKind="claude-code" />)
    await screen.findByText("Session ready — send a message to start")

    // The picker replaces the sidebar below md, and names the tool itself.
    const badge = screen.getByLabelText("Claude Code, native session")
    expect(badge.classList.contains("hidden")).toBe(true)
    expect(badge.classList.contains("md:inline-flex")).toBe(true)
    expect(badge.classList.contains("inline-flex")).toBe(false)
  })

  it("names the Claude Code session while it loads", () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(() => new Promise<Response>(() => {}))

    render(<NativeAgentPanel agentId="task-1" agentKind="claude-code" />)

    expect(screen.getByText("Loading Claude Code session…")).not.toBeNull()
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
    const composer = await screen.findByPlaceholderText("Message task-1")
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
    const composer = await screen.findByPlaceholderText("Message task-1")
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
    const composer = await screen.findByPlaceholderText("Message task-1")
    composer.blur()
    fireEvent.keyDown(window, { key: "m" })
    expect(screen.getByText("(no macros configured)")).not.toBeNull()
    fireEvent.keyDown(window, { key: "1" })
    expect(send).not.toHaveBeenCalledWith(expect.objectContaining({ type: "agent_message" }))
  })

  it("opens with the agent's task, its pull request and its state", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => String(input).includes("/snapshot")
      ? new Response(JSON.stringify({ id: "session-1", status: "running", messages: [], toolCalls: [] }), { status: 200 })
      : new Response("[]", { status: 200 }))
    render(<NativeAgentPanel agentId="task-1" agentKind="claude-code" title="Complete the Cover section" stateTime="5h 44m" pullRequest={1334} />)
    const header = await screen.findByRole("banner")

    expect(within(header).getByRole("heading", { name: "Complete the Cover section" })).not.toBeNull()
    await waitFor(() => expect(within(header).getByText(/Running/).textContent).toBe("● Running 5h 44m"))
    expect(within(header).getByText("task-1 · PR #1334")).not.toBeNull()
  })

  it("says above the message box what the working agent runs, with Interrupt, and Queue under it", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => String(input).includes("/snapshot")
      ? new Response(JSON.stringify({ id: "session-1", status: "running", messages: [], toolCalls: [] }), { status: 200 })
      : new Response("[]", { status: 200 }))

    const view = render(<NativeAgentPanel agentId="task-1" />)
    const composer = await within(view.container).findByTestId("agent-composer")
    act(() => websocketHandler?.({
      type: "chimera_event",
      agent_id: "task-1",
      content: JSON.stringify({ eventId: "bash-1", type: "tool_call_start", callId: "call-1", name: "bash", args: { command: "./dev check --fe=all" } }),
    }))

    const status = within(view.container).getByRole("status", { name: "Agent working" })
    expect(within(status).getByText(/Working/)).not.toBeNull()
    expect(within(status).getByText("bash ./dev check --fe=all")).not.toBeNull()
    fireEvent.click(within(status).getByRole("button", { name: "Interrupt" }))
    expect(send).toHaveBeenCalledWith({ type: "agent_interrupt", agent_id: "task-1" })

    fireEvent.change(within(composer).getByPlaceholderText("Queue a message. Sent after the current step."), { target: { value: "Next" } })
    const actions = within(view.container).getByTestId("composer-actions")
    expect(within(actions).getByRole("button", { name: "Queue" })).not.toBeNull()
    expect(within(actions).queryByRole("button", { name: "Interrupt" })).toBeNull()
  })

  it("lists a queued message in the composer until the agent reads it", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => String(input).includes("/snapshot")
      ? new Response(JSON.stringify({ id: "session-1", status: "running", messages: [], toolCalls: [] }), { status: 200 })
      : new Response("[]", { status: 200 }))

    render(<NativeAgentPanel agentId="task-1" />)
    const box = await screen.findByRole("textbox", { name: "Message" })
    fireEvent.change(box, { target: { value: "then render it at mobile width" } })
    fireEvent.keyDown(box, { key: "Enter" })

    const queue = screen.getByRole("list", { name: "Queued messages" })
    expect(within(queue).getByText("then render it at mobile width")).not.toBeNull()
    expect((box as HTMLTextAreaElement).placeholder).toBe("Queue another")

    act(() => websocketHandler?.({
      type: "chimera_event",
      agent_id: "task-1",
      content: JSON.stringify({ eventId: "user-1", type: "user_message", content: "then render it at mobile width" }),
    }))
    expect(screen.queryByRole("list", { name: "Queued messages" })).toBeNull()
  })

  it("lets a queued shell command be edited or taken out before it runs", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => String(input).includes("/snapshot")
      ? new Response(JSON.stringify({ id: "session-1", status: "running", messages: [], toolCalls: [] }), { status: 200 })
      : new Response("[]", { status: 200 }))

    render(<NativeAgentPanel agentId="task-1" />)
    const box = await screen.findByRole("textbox", { name: "Message" }) as HTMLTextAreaElement
    for (const command of ["!git status", "!ls"]) {
      fireEvent.change(box, { target: { value: command } })
      fireEvent.keyDown(box, { key: "Enter" })
    }
    const queue = screen.getByRole("list", { name: "Queued messages" })
    expect(within(queue).getAllByRole("listitem")).toHaveLength(2)

    fireEvent.click(within(queue).getAllByRole("button", { name: "Remove queued message" })[1])
    expect(within(queue).getAllByRole("listitem")).toHaveLength(1)

    fireEvent.click(within(queue).getByRole("button", { name: "Edit" }))
    expect(box.value).toBe("!git status")
    expect(screen.queryByRole("list", { name: "Queued messages" })).toBeNull()
  })

  it("interrupts the working agent with Escape in the message box", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => String(input).includes("/snapshot")
      ? new Response(JSON.stringify({ id: "session-1", status: "running", messages: [], toolCalls: [] }), { status: 200 })
      : new Response("[]", { status: 200 }))

    render(<NativeAgentPanel agentId="task-1" />)
    fireEvent.keyDown(await screen.findByRole("textbox", { name: "Message" }), { key: "Escape" })

    expect(send).toHaveBeenCalledWith({ type: "agent_interrupt", agent_id: "task-1" })
  })

  it("leaves Escape alone while the agent is idle", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => String(input).includes("/snapshot")
      ? new Response(JSON.stringify({ id: "session-1", status: "idle", messages: [], toolCalls: [] }), { status: 200 })
      : new Response("[]", { status: 200 }))

    render(<NativeAgentPanel agentId="task-1" />)
    fireEvent.keyDown(await screen.findByRole("textbox", { name: "Message" }), { key: "Escape" })

    expect(send).not.toHaveBeenCalledWith({ type: "agent_interrupt", agent_id: "task-1" })
    expect(screen.queryByRole("status", { name: "Agent working" })).toBeNull()
  })

  it("expands a lone edit's diff by default without expanding other tool calls", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => String(input).includes("/snapshot")
      ? new Response(JSON.stringify({
          id: "session-1",
          status: "idle",
          messages: [
            { role: "user", content: "system" },
            { role: "assistant", content: [{ type: "tool-call" }] },
            { role: "assistant", content: [{ type: "text", text: "Running the tests" }, { type: "tool-call" }] },
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
    const composer = within(view.container).getByPlaceholderText("Message task-1")
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
    const composer = within(view.container).getByPlaceholderText("Message task-1")
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
    const composer = within(view.container).getByPlaceholderText("Message task-1")
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
    const composer = within(view.container).getByPlaceholderText("Message task-1")
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
    const composer = within(view.container).getByPlaceholderText("Message task-1") as HTMLTextAreaElement

    // No indicator when draft is empty or normal text
    expect(within(view.container).queryByText("bash")).toBeNull()
    fireEvent.change(composer, { target: { value: "hello" } })
    expect(within(view.container).queryByText("bash")).toBeNull()

    // Indicator appears when draft starts with !
    fireEvent.change(composer, { target: { value: "!ls" } })
    expect(within(view.container).getByText("bash")).not.toBeNull()

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
    const composer = within(view.container).getByPlaceholderText("Queue a message. Sent after the current step.")
    fireEvent.change(composer, { target: { value: "!echo done" } })
    fireEvent.click(within(view.container).getByRole("button", { name: "Run" }))

    await waitFor(() => expect(within(view.container).getByText("!echo done")).not.toBeNull())
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
    const composer = await within(view.container).findByPlaceholderText("Queue a message. Sent after the current step.")
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

    expect((await screen.findByLabelText("Context 50% full")).getAttribute("title")).toBe("128k of 256k context")
    expect(screen.getByText("128k")).not.toBeNull()
    expect(screen.queryByText(/3,800,000 tokens/)).toBeNull()
    // Half full is still green: amber from 70%, red from 85%, as elsewhere.
    expect(screen.getByLabelText("Context 50% full").querySelector(".bg-lw-live")).not.toBeNull()

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

    expect(screen.getByText("192k")).not.toBeNull()
    expect(screen.getByLabelText("Context 75% full").querySelector(".bg-lw-needs")).not.toBeNull()
  })

  it("says over the message box when the context is nearly full", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => String(input).includes("/snapshot")
      ? new Response(JSON.stringify({ id: "session-1", status: "idle", messages: [], toolCalls: [], contextWindow: 1_000_000, usedContextTokens: 840_000 }), { status: 200 })
      : new Response("[]", { status: 200 }))

    render(<NativeAgentPanel agentId="task-1" />)
    await screen.findByLabelText("Context 84% full")
    expect(screen.queryByRole("status", { name: "Context nearly full" })).toBeNull()

    act(() => websocketHandler?.({
      type: "chimera_event",
      agent_id: "task-1",
      content: JSON.stringify({ eventId: "usage-1", type: "usage_updated", contextWindow: 1_000_000, usedContextTokens: 912_000 }),
    }))
    expect(screen.getByRole("status", { name: "Context nearly full" }).textContent).toContain("912k of 1M used")
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

    // The latest subscription carries the cursor (the panel also asks what
    // the terminal is showing, so it is not the last message of all).
    const subscriptions = send.mock.calls.map(([message]) => message).filter((message) => message.type === "subscribe_agent")
    expect(subscriptions.at(-1)).toEqual({ type: "subscribe_agent", agent_id: "task-1", cursor: "cursor-1" })
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
    expect(within(view.container).queryByText("Loading Chimera session…")).toBeNull()
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
    fireEvent.click(within(view.container).getByRole("menuitem", { name: "Refresh from snapshot" }))
    expect(await within(view.container).findByText("Refreshed reply")).not.toBeNull()

    refreshFails = true
    fireEvent.click(within(view.container).getByTitle("More actions"))
    fireEvent.click(within(view.container).getByRole("menuitem", { name: "Refresh from snapshot" }))
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
    await waitFor(() => expect(screen.queryByText("Loading Chimera session…")).toBeNull())

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
    await waitFor(() => expect(screen.queryByText("Loading Chimera session…")).toBeNull())

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

    await waitFor(() => expect(screen.queryByText("Loading Chimera session…")).toBeNull())

    expect(screen.getAllByText("write", { selector: "span" })).toHaveLength(1)
    expect(screen.getByText("result.txt")).not.toBeNull()
    for (const text of ["First update", "Second update", "Writing the file", "Fourth update", "Fifth update"]) {
      expect(screen.getByText(text)).not.toBeNull()
    }
  })

  describe("consecutive tool calls", () => {
    type SnapshotCall = { callId: string; name: string; args: Record<string, unknown>; result?: Record<string, unknown> }

    // Each inner list is one assistant turn; a turn after the first opens with text.
    function mockToolTurns(turns: SnapshotCall[][], status = "idle") {
      vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => String(input).includes("/snapshot")
        ? new Response(JSON.stringify({
            id: "session-1",
            status,
            messages: [
              { role: "user", content: "system" },
              ...turns.map((calls, turn) => ({
                role: "assistant",
                content: [
                  ...(turn > 0 ? [{ type: "text", text: `Turn ${turn + 1}` }] : []),
                  ...calls.map(() => ({ type: "tool-call" })),
                ],
              })),
            ],
            toolCalls: turns.flat(),
          }), { status: 200 })
        : new Response("[]", { status: 200 }))
    }

    const read = (callId: string, path: string): SnapshotCall => ({ callId, name: "read", args: { path } })

    it("gathers them into one group and leaves a call after a reply on its own", async () => {
      mockToolTurns([
        [read("c1", "a.go"), { callId: "c2", name: "bash", args: { command: "go test ./..." } }, read("c3", "b.go")],
        [read("c4", "c.go")],
      ])

      const view = render(<NativeAgentPanel agentId="task-1" />)

      const group = await within(view.container).findByRole("group", { name: "3 tool calls" })
      expect(within(group).getByText("a.go")).not.toBeNull()
      expect(within(group).getByText("go test ./...")).not.toBeNull()
      expect(within(group).getByText("b.go")).not.toBeNull()
      expect(within(group).queryByText("c.go")).toBeNull()
      expect(within(view.container).getByText("c.go")).not.toBeNull()
      expect(within(view.container).getAllByRole("group")).toHaveLength(1)
    })

    it("keeps an edit's diff closed inside a group until it is opened", async () => {
      mockToolTurns([[
        { callId: "e1", name: "edit", args: { path: "main.go", old_string: "old line", new_string: "new line" } },
        read("c2", "b.go"),
      ]])

      const view = render(<NativeAgentPanel agentId="task-1" />)

      const group = await within(view.container).findByRole("group", { name: "2 tool calls" })
      expect(within(group).queryByText("- old line")).toBeNull()

      fireEvent.click(within(group).getByRole("button", { name: /main\.go/ }))

      expect(within(group).getByText("- old line")).not.toBeNull()
      expect(within(group).getByText("+ new line")).not.toBeNull()
    })

    it("shows the latest five of a long run under a count of the earlier ones", async () => {
      mockToolTurns([Array.from({ length: 8 }, (_, index) => read(`c${index + 1}`, `file-${index + 1}.go`))])

      const view = render(<NativeAgentPanel agentId="task-1" />)

      const group = await within(view.container).findByRole("group", { name: "8 tool calls" })
      const shown = () => Array.from({ length: 8 }, (_, index) => `file-${index + 1}.go`).filter((name) => within(group).queryByText(name))
      expect(shown()).toEqual(["file-4.go", "file-5.go", "file-6.go", "file-7.go", "file-8.go"])

      const earlier = within(group).getByRole("button", { name: /3 earlier tool calls/ })
      expect(earlier.getAttribute("aria-expanded")).toBe("false")
      // The count sits above the calls that follow the ones it stands for.
      expect(earlier.compareDocumentPosition(within(group).getByText("file-4.go")) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()

      fireEvent.click(earlier)
      expect(shown()).toHaveLength(8)
      expect(earlier.getAttribute("aria-expanded")).toBe("true")

      fireEvent.click(earlier)
      expect(shown()).toHaveLength(5)
    })

    it("adds a live call to the run in progress and folds only once that saves a line", async () => {
      mockToolTurns([Array.from({ length: 5 }, (_, index) => read(`c${index + 1}`, `file-${index + 1}.go`))], "running")
      const liveRead = (n: number) => act(() => websocketHandler?.({
        type: "chimera_event",
        agent_id: "task-1",
        content: JSON.stringify({ eventId: `live-${n}`, type: "tool_call_start", callId: `c${n}`, name: "read", args: { path: `file-${n}.go` } }),
      }))

      const view = render(<NativeAgentPanel agentId="task-1" />)
      await within(view.container).findByRole("group", { name: "5 tool calls" })

      // Folding one call behind a count would take the line it frees.
      liveRead(6)
      const six = within(view.container).getByRole("group", { name: "6 tool calls" })
      expect(within(six).getByText("file-1.go")).not.toBeNull()
      expect(within(six).getByText("file-6.go")).not.toBeNull()
      expect(within(six).queryByRole("button", { name: /earlier tool call/ })).toBeNull()

      liveRead(7)
      const seven = within(view.container).getByRole("group", { name: "7 tool calls" })
      expect(within(seven).getByRole("button", { name: /2 earlier tool calls/ })).not.toBeNull()
      expect(within(seven).queryByText("file-2.go")).toBeNull()
      expect(within(seven).getByText("file-3.go")).not.toBeNull()
      expect(within(seven).getByText("file-7.go")).not.toBeNull()
    })

    it("says which tools the earlier calls used", async () => {
      mockToolTurns([[
        { callId: "c1", name: "bash", args: { command: "git status" } },
        read("c2", "a.go"),
        { callId: "c3", name: "edit", args: { path: "a.go", old_string: "x", new_string: "y" } },
        read("c4", "b.go"),
        ...Array.from({ length: 5 }, (_, index) => read(`late-${index}`, `late-${index}.go`)),
      ]])

      const view = render(<NativeAgentPanel agentId="task-1" />)

      const earlier = await within(view.container).findByRole("button", { name: /4 earlier tool calls/ })
      expect(within(earlier).getByText("2 read · 1 bash · 1 edit")).not.toBeNull()
    })

    it("leaves a subagent in its own card between the runs around it", async () => {
      mockToolTurns([[
        read("c1", "a.go"),
        read("c2", "b.go"),
        { callId: "spawn-1", name: "spawn_agent", args: { purpose: "Review the change", prompt: "Review it" } },
        read("c3", "c.go"),
        read("c4", "d.go"),
      ]])

      const view = render(<NativeAgentPanel agentId="task-1" />)

      const card = await within(view.container).findByTestId("subagent-card-spawn-1")
      const groups = within(view.container).getAllByRole("group", { name: "2 tool calls" })
      expect(groups).toHaveLength(2)
      expect(groups.some((group) => group.contains(card))).toBe(false)
      expect(within(groups[0]).getByText("b.go")).not.toBeNull()
      expect(within(groups[1]).getByText("c.go")).not.toBeNull()
    })
  })

  it("renders a model dropdown with models from the models endpoint", async () => {
    const modelsData = [
      { ref: "manticore/glm", contextWindow: 256000, isDefault: true },
      { ref: "manticore/gpt", contextWindow: 256000, vision: true, capabilities: { inputModalities: ["text", "image"] }, isDefault: false },
      { ref: "manticore/kimi", contextWindow: 256000, vision: true, isDefault: false },
    ]
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const url = String(input)
      if (url.includes("/snapshot")) return new Response(JSON.stringify({ id: "session-1", status: "idle", model: { providerId: "manticore", modelId: "glm" }, messages: [], toolCalls: [] }), { status: 200 })
      if (url.endsWith("/chimera/models")) return new Response(JSON.stringify(modelsData), { status: 200 })
      return new Response("[]", { status: 200 })
    })

    render(<NativeAgentPanel agentId="task-1" />)
    await waitFor(() => expect(screen.queryByText("Loading Chimera session…")).toBeNull())

    const modelButton = screen.getByTitle("Change model")
    expect(modelButton).not.toBeNull()
    expect(within(modelButton as HTMLElement).getByText("glm")).not.toBeNull()

    fireEvent.click(modelButton)
    expect(await screen.findByText("manticore/gpt")).not.toBeNull()
    expect(screen.getByText("manticore/kimi")).not.toBeNull()
    // The "default" badge appears on the default model entry
    const defaultBadges = screen.getAllByText("default")
    expect(defaultBadges.length).toBeGreaterThanOrEqual(1)
    expect(screen.getAllByTitle("vision")).toHaveLength(2)
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
    await waitFor(() => expect(screen.queryByText("Loading Chimera session…")).toBeNull())

    fireEvent.click(screen.getByTitle("Change model"))
    const dropdownItems = await screen.findAllByText("manticore/gpt")
    fireEvent.click(dropdownItems[0])

    await waitFor(() => {
      expect(fetchSpy).toHaveBeenCalledWith(expect.stringContaining("/chimera/model"), expect.objectContaining({ method: "POST" }))
    })
    await waitFor(() => expect(screen.getByTitle("Change model").textContent).toContain("gpt"))
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
    await waitFor(() => expect(screen.queryByText("Loading Chimera session…")).toBeNull())

    const modelCallsBefore = fetchSpy.mock.calls.filter((c) => String(c[0]).includes("/chimera/model"))
    fireEvent.click(screen.getByTitle("Change model"))
    // The dropdown item for the active model is the second match (first is the button label)
    const dropdownItems = await screen.findAllByText("manticore/glm")
    fireEvent.click(dropdownItems[dropdownItems.length - 1])
    const modelCallsAfter = fetchSpy.mock.calls.filter((c) => String(c[0]).includes("/chimera/model"))
    expect(modelCallsAfter.length).toBe(modelCallsBefore.length)
  })

  it("keeps the short model name and gives the mode label way to its icon below the sm breakpoint", async () => {
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
    await waitFor(() => expect(screen.queryByText("Loading Chimera session…")).toBeNull())

    // On a phone the chip keeps the model's short name; the provider is
    // named in the menu only.
    const modelButton = screen.getByTitle("Change model")
    const modelRefSpan = within(modelButton as HTMLElement).getByText("glm")
    expect(modelRefSpan.className).not.toContain("hidden")

    // The mode selector stays on a phone, its label giving way to its icon
    const modeButton = screen.getByTitle("Change mode")
    expect(within(modeButton as HTMLElement).getByText("Mode").className).toContain("sm:inline")

    // Macros stays visible (not collapsed)
    const macrosButton = screen.getByTitle("Macros (M)")
    expect(macrosButton.parentElement!.className).not.toContain("hidden")

    // Dot menu is always present
    expect(view.container.querySelector('[title="More actions"]')).not.toBeNull()
  })

  it("offers the modes from the composer's mode button", async () => {
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
    await waitFor(() => expect(screen.queryByText("Loading Chimera session…")).toBeNull())

    fireEvent.click(within(view.container).getByTitle("Change mode"))

    const buildButtons = await within(view.container).findAllByRole("button", { name: /build/i })
    expect(buildButtons.length).toBeGreaterThanOrEqual(1)
    expect(within(view.container).getByText("plan")).not.toBeNull()
  })

  it("switches mode from the composer's mode list", async () => {
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
    await waitFor(() => expect(screen.queryByText("Loading Chimera session…")).toBeNull())

    fireEvent.click(within(view.container).getByTitle("Change mode"))
    fireEvent.click(await within(view.container).findByRole("button", { name: /plan/i }))

    await waitFor(() => {
      expect(fetchSpy).toHaveBeenCalledWith(expect.stringContaining("/chimera/modes"), expect.objectContaining({ method: "POST" }))
    })
  })
})
