import { describe, expect, it } from "vitest"
import { parseReeveMessage, reeveBrief } from "../reeve"

describe("reeveBrief", () => {
  it("says what the person asks about, its IDs and its error line", () => {
    const brief = reeveBrief({ what: "the Attention item “Sync failed for Jira”", ids: { "Attention item": "att-1", task: "LEG-3" }, error: "Jira answered 401" })
    expect(brief).toBe([
      "The person asks you about the Attention item “Sync failed for Jira”.",
      "- Attention item: att-1",
      "- task: LEG-3",
      "- Error: Jira answered 401",
      "",
      "Find out what is wrong and what to do about it. Read first; any change goes to the person as a change set.",
    ].join("\n"))
  })

  it("leaves out what it was not given", () => {
    expect(reeveBrief({ what: "agent T-1" })).toBe("The person asks you about agent T-1.\n\nFind out what is wrong and what to do about it. Read first; any change goes to the person as a change set.")
  })
})

describe("parseReeveMessage", () => {
  it("reads another Reeve's message: its instance, role and words", () => {
    expect(parseReeveMessage("[from the Reeve on vm-1 · worker] all well\nhere")).toEqual({ instance: "vm-1", role: "worker", body: "all well\nhere" })
  })

  it("is null for anything else", () => {
    expect(parseReeveMessage("[legato] hello")).toBeNull()
    expect(parseReeveMessage("from the Reeve on vm-1")).toBeNull()
  })
})
