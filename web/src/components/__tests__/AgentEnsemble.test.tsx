import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { AgentEnsemble } from "../AgentEnsemble"
import type { AgentInfo } from "../../hooks/useWebSocket"
import type { AgentConversation } from "../../lib/agentGlance"

const { apiFetch } = vi.hoisted(() => ({ apiFetch: vi.fn() }))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../lib/api", () => ({ apiFetch }))

const agent = (values: Partial<AgentInfo> & Pick<AgentInfo, "task_id">): AgentInfo => ({
  id: 1,
  task_title: `${values.task_id} title`,
  tmux_session: `legato-${values.task_id}`,
  command: "claude",
  agent_kind: "claude-code",
  status: "running",
  activity: "",
  started_at: "now",
  working_seconds: 0,
  waiting_seconds: 0,
  error_seconds: 0,
  has_chimera_endpoint: true,
  ...values,
})

const conversation = (values: Partial<AgentConversation>): AgentConversation => ({
  task_id: "", status: "idle", entries: [], permission: null, question: null, context_used: 0, context_window: 0, ...values,
})

// What each agent's endpoints answer: a conversation, the text of a pane, or
// a status that refuses.
let conversations: Record<string, AgentConversation | number>
let panes: Record<string, string>

function show(agents: AgentInfo[]) {
  const handlers = { onOpen: vi.fn(), onSpawn: vi.fn(), onShowList: vi.fn() }
  render(<AgentEnsemble agents={agents} {...handlers} />)
  return handlers
}

const tile = (taskId: string) => screen.getByRole("article", { name: new RegExp(taskId) })

