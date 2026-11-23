import { describe, expect, it } from "vitest"
import { getTextWidth } from "@evenrealities/pretext"
import { CHAT_WIDTH, heard, reeveChat, reeveDictation } from "./reeve"
import type { Agent, Conversation } from "./legato"

const reeve = (activity: string): Agent => ({
  task_id: "rv1",
  title: "The Reeve",
  status: "running",
  activity,
  sandboxed: true,
  started_at: "2026-11-23T07:00:00Z",
  reeve: true,
})

const read = (entries: Conversation["entries"], fields: Partial<Conversation> = {}) => ({
  ok: true as const,
  conversation: { task_id: "rv1", status: "running", entries, permission: null, question: null, context_used: 0, context_window: 0, ...fields },
})

const hint = (text: string) => text.trim()

describe("reeveChat", () => {
  it("shows the newest exchange: what the operator said, dim on its own line, and the Reeve's reply", () => {
    const page = reeveChat(
      reeve(""),
      read([
        { kind: "user", text: "[legato glasses] how many agents are working?" },
        { kind: "assistant", text: "Three. LEG-212 waits on a build." },
        { kind: "user", text: "[legato glasses] start one on LEG-230" },
        { kind: "tool", text: "Start an agent", tool: "mcp__legato__legato_api", state: "done" },
        { kind: "assistant", text: "Started LEG-230 in the sandbox." },
        { kind: "assistant", text: "I'll tell you when it waits." },
      ]),
      { instance: "Desk" },
    )

    expect(page).toMatchObject({
      badge: "○ ready",
      title: "Reeve · Desk",
      asked: "You: start one on LEG-230",
      reply: "Started LEG-230 in the sandbox.\n\nI'll tell you when it waits.",
    })
    expect(hint(page.hint)).toBe("Hold: talk · Tap: history · Double tap: back")
  })

  it("says it is thinking, and what it is doing, until it replies", () => {
    const page = reeveChat(
      reeve("working"),
      read([
        { kind: "user", text: "[legato glasses] start one on LEG-230" },
        { kind: "tool", text: "GET /api/tasks/LEG-230", tool: "mcp__legato__legato_api", state: "running" },
      ]),
      { instance: "Desk" },
    )

    expect(page.badge).toBe("▶ working")
    expect(page.reply).toBe("Thinking…\n\n▶ legato_api: GET /api/tasks/LEG-230")
  })

  it("shows a message just sent, before the conversation has it", () => {
    const page = reeveChat(reeve(""), read([{ kind: "user", text: "older" }, { kind: "assistant", text: "Done." }]), {
      instance: "Desk",
      pending: "start one on LEG-230",
    })

    expect(page.asked).toBe("You: start one on LEG-230")
    expect(page.reply).toBe("Thinking…")
  })

  it("names Legato's own messages, which the Reeve also answers", () => {
    const page = reeveChat(
      reeve(""),
      read([
        { kind: "user", text: "[legato agent] LEG-230 is waiting" },
        { kind: "assistant", text: "LEG-230 asks to push its branch." },
      ]),
      { instance: "Desk" },
    )

    expect(page.asked).toBe("Legato agent: LEG-230 is waiting")
  })

  it("leaves out the snapshot a resumed Reeve was given with the message", () => {
    const page = reeveChat(
      reeve(""),
      read([{ kind: "user", text: "[legato glasses] status?\n\n[legato] since you were last here:\n\n## This instance…" }]),
      { instance: "Desk" },
    )

    expect(page.asked).toBe("You: status?")
  })

  it("puts what the Reeve asks the operator first", () => {
    const page = reeveChat(reeve("waiting"), read([{ kind: "user", text: "hi" }], { question: { text: "Which repo?", options: ["legato", "site"] } }), {
      instance: "Desk",
    })

    expect(page.badge).toBe("● needs you")
    expect(page.reply).toBe("Asks: Which repo?\n1. legato\n2. site")
  })

  it("strips the Markdown the glasses can't show", () => {
    const page = reeveChat(
      reeve(""),
      read([
        { kind: "user", text: "hi" },
        { kind: "assistant", text: "## Done\n\n**LEG-230** runs in `~/src/legato`.\n- one\n* two\nSee [the PR](https://x/1)." },
      ]),
      { instance: "Desk" },
    )

    expect(page.reply).toBe("Done\n\nLEG-230 runs in ~/src/legato.\n• one\n• two\nSee the PR.")
  })

  it("cuts the operator's line to the frame's width", () => {
    const long = "start an agent on every ticket in the milestone that is ready, oldest first, and watch them"
    const page = reeveChat(reeve(""), read([{ kind: "user", text: `[legato glasses] ${long}` }]), { instance: "Desk" })

    expect(page.asked.endsWith("…")).toBe(true)
    expect(getTextWidth(page.asked)).toBeLessThanOrEqual(CHAT_WIDTH)
  })

  it("says how to wake the Reeve when it is not running", () => {
    expect(reeveChat(undefined, undefined, { instance: "Desk" })).toMatchObject({
      badge: "○ asleep",
      asked: "",
      reply: "The Reeve is asleep. Hold to talk and it wakes.",
    })
  })

  it("invites a first message, and says what went wrong reading", () => {
    expect(reeveChat(reeve(""), read([]), { instance: "Desk" }).reply).toBe("Hold to talk to the Reeve.")
    expect(reeveChat(reeve(""), { ok: false, problem: "Can't reach Legato" }, { instance: "Desk" }).reply).toBe("Can't reach Legato")
    expect(reeveChat(reeve(""), undefined, { instance: "Desk" }).reply).toBe("Reading…")
  })
})

describe("heard", () => {
  it("says whether the conversation has the message sent", () => {
    const conv = read([{ kind: "user", text: "[legato glasses] start one" }])

    expect(heard(conv, "start one")).toBe(true)
    expect(heard(conv, "something else")).toBe(false)
    expect(heard(undefined, "start one")).toBe(false)
  })
})

describe("reeveDictation", () => {
  it("speaks of the Reeve while dictating to it", () => {
    expect(reeveDictation("Desk", { step: "listening", seconds: 3 })).toMatchObject({
      badge: "● Listening",
      title: "Reeve · Desk",
      body: "Speak to the Reeve.\n\n0:03",
    })
    expect(reeveDictation("Desk", { step: "sent" }).body).toBe("Sent to the Reeve.")
  })
})
