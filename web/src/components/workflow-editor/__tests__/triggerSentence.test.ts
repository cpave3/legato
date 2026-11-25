import { describe, expect, it } from "vitest"
import { KIND_TITLES, newTrigger, triggerKinds, triggerSentence } from "../triggerForms"
import { noOptions } from "../stepForms"

const plain = (parts: { text: string; strong?: boolean }[]) => parts.map(p => p.strong ? `**${p.text}**` : p.text).join("")

describe("a trigger as a sentence", () => {
  it("says which column a card enters, and the conditions", () => {
    expect(plain(triggerSentence("task.entered_column", { column: "Review" }))).toBe("A card enters **Review**")
    expect(plain(triggerSentence("task.entered_column", { column: "Review", conditions: { all: [{ field: "task.priority", equals: "high" }] } }))).toBe("A card enters **Review** · priority is **high**")
    expect(plain(triggerSentence("task.entered_column", { column: "Doing", conditions: { all: [
      { field: "task.metadata.triaged", exists: false },
      { any: [{ field: "event.from_column", equals: "Backlog" }, { field: "task.priority", in: ["high", "urgent"] }] },
      { not: { field: "task.title", not_equals: "x" } },
    ] } }))).toBe("A card enters **Doing** · triaged is not set · from_column is **Backlog** or priority is one of **high, urgent** · not (title is not **x**)")
    expect(plain(triggerSentence("task.entered_column", {}))).toBe("A card enters **a column**")
  })

  it("says when a schedule is due", () => {
    expect(plain(triggerSentence("schedule", { every: "weekdays", at: "09:00" }))).toBe("Every **weekday** at **09:00**")
    expect(plain(triggerSentence("schedule", { every: "daily", at: "18:30" }))).toBe("Every **day** at **18:30**")
    expect(plain(triggerSentence("schedule", { every: "weekly", at: "08:00", on: "monday" }))).toBe("Every **monday** at **08:00**")
    expect(plain(triggerSentence("schedule", { cron: "0 9 * * 1-5" }))).toBe("On the cron schedule **0 9 * * 1-5**")
  })

  it("says a pull request gets changes requested", () => {
    expect(plain(triggerSentence("pr.changes_requested", { enabled: false }))).toBe("A PR gets changes requested")
  })

  it("says which chat message starts a run", () => {
    expect(plain(triggerSentence("chat.message", { bot: "slack", channel: "C123", text: { contains: "deploy" } }))).toBe("A message in **C123** on **slack** · containing **deploy**")
    expect(plain(triggerSentence("chat.message", { bot: "slack" }))).toBe("A message on **slack**")
    expect(plain(triggerSentence("chat.message", { bot: "discord", user: "U9", text: { regex: "^ship (\\w+)" } }))).toBe("A message on **discord** from **U9** · matching **^ship (\\w+)**")
    expect(plain(triggerSentence("chat.message", {}))).toBe("A message on **a bot**")
  })

  it("says which mention of a bot starts a run", () => {
    expect(plain(triggerSentence("chat.mention", { bot: "discord" }))).toBe("A mention of **discord**")
    expect(plain(triggerSentence("chat.mention", { bot: "discord", channel: "C1", text: { contains: "help" } }))).toBe("A mention of **discord** in **C1** · containing **help**")
    expect(plain(triggerSentence("chat.mention", { bot: "slack", text: { regex: "investigat", not_regex: "fix" } }))).toBe("A mention of **slack** · matching **investigat** · not matching **fix**")
    expect(plain(triggerSentence("chat.message", { bot: "slack", text: { not_contains: "lunch" } }))).toBe("A message on **slack** · not containing **lunch**")
    expect(plain(triggerSentence("chat.mention", { bot: "slack", fallback: true }))).toBe("A mention of **slack** · when no other trigger matched")
  })

  it("names a kind it doesn't know", () => {
    expect(plain(triggerSentence("something.new", {}))).toBe("**something.new**")
  })
})

describe("a new trigger", () => {
  it("starts a chat trigger paused, on the first bot offered", () => {
    expect(newTrigger("chat.message", { ...noOptions, bots: ["discord", "slack"] })).toEqual({ bot: "discord", enabled: false })
    expect(newTrigger("chat.mention", { ...noOptions, bots: ["slack"] })).toEqual({ bot: "slack", enabled: false })
    expect(newTrigger("chat.mention", noOptions)).toEqual({ bot: "", enabled: false })
  })

  it("has a title for every kind in the schema", () => {
    for (const { kind } of triggerKinds) expect(KIND_TITLES[kind], kind).toBeTruthy()
    expect(KIND_TITLES["chat.message"]).toBe("When a chat message arrives")
    expect(KIND_TITLES["chat.mention"]).toBe("When the bot is mentioned")
  })
})
