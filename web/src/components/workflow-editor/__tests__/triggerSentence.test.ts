import { describe, expect, it } from "vitest"
import { triggerSentence } from "../triggerForms"

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

  it("names a kind it doesn't know", () => {
    expect(plain(triggerSentence("something.new", {}))).toBe("**something.new**")
  })
})
