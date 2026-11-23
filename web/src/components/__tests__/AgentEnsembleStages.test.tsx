import { cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { AgentEnsemble, ENSEMBLE_LAYOUT_KEY } from "../AgentEnsemble"
import type { AgentInfo } from "../../hooks/useWebSocket"
import type { Approval } from "../../lib/approvals"
import { getRailStateForTests as railState } from "../../lib/railState"

const { apiFetch } = vi.hoisted(() => ({ apiFetch: vi.fn() }))
vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../lib/api", () => ({ apiFetch }))

const agent = (task_id: string, stage: string | undefined, glance: string, fields: Partial<AgentInfo> = {}): AgentInfo => ({
  id: 1, task_id, task_title: `${task_id} title`, tmux_session: "", command: "claude", agent_kind: "claude-code",
  status: "running", activity: "", started_at: "now", working_seconds: 0, waiting_seconds: 0, error_seconds: 0,
  stage, glance, ...fields,
})

const agents = [
  agent("LEG-438", "exploring", "your_turn"),
  agent("LEG-406", "exploring", "working", { activity: "working" }),
  agent("LEG-412", "planning", "needs_you", { plan_pending: true }),
  agent("LEG-409", "implementing", "needs_you", { activity: "waiting" }),
  agent("LEG-407", "implementing", "working", { activity: "working" }),
  agent("LEG-408", "reviewing", "working", { activity: "background" }),
  agent("LEG-405", "finished", "finished"),
  agent("REEVE", undefined, "your_turn", { reeve: true }),
]

const bands = () => screen.getAllByRole("region").map((band) => band.getAttribute("aria-label")).filter((name) => name !== "Ensemble")
const emptyLines = () => screen.getAllByRole("note").map((line) => line.textContent)

function show() {
  render(<AgentEnsemble agents={agents} onOpen={vi.fn()} onSpawn={vi.fn()} onShowList={vi.fn()} />)
}

beforeEach(() => {
  localStorage.clear()
  apiFetch.mockReset()
  apiFetch.mockResolvedValue(new Response("not available", { status: 404 }))
})
afterEach(cleanup)

describe("the ensemble's stages", () => {
  it("counts the agents by glance, leaving the Reeve out", () => {
    show()
    const counts = screen.getByRole("list", { name: "Agents by state" })
    expect(within(counts).getAllByRole("listitem").map((item) => item.textContent)).toEqual([
      "◆ 2 need you", "◇ 1 your turn", "● 3 working", "✓ 1 finished",
    ])
  })

  it("names each tile's stage beside its state", () => {
    show()
    const tile = screen.getByRole("article", { name: /LEG-409/ })
    expect(within(tile).getByRole("img", { name: "Implementing" })).toBeTruthy()
    expect(within(tile).getByText("Implementing")).toBeTruthy()
  })

  it("starts in tiles, and lays the agents out in stage bands when asked, remembering it", () => {
    show()
    expect(screen.queryByRole("region", { name: "Exploring" })).toBeNull()
    fireEvent.click(within(screen.getByRole("group", { name: "Layout" })).getByRole("button", { name: "By stage" }))
    expect(bands()).toEqual(["Exploring", "Planning", "Implementing", "Reviewing", "Finished"])
    const implementing = screen.getByRole("region", { name: "Implementing" })
    expect(within(implementing).getAllByRole("article").map((tile) => tile.getAttribute("aria-label")?.split(",")[0])).toEqual(["LEG-409", "LEG-407"])
    expect(screen.queryByRole("article", { name: /REEVE/ })).toBeNull()
    expect(localStorage.getItem(ENSEMBLE_LAYOUT_KEY)).toBe("stage")

    cleanup()
    show()
    expect(screen.getByRole("region", { name: "Planning" })).toBeTruthy()
  })

  it("does not repeat a band's stage on its cards", () => {
    localStorage.setItem(ENSEMBLE_LAYOUT_KEY, "stage")
    show()
    const tile = within(screen.getByRole("region", { name: "Implementing" })).getByRole("article", { name: /LEG-409/ })
    expect(within(tile).queryByText("Implementing")).toBeNull()
    expect(within(tile).queryByRole("img", { name: "Implementing" })).toBeNull()
  })

  it("names a band by its track, word and count, with the needs-you marks", () => {
    localStorage.setItem(ENSEMBLE_LAYOUT_KEY, "stage")
    show()
    const head = (name: string) => within(screen.getByRole("region", { name })).getByRole("heading", { level: 2 })
    expect(head("Implementing").textContent).toBe("Implementing2◆ 1")
    expect(within(head("Implementing")).getByRole("img", { name: "Implementing" })).toBeTruthy()
    expect(head("Exploring").textContent).toBe("Exploring2◇ 1")
    expect(head("Reviewing").textContent).toBe("Reviewing1")
  })

  it("merges stages with nothing in them into one line that names them", () => {
    localStorage.setItem(ENSEMBLE_LAYOUT_KEY, "stage")
    // The screenshots' three agents.
    const three = [
      agent("LEG-386", "implementing", "your_turn"),
      agent("LEG-408", "reviewing", "working", { activity: "background" }),
      agent("LEG-405", "finished", "finished"),
      agent("REEVE", undefined, "your_turn", { reeve: true }),
    ]
    render(<AgentEnsemble agents={three} onOpen={vi.fn()} onSpawn={vi.fn()} onShowList={vi.fn()} />)
    expect(bands()).toEqual(["Implementing", "Reviewing", "Finished"])
    expect(emptyLines()).toEqual(["Exploring · Planning · none"])
    expect(screen.queryByText("Nothing here.")).toBeNull()
    const counts = screen.getByRole("list", { name: "Agents by state" })
    expect(within(counts).getAllByRole("listitem").map((item) => item.textContent)).toEqual(["◇ 1 your turn", "● 1 working", "✓ 1 finished"])
  })

  it("draws one band when every other stage is empty", () => {
    localStorage.setItem(ENSEMBLE_LAYOUT_KEY, "stage")
    render(<AgentEnsemble agents={[agent("LEG-408", "reviewing", "working", { activity: "working" })]} onOpen={vi.fn()} onSpawn={vi.fn()} onShowList={vi.fn()} />)
    expect(bands()).toEqual(["Reviewing"])
    expect(emptyLines()).toEqual(["Exploring · Planning · Implementing · none", "Finished · none"])
  })

  it("opens by itself, on a phone, a stage holding an agent that needs you", () => {
    localStorage.setItem(ENSEMBLE_LAYOUT_KEY, "stage")
    show()
    const toggle = (name: string) => within(screen.getByRole("region", { name })).getByRole("button", { name: new RegExp(`^${name}`) })
    expect(toggle("Planning").getAttribute("aria-expanded")).toBe("true")
    expect(toggle("Implementing").getAttribute("aria-expanded")).toBe("true")
    expect(toggle("Exploring").getAttribute("aria-expanded")).toBe("false")
    expect(toggle("Finished").getAttribute("aria-expanded")).toBe("false")
    fireEvent.click(toggle("Exploring"))
    expect(toggle("Exploring").getAttribute("aria-expanded")).toBe("true")
  })

  it("opens a stage by itself when one of its agents comes to need you", () => {
    localStorage.setItem(ENSEMBLE_LAYOUT_KEY, "stage")
    const working = [agent("LEG-406", "exploring", "working", { activity: "working" })]
    const { rerender } = render(<AgentEnsemble agents={working} onOpen={vi.fn()} onSpawn={vi.fn()} onShowList={vi.fn()} />)
    const toggle = () => within(screen.getByRole("region", { name: "Exploring" })).getByRole("button", { name: /^Exploring/ })
    expect(toggle().getAttribute("aria-expanded")).toBe("false")
    rerender(<AgentEnsemble agents={[{ ...working[0], activity: "waiting", glance: "needs_you" }]} onOpen={vi.fn()} onSpawn={vi.fn()} onShowList={vi.fn()} />)
    expect(toggle().getAttribute("aria-expanded")).toBe("true")
  })
})

describe("the cards in the stage bands", () => {
  const replies: Record<string, string> = {}
  const long = "First line of the reply.\nSecond line.\nThird line.\nFourth line.\nFifth line, past the clamp.\nSixth."

  beforeEach(() => {
    localStorage.setItem(ENSEMBLE_LAYOUT_KEY, "stage")
    for (const key of Object.keys(replies)) delete replies[key]
    apiFetch.mockImplementation(async (_baseUrl: string, path: string) => {
      const [, taskId] = /^\/api\/agents\/([^/]+)\/conversation/.exec(path) ?? []
      if (!taskId) return new Response("not available", { status: 404 })
      const entries = [
        { kind: "user", text: "go" },
        ...(taskId === "LEG-407" ? [{ kind: "tool", tool: "bash", text: "go test ./internal/server", state: "running" }] : []),
        ...(replies[taskId] ? [{ kind: "assistant", text: replies[taskId] }] : []),
      ]
      return new Response(JSON.stringify({ task_id: taskId, status: "idle", entries, permission: null, question: null, context_used: 0, context_window: 0 }), { status: 200 })
    })
  })

  const native = (id: string, stage: string, glance: string, fields: Partial<AgentInfo> = {}) => agent(id, stage, glance, { has_chimera_endpoint: true, stage_reason: "6 files changed", ...fields })
  const card = (id: string) => screen.getByRole("article", { name: new RegExp(`^${id},`) })

  it("leads with the glance and the time, then the ID, the title and why · tool", () => {
    render(<AgentEnsemble agents={[native("LEG-386", "implementing", "your_turn", { working_seconds: 360 })]} onOpen={vi.fn()} onSpawn={vi.fn()} onShowList={vi.fn()} />)
    const shown = card("LEG-386")
    expect(shown.getAttribute("aria-label")).toBe("LEG-386, your turn")
    expect(within(shown).getByText("◇ Your turn · 6m")).toBeTruthy()
    expect(within(shown).getByText("LEG-386 title")).toBeTruthy()
    expect(within(shown).getByText("6 files changed · Claude Code")).toBeTruthy()
  })

  it("shows a long last reply whole, for the clamp to cut at four lines, never a fixed height", async () => {
    replies["LEG-386"] = long
    render(<AgentEnsemble agents={[native("LEG-386", "implementing", "your_turn")]} onOpen={vi.fn()} onSpawn={vi.fn()} onShowList={vi.fn()} />)
    const quote = await within(card("LEG-386")).findByRole("blockquote")
    expect(quote.textContent).toBe(long)
    expect(within(card("LEG-386")).queryByText("Last reply")).toBeNull()
  })

  it("shows what a working agent is doing on one line", async () => {
    render(<AgentEnsemble agents={[native("LEG-407", "implementing", "working", { activity: "working" })]} onOpen={vi.fn()} onSpawn={vi.fn()} onShowList={vi.fn()} />)
    const shown = card("LEG-407")
    expect(within(shown).getByText("● Working")).toBeTruthy()
    expect((await within(shown).findByLabelText("Now")).textContent).toBe("bash go test ./internal/server")
  })

  it("draws background work as working, in its own words", () => {
    render(<AgentEnsemble agents={[native("LEG-408", "reviewing", "working", { activity: "background", background_jobs: [{ kind: "bash", id: "b1", label: "npm run build", started_at: "" }] })]} onOpen={vi.fn()} onSpawn={vi.fn()} onShowList={vi.fn()} />)
    expect(within(card("LEG-408")).getByText("◌ Working · background")).toBeTruthy()
    expect(within(card("LEG-408")).getByLabelText("Now").textContent).toBe("bash: npm run build")
  })

  it("puts what a waiting agent asks in a needs box, with Allow and Deny", () => {
    const onDecide = vi.fn()
    const approval = {
      id: "ap-1", source: "agent_permission", source_key: "", task_id: "LEG-409", title: "Run a command", summary: "go test ./...", details: { command: "go test ./..." },
      options: [{ id: "once", label: "Allow once", kind: "allow" as const }, { id: "no", label: "Deny", kind: "deny" as const }],
      blocking: true, status: "pending", revision: 1, created_at: "",
    }
    render(<AgentEnsemble agents={[native("LEG-409", "implementing", "needs_you", { activity: "waiting" })]} approvals={[approval]} onDecide={onDecide} onOpen={vi.fn()} onSpawn={vi.fn()} onShowList={vi.fn()} />)
    const shown = card("LEG-409")
    expect(within(shown).getByText("◆ Needs you")).toBeTruthy()
    const box = within(shown).getByRole("group", { name: "Needs you" })
    // What will run, not only the approval's generic title.
    expect(within(box).getByText("Run a command")).toBeTruthy()
    expect(within(box).getByText("go test ./...")).toBeTruthy()
    fireEvent.click(within(box).getByRole("button", { name: "Allow" }))
    expect(onDecide).toHaveBeenCalledWith(approval, { decision: "allow", option: "once" })
    fireEvent.click(within(box).getByRole("button", { name: "Deny" }))
    expect(onDecide).toHaveBeenLastCalledWith(approval, { decision: "deny" })
  })

  const pending = (fields: Partial<Approval>): Approval => ({
    id: "ap-1", source: "agent_permission", source_key: "", task_id: "LEG-409", title: "Run a command", details: {},
    options: [{ id: "once", label: "Allow once", kind: "allow" }, { id: "no", label: "Deny", kind: "deny" }],
    blocking: true, status: "pending", revision: 1, created_at: "", ...fields,
  })

  it("shows the plan an approval asks to approve before Allow", () => {
    const approval = pending({ title: "Approve a plan", details: { plan: "# Plan\n\n1. Add the band" } })
    render(<AgentEnsemble agents={[native("LEG-409", "planning", "needs_you", { activity: "waiting" })]} approvals={[approval]} onDecide={vi.fn()} onOpen={vi.fn()} onSpawn={vi.fn()} onShowList={vi.fn()} />)
    const box = within(card("LEG-409")).getByRole("group", { name: "Needs you" })
    expect(box.textContent).toContain("1. Add the band")
  })

  it("puts each slot's approval on its own card, by the execution that asked", () => {
    const reviewers = pending({ id: "ap-r", execution_id: "ex-r", summary: "gh pr view 12", details: { command: "gh pr view 12", slot: "reviewer" } })
    const authors = pending({ id: "ap-m", execution_id: "ex-m", summary: "go test ./...", details: { command: "go test ./..." } })
    const onDecide = vi.fn()
    render(<AgentEnsemble agents={[
      native("LEG-409", "implementing", "needs_you", { activity: "waiting", execution_id: "ex-m" }),
      native("LEG-409_reviewer", "implementing", "needs_you", { activity: "waiting", execution_id: "ex-r", card_task_id: "LEG-409", slot: "reviewer" }),
    ]} approvals={[reviewers, authors]} onDecide={onDecide} onOpen={vi.fn()} onSpawn={vi.fn()} onShowList={vi.fn()} />)
    const author = within(card("LEG-409")).getByRole("group", { name: "Needs you" })
    expect(author.textContent).toContain("go test ./...")
    fireEvent.click(within(author).getByRole("button", { name: "Allow" }))
    expect(onDecide).toHaveBeenCalledWith(authors, { decision: "allow", option: "once" })
    const reviewer = within(card("LEG-409_reviewer")).getByRole("group", { name: "Needs you" })
    expect(reviewer.textContent).toContain("gh pr view 12")
  })

  it("matches a slot's approval by task and slot from a server without execution IDs", () => {
    const reviewers = pending({ id: "ap-r", summary: "gh pr view 12", details: { command: "gh pr view 12", slot: "reviewer" } })
    render(<AgentEnsemble agents={[
      native("LEG-409", "implementing", "needs_you", { activity: "waiting" }),
      native("LEG-409_reviewer", "implementing", "needs_you", { activity: "waiting", card_task_id: "LEG-409", slot: "reviewer" }),
    ]} approvals={[reviewers]} onDecide={vi.fn()} onOpen={vi.fn()} onSpawn={vi.fn()} onShowList={vi.fn()} />)
    expect(within(card("LEG-409")).queryByRole("button", { name: "Allow" })).toBeNull()
    expect(within(card("LEG-409_reviewer")).getByRole("button", { name: "Allow" })).toBeTruthy()
  })

  it("sends a signed approval, or one that is not an agent's permission, to the approval itself", () => {
    const onDecide = vi.fn()
    render(<AgentEnsemble agents={[native("LEG-409", "implementing", "needs_you", { activity: "waiting" })]}
      approvals={[pending({ source: "credential", signed: true, title: "Use the GitHub token", details: { credential: "github", operations: ["push"] } })]}
      onDecide={onDecide} onOpen={vi.fn()} onSpawn={vi.fn()} onShowList={vi.fn()} />)
    const box = within(card("LEG-409")).getByRole("group", { name: "Needs you" })
    expect(within(box).queryByRole("button", { name: "Allow" })).toBeNull()
    fireEvent.click(within(box).getByRole("button", { name: "Answer" }))
    expect(railState().wanted).toBe("ap-1")
    expect(onDecide).not.toHaveBeenCalled()
  })

  it("shows a terminal agent's terminal, and says when it could not be read", async () => {
    apiFetch.mockImplementation(async (_baseUrl: string, path: string) => path.includes("/LEG-500/preview")
      ? new Response(JSON.stringify({ task_id: "LEG-500", content: "$ make build\nok  ./...\n", captured_at: "now" }), { status: 200 })
      : new Response("no", { status: 500 }))
    render(<AgentEnsemble agents={[
      agent("LEG-500", "implementing", "working", { activity: "working" }),
      agent("LEG-501", "implementing", "your_turn"),
    ]} onOpen={vi.fn()} onSpawn={vi.fn()} onShowList={vi.fn()} />)
    expect((await within(card("LEG-500")).findByLabelText("Now")).textContent).toBe("ok  ./...")
    expect(await within(card("LEG-501")).findByText("Could not read this agent's terminal.")).toBeTruthy()
  })

  it("offers a plan to review when that is what waits", () => {
    const onOpen = vi.fn()
    render(<AgentEnsemble agents={[native("LEG-412", "planning", "needs_you", { plan_pending: true })]} onOpen={onOpen} onSpawn={vi.fn()} onShowList={vi.fn()} />)
    const box = within(card("LEG-412")).getByRole("group", { name: "Needs you" })
    expect(within(box).getByText("Plan to approve")).toBeTruthy()
    fireEvent.click(within(box).getByRole("button", { name: "Review plan" }))
    expect(onOpen).toHaveBeenCalledWith("LEG-412")
  })

  it("opens a message box in place on Reply, and sends with Enter", () => {
    const onSend = vi.fn()
    render(<AgentEnsemble agents={[native("LEG-386", "implementing", "your_turn")]} onSend={onSend} onOpen={vi.fn()} onSpawn={vi.fn()} onShowList={vi.fn()} />)
    const shown = card("LEG-386")
    expect(within(shown).queryByRole("textbox")).toBeNull()
    fireEvent.click(within(shown).getByRole("button", { name: "Reply to LEG-386" }))
    const box = within(shown).getByRole("textbox", { name: "Reply to LEG-386" })
    fireEvent.change(box, { target: { value: "carry on" } })
    fireEvent.keyDown(box, { key: "Enter" })
    expect(onSend).toHaveBeenCalledWith("LEG-386", "carry on")
    expect(within(shown).getByRole("button", { name: "Open LEG-386" })).toBeTruthy()
  })

  it("lists Finished agents as one-line rows that open the agent", () => {
    const onOpen = vi.fn()
    render(<AgentEnsemble agents={[
      native("LEG-405", "finished", "finished", { stage_reason: "PR #212 merged" }),
      native("LEG-413", "finished", "finished", { stage_reason: "both bugs fixed", open_items: 1, open_done_items: 1 }),
    ]} onOpen={onOpen} onSpawn={vi.fn()} onShowList={vi.fn()} />)
    const band = screen.getByRole("region", { name: "Finished" })
    expect(within(band).queryByRole("article")).toBeNull()
    const rows = within(band).getAllByRole("button", { name: /^Open LEG-/ })
    expect(rows.map((row) => row.textContent)).toEqual(["LEG-405LEG-405 titlePR #212 merged", "LEG-413LEG-413 titleboth bugs fixed"])
    expect(within(rows[1]).getByLabelText("Unread: finished with a note for you")).toBeTruthy()
    expect(within(rows[0]).queryByLabelText(/Unread/)).toBeNull()
    fireEvent.click(rows[0])
    expect(onOpen).toHaveBeenCalledWith("LEG-405")
  })
})
