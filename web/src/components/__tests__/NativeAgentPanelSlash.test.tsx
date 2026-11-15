import "@testing-library/jest-dom/vitest"
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { NativeAgentPanel } from "../NativeAgentPanel"

// The global voice sheet is outside the composer interaction under test.
vi.mock("../../voice/VoiceLauncher", () => ({ VoiceLauncher: () => null }))

const send = vi.fn()
let serverUrl = ""

vi.mock("../../hooks/useWebSocket", () => ({
  useWebSocket: () => ({
    connected: true,
    send,
    subscribe: () => () => {},
  }),
}))

vi.mock("../../hooks/useServer", () => ({
  useServer: () => ({ baseUrl: serverUrl }),
}))

type Command = { name: string; description?: string; body: string }
type Skill = { name: string; description: string }

function serve(commands: Command[], skills: Skill[], status = "idle") {
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const path = String(input)
    if (path.includes("/snapshot")) {
      return new Response(JSON.stringify({ id: "session-1", status, messages: [], toolCalls: [] }), { status: 200 })
    }
    if (path.endsWith("/chimera/commands")) {
      return new Response(JSON.stringify(commands.map((c) => ({ ...c, path: `/c/${c.name}.md`, source: "user" }))), { status: 200 })
    }
    if (path.endsWith("/chimera/skills")) {
      return new Response(JSON.stringify(skills.map((s) => ({ ...s, path: `/s/${s.name}`, source: "user" }))), { status: 200 })
    }
    return new Response("[]", { status: 200 })
  })
}

const skill = (name: string, description = `${name} skill`): Skill => ({ name, description })

// Renders the panel and waits until typing "/" lists the given name, so the
// commands and skills have loaded.
async function openPanel(ready: string) {
  render(<NativeAgentPanel agentId="agent-1" />)
  const box = await screen.findByRole("textbox", { name: "Message" }) as HTMLTextAreaElement
  fireEvent.change(box, { target: { value: "/" } })
  await screen.findAllByRole("option", { name: new RegExp(ready) })
  return box
}

function type(box: HTMLTextAreaElement, value: string) {
  fireEvent.change(box, { target: { value } })
}

function optionNames(): string[] {
  return within(screen.getByRole("listbox")).getAllByRole("option").map((o) => o.getAttribute("data-name") ?? "")
}

function sentMessages(): string[] {
  return send.mock.calls.map(([message]) => message).filter((m) => m.type === "agent_message").map((m) => m.content)
}

function highlighted(): string | null {
  const active = within(screen.getByRole("listbox")).getAllByRole("option").find((o) => o.getAttribute("aria-selected") === "true")
  return active?.getAttribute("data-name") ?? null
}

