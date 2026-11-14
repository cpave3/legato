import { describe, expect, it } from "vitest"
import { getTextWidth } from "@evenrealities/pretext"
import { agentPage, dictationPage, entryPage } from "./agent"
import { CANVAS_WIDTH, ITEM_TEXT_WIDTH, TITLE_WIDTH, center, inner } from "./layout"
import { LIST_ITEM_BYTES, LIST_ITEMS, TEXT_UPDATE_LIMIT } from "./glance"
import type { Agent, Conversation } from "./legato"

const agent: Agent = {
  task_id: "LEG-163",
  title: "POC: an Even Realities G2 glasses app that shows the agents working now",
  status: "running",
  activity: "waiting",
  sandboxed: true,
  started_at: "2026-11-13T07:24:57Z",
}

const conversation = (fields: Partial<Conversation>): Conversation => ({
  task_id: "LEG-163",
  status: "running",
  entries: [],
  permission: null,
  question: null,
  context_used: 0,
  context_window: 0,
  ...fields,
})

const texts = (items: { text: string }[]) => items.map((i) => i.text)
const utf8 = (s: string) => new TextEncoder().encode(s).length
const hintWidth = CANVAS_WIDTH

describe("agentPage", () => {
  it("puts the agent's state in a badge of its own, apart from its task and title", () => {
    const page = agentPage(agent, { ok: true, conversation: conversation({}) })

    expect(page.badge).toBe("● needs you")
    expect(page.title.startsWith("LEG-163 POC: an Even Realities")).toBe(true)
    expect(getTextWidth(page.title)).toBeLessThanOrEqual(inner(TITLE_WIDTH))
  })

  it("lists what the agent asks for, then what it said and did, newest first, one line each", () => {
    const page = agentPage(agent, {
      ok: true,
      conversation: conversation({
        entries: [
          { kind: "user", text: "Add tabs to the glasses app." },
          { kind: "tool", text: "Run the tests", tool: "Bash", command: "pnpm test", state: "failed" },
          { kind: "assistant", text: "The tests fail on the tab labels.\nI'll fix them." },
          { kind: "tool", text: "", tool: "mcp__plugin_legato-tools_legato__request_access", state: "done" },
        ],
        permission: { tool: "Bash", command: "git push", reason: "Push the branch" },
      }),
    })

    expect(texts(page.items)).toEqual([
      "● Asks: Bash git push",
      "· request_access",
      "Agent: The tests fail on the tab labels. I'll fix them.",
      "▲ Bash: Run the tests",
      "You: Add tabs to the glasses app.",
    ])
  })

  it("keeps every line to the list's width and bytes, and the list to its twenty items", () => {
    const long = "word ".repeat(80)
    const entries = Array.from({ length: 30 }, () => ({ kind: "assistant", text: long }))
    const page = agentPage(agent, { ok: true, conversation: conversation({ entries }) })

    expect(page.items.length).toBeLessThanOrEqual(LIST_ITEMS)
    for (const item of page.items) {
      expect(getTextWidth(item.text)).toBeLessThanOrEqual(ITEM_TEXT_WIDTH)
      expect(utf8(item.text)).toBeLessThanOrEqual(LIST_ITEM_BYTES)
    }
  })

  it("shows what Legato or another agent sent as theirs, not the operator's", () => {
    const page = agentPage(agent, {
      ok: true,
      conversation: conversation({
        entries: [
          { kind: "user", text: "Fix the tabs." },
          { kind: "user", text: "[legato interrupted] Legato restarted while you worked." },
          { kind: "user", text: "[legato peer LEG-150] I changed glance.ts too.\n\n---\nAgent-to-agent message: reply with legato agent message" },
          { kind: "user", text: "[legato] Your swarm inbox has 2 messages." },
        ],
      }),
    })

    expect(texts(page.items)).toEqual([
      "Legato: Your swarm inbox has 2 messages.",
      "Agent LEG-150: I changed glance.ts too.",
      "Legato interrupted: Legato restarted while you worked.",
      "You: Fix the tabs.",
    ])
  })

  it("keeps each entry whole for when it is opened", () => {
    const page = agentPage(agent, {
      ok: true,
      conversation: conversation({
        entries: [{ kind: "tool", text: "Run the tests", tool: "Bash", command: "pnpm test", state: "failed" }],
        question: { text: "Which layout?", options: ["Tabs", "One list"] },
      }),
    })

    expect(page.items.map((i) => i.detail)).toEqual([
      "Asks: Which layout?\n\n1. Tabs\n2. One list",
      "Bash (failed)\n\nRun the tests\n\npnpm test",
    ])
  })

  it("says why there is nothing to list, as its only line", () => {
    expect(texts(agentPage(agent, { ok: false, problem: "The agent is no longer running." }).items)).toEqual([
      "The agent is no longer running.",
    ])
    expect(texts(agentPage(agent, undefined).items)).toEqual(["Reading the conversation…"])
    expect(texts(agentPage(agent, { ok: true, conversation: conversation({}) }).items)).toEqual(["Nothing said yet."])
  })

  it("centres what the touchpad does under the list", () => {
    const { hint } = agentPage(agent, undefined)

    expect(hint.trim()).toBe("Tap: expand · Hold: dictate · Double tap: back")
    expect(hint.startsWith("   ")).toBe(true)
    expect(getTextWidth(hint)).toBeLessThanOrEqual(hintWidth)
  })
})

