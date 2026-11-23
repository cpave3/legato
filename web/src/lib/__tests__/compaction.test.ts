import { describe, expect, it } from "vitest"
import { compactCommand, compactionNotice, nextCompaction } from "../compaction"

describe("compactCommand", () => {
  it("reads /compact alone and with focus words", () => {
    expect(compactCommand("/compact")).toEqual({ instructions: "" })
    expect(compactCommand("  /compact   keep the plan\nand the tests ")).toEqual({ instructions: "keep the plan\nand the tests" })
  })

  it("is not another command, a path or a message", () => {
    expect(compactCommand("/compacted")).toBeNull()
    expect(compactCommand("/compact/notes.md what is this?")).toBeNull()
    expect(compactCommand("please /compact")).toBeNull()
  })
})

describe("the compaction notice", () => {
  it("says a compaction is running, then what it saved", () => {
    const running = nextCompaction(undefined, { type: "compaction_started", reason: "manual" })
    expect(compactionNotice(running)).toBe("Compacting the conversation…")

    const done = nextCompaction(running, { type: "compaction_finished", tokensBefore: 49805, tokensAfter: 6100 })
    expect(compactionNotice(done)).toBe("Compacted: 49.8k → 6.1k tokens")
  })

  it("says compacted without figures the tool did not give", () => {
    expect(compactionNotice(nextCompaction(undefined, { type: "compaction_finished" }))).toBe("Compacted")
  })

  it("says when the tool compacted on its own", () => {
    expect(compactionNotice(nextCompaction(undefined, { type: "compaction_started", reason: "threshold" }))).toBe("Compacting the conversation (the context was nearly full)…")
  })

  it("says why a compaction failed", () => {
    expect(compactionNotice(nextCompaction(undefined, { type: "compaction_failed", error: "Not enough messages to compact." })))
      .toBe("Compaction failed: Not enough messages to compact.")
  })

  it("clears a finished compaction when the next turn starts, not a running one", () => {
    const done = nextCompaction(undefined, { type: "compaction_finished" })
    expect(nextCompaction(done, { type: "session_started" })).toBeUndefined()
    const running = nextCompaction(undefined, { type: "compaction_started" })
    expect(nextCompaction(running, { type: "session_started" })).toBe(running)
  })

  it("leaves the state alone for other events", () => {
    const running = nextCompaction(undefined, { type: "compaction_started" })
    expect(nextCompaction(running, { type: "assistant_text_done" })).toBe(running)
  })
})
