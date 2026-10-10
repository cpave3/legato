import { describe, expect, it } from "vitest"
import { parseLegatoMessage } from "../legatoMessage"

describe("parseLegatoMessage", () => {
  it.each([
    ["[legato coordination] Agents already working in this Git worktree: a (ex-1).", "coordination", "Agents already working in this Git worktree: a (ex-1)."],
    ["[legato review] Question on step 3: why this lock?", "review", "Question on step 3: why this lock?"],
    ["[legato plan] Revision 2 was approved.", "plan", "Revision 2 was approved."],
    ["[legato] new swarm event #4 (question) — run `legato swarm inbox p1` to read.", "notice", "new swarm event #4 (question) — run `legato swarm inbox p1` to read."],
    ["[swarm event] worker \"API\" built", "swarm event", "worker \"API\" built"],
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
})