describe("entryPage", () => {
  it("shows one entry in full under the same badge and title", () => {
    const page = entryPage(agent, { text: "Agent: The tests…", detail: "Agent\n\nThe tests fail on the tab labels." })

    expect(page).toMatchObject({ badge: "● needs you", body: "Agent\n\nThe tests fail on the tab labels." })
    expect(page.hint.trim()).toBe("Hold: dictate · Double tap: back")
  })

  it("cuts an entry to what one text update carries", () => {
    const page = entryPage(agent, { text: "x", detail: "y".repeat(3000) })

    expect(page.body.length).toBeLessThanOrEqual(TEXT_UPDATE_LIMIT)
    expect(page.body.endsWith("…")).toBe(true)
  })
})

describe("dictationPage", () => {
  it("says it is listening in the badge, for how long, and how to stop", () => {
    const page = dictationPage(agent, { step: "listening", seconds: 65 })

    expect(page.badge).toBe("● Listening")
    expect(page.body).toBe("Speak your message to the agent.\n\n1:05")
    expect(page.hint.trim()).toBe("Release: stop · Double tap: cancel")
  })

  it("shows what it heard before sending it", () => {
    expect(dictationPage(agent, { step: "review", text: "Run the tests again." })).toMatchObject({
      badge: "Message",
      body: "“Run the tests again.”",
    })
    expect(dictationPage(agent, { step: "review", text: "x" }).hint.trim()).toBe("Tap: send · Double tap: drop it")
  })

  it("keeps a message that failed to send, to send again", () => {
    const page = dictationPage(agent, { step: "problem", problem: "The agent is busy.", text: "Run the tests." })

    expect(page).toMatchObject({ badge: "▲ Not sent", body: "The agent is busy.\n\n“Run the tests.”" })
    expect(page.hint.trim()).toBe("Tap: send again · Double tap: drop it")
  })

  it("says what went wrong and how to go on", () => {
    const page = dictationPage(agent, { step: "problem", problem: "Heard nothing." })

    expect(page).toMatchObject({ badge: "▲ Problem", body: "Heard nothing." })
    expect(page.hint.trim()).toBe("Hold: try again · Double tap: back")
  })

  it("says the message went", () => {
    expect(dictationPage(agent, { step: "sent" })).toMatchObject({ badge: "Sent", body: "Sent to LEG-163." })
  })
})

describe("center", () => {
  it("pads text on the left so it sits in the middle of the width", () => {
    const text = "Tap: send"
    const centred = center(text, 400)
    const left = getTextWidth(centred) - getTextWidth(text)
    const right = 400 - getTextWidth(centred)

    expect(Math.abs(left - right)).toBeLessThanOrEqual(2 * getTextWidth(" "))
  })
})