describe("AgentEnsemble", () => {
  beforeEach(() => {
    conversations = {}
    panes = {}
    apiFetch.mockReset()
    apiFetch.mockImplementation(async (_baseUrl: string, path: string) => {
      const [, taskId, what] = /^\/api\/agents\/([^/]+)\/(conversation|preview)/.exec(path) ?? []
      if (what === "preview" && taskId in panes) {
        return new Response(JSON.stringify({ task_id: taskId, content: panes[taskId], captured_at: "now" }), { status: 200 })
      }
      const answer = what === "conversation" ? conversations[taskId] : undefined
      if (typeof answer === "object") return new Response(JSON.stringify(answer), { status: 200 })
      return new Response("not available", { status: typeof answer === "number" ? answer : 404 })
    })
  })
  afterEach(cleanup)

  it("shows every agent as a tile, the ones that need the person first", () => {
    show([
      agent({ task_id: "idle-1" }),
      agent({ task_id: "busy-1", activity: "working" }),
      agent({ task_id: "ask-1", activity: "waiting" }),
    ])

    const order = screen.getAllByRole("article").map((article) => article.getAttribute("aria-label"))
    expect(order).toEqual(["ask-1, needs you", "busy-1, running", "idle-1, idle"])
    expect(within(tile("busy-1")).getByText("busy-1 title")).toBeTruthy()
  })

  it("counts the agents by what they are doing", () => {
    show([
      agent({ task_id: "idle-1" }),
      agent({ task_id: "idle-2" }),
      agent({ task_id: "busy-1", activity: "working" }),
      agent({ task_id: "fail-1", activity: "error" }),
    ])

    const counts = screen.getByRole("list", { name: "Agents by state" })
    expect(within(counts).getAllByRole("listitem").map((item) => item.textContent)).toEqual(["1 needs you", "1 running", "2 idle"])
  })

  it("shows what a native agent said and did, read from its conversation", async () => {
    conversations["busy-1"] = conversation({
      status: "running",
      entries: [
        { kind: "user", text: "add a --json flag" },
        { kind: "tool", tool: "read", text: "list.ts", state: "done" },
        { kind: "tool", tool: "bash", text: "Run the list tests", command: "pnpm vitest", state: "failed" },
        { kind: "assistant", text: "One test fails. **Fixing** it." },
        { kind: "tool", tool: "bash", text: "Build it", state: "running" },
      ],
    })
    show([agent({ task_id: "busy-1", activity: "working" })])

    const busy = within(tile("busy-1"))
    await waitFor(() => expect(busy.getByText("One test fails. Fixing it.")).toBeTruthy())
    expect(busy.getByText("add a --json flag")).toBeTruthy()
    expect(busy.getByText("Run the list tests").closest("[data-state]")?.getAttribute("data-state")).toBe("failed")
    expect(busy.getByText("Build it").closest("[data-state]")?.getAttribute("data-state")).toBe("running")
    expect(apiFetch).toHaveBeenCalledWith("", "/api/agents/busy-1/conversation?limit=40")
  })

  it("shows the pane of an agent driven through its terminal", async () => {
    panes["shell-1"] = "➜ legato git:(main) ✗ make test\nok"
    show([agent({ task_id: "shell-1", agent_kind: "shell", has_chimera_endpoint: false })])

    await waitFor(() => expect(within(tile("shell-1")).getByText(/make test/)).toBeTruthy())
    expect(apiFetch).toHaveBeenCalledWith("", "/api/agents/shell-1/preview")
    expect(apiFetch).not.toHaveBeenCalledWith("", expect.stringContaining("/conversation"))
  })

  it("says what an agent waits to be allowed", async () => {
    conversations["ask-1"] = conversation({
      status: "waiting_for_permission",
      permission: { tool: "bash", command: "rm -rf dist && pnpm build" },
    })
    show([agent({ task_id: "ask-1", activity: "waiting" })])

    const ask = within(tile("ask-1"))
    await waitFor(() => expect(ask.getByText("Wants to run:")).toBeTruthy())
    expect(ask.getByText("rm -rf dist && pnpm build")).toBeTruthy()
  })

  it("says what an agent asks", async () => {
    conversations["ask-1"] = conversation({
      status: "waiting_for_input",
      question: { text: "Which color?", options: ["Red", "Blue"] },
    })
    show([agent({ task_id: "ask-1", activity: "waiting" })])

    const ask = within(tile("ask-1"))
    await waitFor(() => expect(ask.getByText("Which color?")).toBeTruthy())
    expect(ask.getByText("Red · Blue")).toBeTruthy()
  })

  it("shows how full a native agent's context is", async () => {
    conversations["busy-1"] = conversation({ context_used: 138_000, context_window: 200_000 })
    show([agent({ task_id: "busy-1", activity: "working" })])

    await waitFor(() => expect(within(tile("busy-1")).getByLabelText("Context 69% full")).toBeTruthy())
  })

  it("says so when an agent has said nothing yet", async () => {
    conversations["idle-1"] = conversation({})
    show([agent({ task_id: "idle-1" })])

    await waitFor(() => expect(within(tile("idle-1")).getByText("No messages yet.")).toBeTruthy())
  })

  it("says so when an agent's conversation cannot be read", async () => {
    conversations["busy-1"] = 502
    show([agent({ task_id: "busy-1", activity: "working" })])

    await waitFor(() => expect(within(tile("busy-1")).getByText("Could not read this agent's conversation.")).toBeTruthy())
  })

  it("opens the agent whose tile is picked", () => {
    const { onOpen } = show([agent({ task_id: "idle-1" }), agent({ task_id: "busy-1", activity: "working" })])

    fireEvent.click(screen.getByRole("button", { name: "Open busy-1" }))

    expect(onOpen).toHaveBeenCalledWith("busy-1")
  })

  it("offers to spawn an agent and to go back to the list", () => {
    const { onSpawn, onShowList } = show([agent({ task_id: "idle-1" })])

    fireEvent.click(screen.getByRole("button", { name: "Spawn agent" }))
    fireEvent.click(screen.getByRole("button", { name: "List" }))

    expect(onSpawn).toHaveBeenCalledTimes(1)
    expect(onShowList).toHaveBeenCalledTimes(1)
  })

  it("reads a tile again while it is shown", async () => {
    vi.useFakeTimers()
    try {
      conversations["busy-1"] = conversation({ entries: [{ kind: "assistant", text: "Starting." }] })
      show([agent({ task_id: "busy-1", activity: "working" })])
      await vi.waitFor(() => expect(within(tile("busy-1")).getByText("Starting.")).toBeTruthy())

      conversations["busy-1"] = conversation({ entries: [{ kind: "assistant", text: "Finished." }] })
      await vi.advanceTimersByTimeAsync(3000)

      await vi.waitFor(() => expect(within(tile("busy-1")).getByText("Finished.")).toBeTruthy())
    } finally {
      vi.useRealTimers()
    }
  })
})