describe("NativeAgentPanel slash menu", () => {
  beforeEach(() => {
    send.mockReset()
    serverUrl = ""
  })

  afterEach(() => {
    cleanup()
    vi.restoreAllMocks()
  })

  it("browses commands and skills without replacing the draft or sending", async () => {
    serve([{ name: "standup", description: "Write a full account of today’s work and tomorrow’s plans.", body: "Write my standup notes" }], [skill("plugin:review", "Review the entire change, including tests and documentation.")])
    render(<NativeAgentPanel agentId="agent-1" />)
    const box = await screen.findByRole("textbox", { name: "Message" }) as HTMLTextAreaElement
    type(box, "Keep these notes")
    fireEvent.click(screen.getByRole("button", { name: "Commands and skills" }))
    const search = screen.getByRole("combobox", { name: "Search commands and skills" })
    expect(box.value).toBe("Keep these notes")
    await screen.findByRole("option", { name: /standup/ })
    fireEvent.click(screen.getByRole("button", { name: "Skills" }))
    expect(optionNames()).toEqual(["plugin:review"])
    expect(screen.getByText("Review the entire change, including tests and documentation.")).toBeVisible()
    expect(screen.getByText(/Source: user.*Plugin: plugin/)).toBeVisible()
    fireEvent.click(screen.getByRole("button", { name: "Commands" }))
    fireEvent.change(search, { target: { value: "stand" } })
    expect(optionNames()).toEqual(["standup"])
    fireEvent.click(screen.getByRole("option", { name: /standup/ }))
    expect(box.value).toBe("Keep these notes\nWrite my standup notes")
    expect(screen.queryByRole("listbox")).toBeNull()
    expect(box).toHaveFocus()
    expect(sentMessages()).toEqual([])
  })

  it("keeps a browsed skill invocation at the start of the preserved draft", async () => {
    serve([], [skill("review")])
    render(<NativeAgentPanel agentId="agent-1" />)
    const box = await screen.findByRole("textbox", { name: "Message" }) as HTMLTextAreaElement
    type(box, "Review the current branch")
    fireEvent.click(screen.getByRole("button", { name: "Commands and skills" }))
    fireEvent.click(await screen.findByRole("option", { name: /review/ }))
    expect(box.value).toBe("/review Review the current branch")
    expect(sentMessages()).toEqual([])
  })

  it("retains successful catalogs during a partial failure and retry", async () => {
    serve([{ name: "standup", body: "Write notes" }], [])
    const fallback = vi.mocked(fetch).getMockImplementation()!
    let recover: ((response: Response) => void) | undefined
    let attempts = 0
    vi.mocked(fetch).mockImplementation((input, init) => {
      if (String(input).endsWith("/skills")) {
        attempts++
        return attempts === 1 ? Promise.resolve(new Response("offline", { status: 503 })) : new Promise((resolve) => { recover = resolve })
      }
      return fallback(input, init)
    })
    render(<NativeAgentPanel agentId="agent-1" />)
    fireEvent.click(await screen.findByRole("button", { name: "Commands and skills" }))
    expect(await screen.findByRole("alert")).toHaveTextContent(/Skills unavailable/)
    expect(optionNames()).toEqual(["standup"])
    fireEvent.click(screen.getByRole("button", { name: "Retry" }))
    expect(screen.getByRole("status")).toHaveTextContent(/Loading/)
    expect(optionNames()).toEqual(["standup"])
    await act(async () => { recover!(new Response(JSON.stringify([skill("review")]), { status: 200 })) })
    await screen.findByRole("option", { name: /review/ })
    expect(screen.queryByRole("alert")).toBeNull()
    expect(optionNames()).toContain("standup")
    expect(sentMessages()).toEqual([])
  })

  it.each(["agent", "server", "unmount"])("clears catalogs on %s switch and ignores late responses", async (changed) => {
    serve([], [])
    const fallback = vi.mocked(fetch).getMockImplementation()!
    let oldSkills: ((response: Response) => void) | undefined
    vi.mocked(fetch).mockImplementation((input, init) => {
      const path = String(input)
      const current = changed !== "server" ? path.includes("agent-2") : path.startsWith("https://second.example")
      if (path.endsWith("/skills")) return current
        ? Promise.resolve(new Response(JSON.stringify([skill("new-skill")]), { status: 200 }))
        : new Promise((resolve) => { oldSkills = resolve })
      if (path.endsWith("/commands")) return Promise.resolve(new Response(JSON.stringify(current ? [] : [{ name: "old-command", body: "old" }]), { status: 200 }))
      return fallback(input, init)
    })
    const panel = render(<NativeAgentPanel agentId="agent-1" />)
    fireEvent.click(await screen.findByRole("button", { name: "Commands and skills" }))
    await screen.findByRole("option", { name: /old-command/ })
    if (changed === "server") serverUrl = "https://second.example"
    if (changed === "unmount") {
      panel.unmount()
      render(<NativeAgentPanel agentId="agent-2" />)
    } else panel.rerender(<NativeAgentPanel agentId={changed === "agent" ? "agent-2" : "agent-1"} />)
    expect(screen.queryByRole("option", { name: /old-command/ })).toBeNull()
    fireEvent.click(await screen.findByRole("button", { name: "Commands and skills" }))
    await screen.findByRole("option", { name: /new-skill/ })
    await act(async () => { oldSkills!(new Response(JSON.stringify([skill("stale-skill")]), { status: 200 })) })
    expect(optionNames()).toEqual(["new-skill"])
    panel.unmount()
  })

  it("consumes Enter and Escape with no matches and does not complete during IME", async () => {
    serve([], [skill("review")], "running")
    const box = await openPanel("review")
    type(box, "/rev")
    fireEvent.keyDown(box, { key: "Enter", isComposing: true })
    expect(box.value).toBe("/rev")
    expect(sentMessages()).toEqual([])
    type(box, "/missing")
    fireEvent.keyDown(box, { key: "Enter" })
    expect(box.value).toBe("/missing")
    expect(sentMessages()).toEqual([])
    fireEvent.keyDown(box, { key: "Escape" })
    expect(screen.queryByRole("listbox")).toBeNull()
    expect(send.mock.calls.some(([message]) => message.type === "agent_interrupt")).toBe(false)
    fireEvent.click(screen.getByRole("button", { name: "Commands and skills" }))
    const search = screen.getByRole("combobox", { name: "Search commands and skills" })
    fireEvent.keyDown(search, { key: "Escape" })
    expect(box.value).toBe("/missing")
    expect(box).toHaveFocus()
    expect(screen.queryByRole("listbox")).toBeNull()
  })

  it("distinguishes an empty catalog from a search with no matches", async () => {
    serve([], [])
    render(<NativeAgentPanel agentId="agent-1" />)
    fireEvent.click(await screen.findByRole("button", { name: "Commands and skills" }))
    expect(await screen.findByText("No commands or skills available for this agent")).toBeVisible()
    expect(screen.queryByRole("alert")).toBeNull()
  })

  it.each(["Tab", "Enter"])("%s chooses a browsed skill and keeps the existing draft", async (key) => {
    serve([], [skill("review")])
    render(<NativeAgentPanel agentId="agent-1" />)
    const box = await screen.findByRole("textbox", { name: "Message" }) as HTMLTextAreaElement
    type(box, "Check the tests")
    fireEvent.click(screen.getByRole("button", { name: "Commands and skills" }))
    await screen.findByRole("option", { name: /review/ })
    const search = screen.getByRole("combobox", { name: "Search commands and skills" })
    fireEvent.change(search, { target: { value: "rev" } })
    fireEvent.keyDown(search, { key })
    expect(box.value).toBe("/review Check the tests")
    expect(sentMessages()).toEqual([])
  })

  it("shows unavailable catalogs with Retry instead of reporting them as empty", async () => {
    serve([], [])
    const fallback = vi.mocked(fetch).getMockImplementation()!
    let failed = true
    vi.mocked(fetch).mockImplementation((input, init) =>
      failed && /\/(commands|skills)$/.test(String(input))
        ? Promise.reject(new Error("Network offline")) : fallback(input, init))
    render(<NativeAgentPanel agentId="agent-1" />)
    fireEvent.click(await screen.findByRole("button", { name: "Commands and skills" }))
    expect(await screen.findByRole("alert")).toHaveTextContent("Network offline")
    expect(screen.getByText("Catalog unavailable")).toBeVisible()
    failed = false
    fireEvent.click(screen.getByRole("button", { name: "Retry" }))
    expect(await screen.findByText("No commands or skills available for this agent")).toBeVisible()
    expect(screen.queryByRole("alert")).toBeNull()
  })

  it("puts a prefix match above a name that only contains the query", async () => {
    serve([], [skill("create-skill"), skill("address-stack-feedback"), skill("review")])
    const box = await openPanel("review")
    type(box, "/re")
    expect(optionNames()[0]).toBe("review")
    expect(optionNames().indexOf("review")).toBeLessThan(optionNames().indexOf("create-skill"))
  })

  it("ranks an exact name, then a prefix, then a word start, then the middle of a word, commands and skills together", async () => {
    serve(
      [{ name: "previewer", body: "Preview it" }, { name: "review-pr", body: "Review the PR" }],
      [skill("codereviewer"), skill("plugin:review"), skill("review-briefing"), skill("review")],
    )
    const box = await openPanel("review")
    type(box, "/review")
    expect(optionNames()).toEqual(["review", "review-pr", "review-briefing", "plugin:review", "previewer", "codereviewer"])
  })

  it("drops names whose letters match only scattered through the middle of words", async () => {
    serve([], [skill("review"), skill("create-workflow")])
    const box = await openPanel("review")
    type(box, "/rw")
    expect(optionNames()).toEqual(["review"])
  })

  it("says nothing matches instead of vanishing", async () => {
    serve([], [skill("review")])
    const box = await openPanel("review")
    type(box, "/zzz")
    expect(within(screen.getByRole("listbox")).getByText("No matching commands or skills")).not.toBeNull()
  })

  it.each(["Tab", "Enter"])("%s completes the highlighted skill without sending, and the person keeps typing", async (key) => {
    serve([], [skill("review"), skill("rebase")])
    const box = await openPanel("review")
    type(box, "/rev")
    fireEvent.keyDown(box, { key })

    expect(box.value).toBe("/review ")
    expect(box.selectionStart).toBe(box.value.length)
    expect(screen.queryByRole("listbox")).toBeNull()
    expect(sentMessages()).toEqual([])

    type(box, "/review the stack")
    expect(screen.queryByRole("listbox")).toBeNull()
    fireEvent.keyDown(box, { key: "Enter" })
    expect(sentMessages()).toEqual(["/review the stack"])
  })

  it("expands a command with a body into the box and does not send it, by key or by click", async () => {
    serve([{ name: "standup", description: "Daily notes", body: "Write my standup notes" }], [skill("review")])
    const box = await openPanel("standup")
    type(box, "/stand")
    fireEvent.keyDown(box, { key: "Tab" })
    expect(box.value).toBe("Write my standup notes")
    expect(sentMessages()).toEqual([])

    type(box, "/stand")
    fireEvent.click(screen.getByRole("option", { name: /standup/ }))
    expect(box.value).toBe("Write my standup notes")
    expect(sentMessages()).toEqual([])
  })

  it("moves the highlight with the arrows, wraps at the ends, and keeps the row in view", async () => {
    const scrolled: string[] = []
    const scrollIntoView = vi.fn(function (this: HTMLElement) { scrolled.push(this.getAttribute("data-name") ?? "") })
    Object.defineProperty(HTMLElement.prototype, "scrollIntoView", { configurable: true, value: scrollIntoView })
    serve([], [skill("alpha"), skill("bravo"), skill("charlie")])
    const box = await openPanel("charlie")
    type(box, "/")
    expect(highlighted()).toBe("alpha")

    fireEvent.keyDown(box, { key: "ArrowDown" })
    fireEvent.keyDown(box, { key: "ArrowDown" })
    expect(highlighted()).toBe("charlie")
    expect(scrolled.at(-1)).toBe("charlie")
    expect(box.getAttribute("aria-activedescendant")).toBe(screen.getByRole("option", { name: /charlie/ }).id)

    fireEvent.keyDown(box, { key: "ArrowDown" })
    expect(highlighted()).toBe("alpha")
    fireEvent.keyDown(box, { key: "ArrowUp" })
    expect(highlighted()).toBe("charlie")
    // @ts-expect-error removing the stub put on for this test
    delete HTMLElement.prototype.scrollIntoView
  })

  it("keeps the highlight while the list stands, and resets it when the list changes", async () => {
    serve([], [skill("review"), skill("rebase")])
    const box = await openPanel("review")
    type(box, "/r")
    fireEvent.keyDown(box, { key: "ArrowDown" })
    expect(highlighted()).toBe("review")

    type(box, "/re")
    expect(highlighted()).toBe("review")
    type(box, "/reb")
    expect(highlighted()).toBe("rebase")
  })

  it("closes on Escape keeping the draft, and reopens when the person types", async () => {
    serve([], [skill("review")])
    const box = await openPanel("review")
    type(box, "/rev")
    fireEvent.keyDown(box, { key: "Escape" })
    expect(screen.queryByRole("listbox")).toBeNull()
    expect(box.value).toBe("/rev")

    type(box, "/revi")
    await waitFor(() => expect(screen.getByRole("listbox")).not.toBeNull())
    expect(optionNames()).toEqual(["review"])
  })
})
