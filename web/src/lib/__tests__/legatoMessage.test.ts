import { describe, expect, it } from "vitest"
import { legatoEventSummary, parseLegatoMessage } from "../legatoMessage"

describe("parseLegatoMessage", () => {
  it.each([
    ["[legato coordination] Agents already working in this Git worktree: a (ex-1).", "coordination", "Agents already working in this Git worktree: a (ex-1)."],
    ["[legato review] Question on step 3: why this lock?", "review", "Question on step 3: why this lock?"],
    ["[legato plan] Revision 2 was approved.", "plan", "Revision 2 was approved."],
    ["[legato] new swarm event #4 (question) — run `legato swarm inbox p1` to read.", "notice", "new swarm event #4 (question) — run `legato swarm inbox p1` to read."],
    ["[swarm event] worker \"API\" built", "swarm event", "worker \"API\" built"],
    // The steward's wakes after approval (spec LEG-559 Q5): a full bubble
    // labelled steward, never a one-line event.
    ["[legato steward] 1 failing check on #12. Run legato review-pass feedback --json to read it.", "steward", "1 failing check on #12. Run legato review-pass feedback --json to read it."],
    ["[legato review pass] Pass 1 has 1 blocking finding.", "review pass", "Pass 1 has 1 blocking finding."],
  ])("recognises %s", (text, label, body) => {
    expect(parseLegatoMessage(text)).toEqual({ label, body })
  })

  it("keeps an encoded swarm event whole, since its tag is the payload", () => {
    const text = "[swarm event b64:aGVsbG8=]"

    expect(parseLegatoMessage(text)).toEqual({ label: "swarm event", body: text })
  })

  it("recognises another agent's message by its sender, without the reply footer", () => {
    const text = "[legato peer pm4d4m4e] Please do not reformat PromptBar.tsx.\n\nI am done editing.\n\n---\nAgent-to-agent message from `pm4d4m4e`. Reply with `legato agent message pm4d4m4e \"<reply>\"`."

    expect(parseLegatoMessage(text)).toEqual({
      label: "peer",
      peer: "pm4d4m4e",
      body: "Please do not reformat PromptBar.tsx.\n\nI am done editing.",
    })
  })

  it("leaves what a person typed alone, even when it mentions Legato or starts with a bracket", () => {
    expect(parseLegatoMessage("fix the [legato coordination] wording")).toBeNull()
    expect(parseLegatoMessage("[WIP] rename the handler")).toBeNull()
    expect(parseLegatoMessage("legato coordination is noisy")).toBeNull()
  })

  it("marks a change set's result and a restart as events, which the conversation draws as one line", () => {
    const report = "[legato change set] Change set cs-816fab12cd (read the skills): 1 of 1 ran.\n1. Run on the host: cat a: ran: hello\n"

    expect(parseLegatoMessage(report)).toMatchObject({ label: "change set", event: true })
    expect(parseLegatoMessage("[legato restart] Legato is back: running v1.2.0 since 10:00:00 UTC.")).toMatchObject({ label: "restart", event: true })
    expect(parseLegatoMessage("[legato coordination] Agents already working here.")?.event).toBeUndefined()
  })

  it.each([
    ["Change set cs-816fab12cd (read the skills): 1 of 1 ran.\n1. …", "cs-816f… · 1 of 1 ran ✓"],
    ["Change set cs-816fab12cd (edit): 1 of 3 ran, 1 failed, 1 skipped.\n1. …", "cs-816f… · 1 of 3 ran, 1 failed, 1 skipped"],
    ["Change set cs-816fab12cd (restart): 1 of 1 ran.\n1. Restart: ran\nLegato restarts now; you will be told when it is back.\n", "cs-816f… · 1 of 1 ran ✓ · restarting"],
    ["The operator denied change set cs-816fab12cd (edit). Their note: no\nDo not …", "cs-816f… · denied"],
  ])("sums up a change set's message %#", (body, summary) => {
    expect(legatoEventSummary("change set", body)).toBe(summary)
  })

  it.each([
    ["Legato is back: running v1.2.0 since 10:00:00 UTC.\nThe restart was asked for by you", "restart: back"],
    ["Legato is back: running v1.2.0 since 10:00:00 UTC.\nThe restart failed and was rolled back: bad port\n", "restart: rolled back"],
  ])("sums up a restart's message %#", (body, summary) => {
    expect(legatoEventSummary("restart", body)).toBe(summary)
  })

  it("falls back to the first line for a message it does not know", () => {
    expect(legatoEventSummary("change set", "Something new.\nMore.")).toBe("Something new.")
  })
})
