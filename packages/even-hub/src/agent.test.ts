import { describe, expect, it } from "vitest"
import { getTextWidth } from "@evenrealities/pretext"
import { agentPage, dictationPage, TITLE_TEXT_WIDTH } from "./agent"
import { TEXT_UPDATE_LIMIT } from "./glance"
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

describe("agentPage", () => {
  it("titles the page with the agent's state, task and title, cut to fit", () => {
    const page = agentPage(agent, { ok: true, conversation: conversation({}) })

    expect(page.title.startsWith("● needs you  LEG-163 POC")).toBe(true)
    expect(getTextWidth(page.title)).toBeLessThanOrEqual(TITLE_TEXT_WIDTH)
  })

  it("shows what the agent asks for first, then its conversation, newest first", () => {
    const page = agentPage(agent, {
      ok: true,
      conversation: conversation({
        entries: [
          { kind: "user", text: "Add tabs to the glasses app." },
          { kind: "tool", text: "Run the tests", tool: "Bash", command: "pnpm test", state: "failed" },
          { kind: "assistant", text: "The tests fail on the tab labels.\nI'll fix them." },
        ],
        permission: { tool: "Bash", command: "git push", reason: "Push the branch" },
      }),
    })

    expect(page.body).toBe(
      [
        "● Asks to use Bash: git push",
        "Push the branch",
        "",
        "The tests fail on the tab labels. I'll fix them.",
        "",
        "▲ Bash: Run the tests",
        "",
        "You: Add tabs to the glasses app.",
      ].join("\n"),
    )
    expect(page.hint).toBe("Tap: dictate a message · Double tap: back")
  })

  it("names an MCP tool by its own name, without its server's prefix", () => {
    const page = agentPage(agent, {
      ok: true,
      conversation: conversation({
        entries: [{ kind: "tool", text: "", tool: "mcp__plugin_legato-tools_legato__request_access", state: "done" }],
      }),
    })

    expect(page.body).toBe("› request_access")
  })

  it("shows a question with its options", () => {
    const page = agentPage(agent, {
      ok: true,
      conversation: conversation({ question: { text: "Which layout?", options: ["Tabs", "One list"] } }),
    })

    expect(page.body).toBe("● Asks: Which layout?\n  1. Tabs\n  2. One list")
  })

  it("says why there is no conversation to show", () => {
    const page = agentPage(agent, { ok: false, problem: "The agent is no longer running." })

    expect(page.body).toBe("The agent is no longer running.")
  })

  it("keeps the newest of a long conversation within one text update", () => {
    const entries = Array.from({ length: 40 }, (_, i) => ({ kind: "assistant", text: `Entry ${i} `.padEnd(200, "x") }))
    const body = agentPage(agent, { ok: true, conversation: conversation({ entries }) }).body

    expect(body.length).toBeLessThanOrEqual(TEXT_UPDATE_LIMIT)
    expect(body.startsWith("Entry 39 ")).toBe(true)
  })

  it("cuts one long message rather than let it fill the page", () => {
    const body = agentPage(agent, {
      ok: true,
      conversation: conversation({ entries: [{ kind: "assistant", text: "word ".repeat(400) }] }),
    }).body

    expect(body.length).toBeLessThanOrEqual(400)
    expect(body.endsWith("…")).toBe(true)
  })

  it("says it is reading before the first answer", () => {
    expect(agentPage(agent, undefined).body).toBe("Reading the conversation…")
  })
})

describe("dictationPage", () => {
  it("says it is listening, for how long, and how to stop", () => {
    expect(dictationPage(agent, { step: "listening", seconds: 65 })).toEqual({
      title: "● Listening · LEG-163",
      body: "Speak your message to the agent.\n\n1:05",
      hint: "Tap: stop · Double tap: cancel",
    })
  })

  it("shows what it heard before sending it", () => {
    expect(dictationPage(agent, { step: "review", text: "Run the tests again." })).toMatchObject({
      body: "“Run the tests again.”",
      hint: "Tap: send · Double tap: cancel",
    })
  })

  it("says what went wrong and how to go on", () => {
    expect(dictationPage(agent, { step: "problem", problem: "Heard nothing." })).toMatchObject({
      body: "Heard nothing.",
      hint: "Tap: try again · Double tap: back",
    })
  })

  it("says the message went", () => {
    expect(dictationPage(agent, { step: "sent" }).body).toBe("Sent to LEG-163.")
  })
})
