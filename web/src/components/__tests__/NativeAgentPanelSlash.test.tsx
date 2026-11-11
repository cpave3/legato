import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { NativeAgentPanel } from "../NativeAgentPanel"

const send = vi.fn()

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

type Command = { name: string; description?: string; body: string }
type Skill = { name: string; description: string }

function serve(commands: Command[], skills: Skill[]) {
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const path = String(input)
    if (path.includes("/snapshot")) {
      return new Response(JSON.stringify({ id: "session-1", status: "idle", messages: [], toolCalls: [] }), { status: 200 })
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
  })

  afterEach(() => {
    cleanup()
    vi.restoreAllMocks()
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
    fireEvent.mouseDown(screen.getByRole("option", { name: /standup/ }))
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
