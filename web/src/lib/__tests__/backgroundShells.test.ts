import { describe, expect, it } from "vitest"
import { createNativeSyncState, reconcileNativeSnapshot, reduceNativeEvent, stateFromNativeSnapshot, type NativeEvent } from "../nativeAgentSync"
import { fromStored, lastLine, mergeShells, plainOutput, shellOutputLimit } from "../backgroundShells"

const started = (id: string): NativeEvent => ({
  eventId: `start-${id}`, type: "background_shell",
  shell: { shellId: id, callId: `toolu_${id}`, command: "sleep 2; echo hi; sleep 30", status: "running", startedAt: 1000, output: { available: true, text: "" } },
})

describe("background shells in the native sync", () => {
  it("records a shell as it starts, follows its output and marks its exit", () => {
    let state = reduceNativeEvent(createNativeSyncState(), started("b1"))
    state = reduceNativeEvent(state, { eventId: "o1", type: "background_shell_output", shellId: "b1", chunk: "hi\n" })
    state = reduceNativeEvent(state, { eventId: "o2", type: "background_shell_output", shellId: "b1", chunk: "there\n" })
    state = reduceNativeEvent(state, {
      eventId: "end", type: "background_shell",
      shell: { shellId: "b1", command: "sleep 2; echo hi; sleep 30", status: "exited", exitCode: 3, startedAt: 1000, endedAt: 9000, output: { available: true, text: "" } },
    })

    expect(state.backgroundShells).toHaveLength(1)
    const [shell] = state.backgroundShells
    expect(shell.status).toBe("exited")
    expect(shell.exitCode).toBe(3)
    expect(shell.callId).toBe("toolu_b1")
    // The end's event carries no output: what was gathered stays.
    expect(shell.output?.text).toBe("hi\nthere\n")
  })

  it("keeps two shells apart", () => {
    let state = reduceNativeEvent(createNativeSyncState(), started("b1"))
    state = reduceNativeEvent(state, started("b2"))
    state = reduceNativeEvent(state, { eventId: "o", type: "background_shell_output", shellId: "b2", chunk: "two\n" })
    expect(state.backgroundShells.map((shell) => [shell.shellId, shell.output?.text])).toEqual([["b1", ""], ["b2", "two\n"]])
  })

  it("keeps only the newest output, and starts over after a gap", () => {
    let state = reduceNativeEvent(createNativeSyncState(), started("b1"))
    state = reduceNativeEvent(state, { eventId: "o1", type: "background_shell_output", shellId: "b1", chunk: "x".repeat(shellOutputLimit) })
    state = reduceNativeEvent(state, { eventId: "o2", type: "background_shell_output", shellId: "b1", chunk: "tail\n" })
    expect(state.backgroundShells[0].output?.text.length).toBe(shellOutputLimit)
    expect(state.backgroundShells[0].output?.text.endsWith("tail\n")).toBe(true)
    expect(state.backgroundShells[0].output?.truncated).toBe(true)

    state = reduceNativeEvent(state, { eventId: "o3", type: "background_shell_output", shellId: "b1", chunk: "after the gap\n", gap: true })
    expect(state.backgroundShells[0].output?.text).toBe("after the gap\n")
  })

  it("hydrates shells and their output from a snapshot, without repeating output read before it", () => {
    const snapshot = {
      status: "idle",
      backgroundShells: [{ shellId: "b1", command: "npm run dev", status: "running" as const, startedAt: 1, output: { available: true, text: "ready\n" } }],
    }
    expect(stateFromNativeSnapshot(snapshot).backgroundShells[0].output?.text).toBe("ready\n")

    const state = reconcileNativeSnapshot(snapshot, createNativeSyncState(), [
      { eventId: "o1", type: "background_shell_output", shellId: "b1", chunk: "ready\n" },
      { eventId: "o2", type: "background_shell_output", shellId: "b1", chunk: "GET /\n" },
    ])
    expect(state.backgroundShells[0].output?.text).toBe("ready\nGET /\n")
  })
})

describe("background shells Legato recorded", () => {
  it("reads the stored records, which carry no output, and lets the endpoint's win", () => {
    const stored = fromStored([
      { id: "b1", command: "npm run dev", status: "running", started_at: "2026-11-17T03:05:00Z" },
      { id: "b2", command: "make", status: "exited", exit_code: 2, started_at: "2026-11-17T03:05:00Z", ended_at: "2026-11-17T03:06:00Z" },
    ])
    expect(stored[1]).toMatchObject({ shellId: "b2", status: "exited", exitCode: 2, endedAt: Date.parse("2026-11-17T03:06:00Z"), output: { available: false } })

    const merged = mergeShells([{ shellId: "b1", command: "npm run dev", status: "running", output: { available: true, text: "ok\n" } }], stored)
    expect(merged.map((shell) => [shell.shellId, shell.output?.available])).toEqual([["b1", true], ["b2", false]])
  })
})

describe("shell output as text", () => {
  it("drops colours and keeps what a redrawn line shows last", () => {
    expect(plainOutput("\x1b[32mok\x1b[0m\nprogress 10%\rprogress 100%\n")).toBe("ok\nprogress 100%\n")
    expect(lastLine("first\n\x1b[1mlast\x1b[0m\n\n")).toBe("last")
  })
})
