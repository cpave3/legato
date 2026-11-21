import { describe, it, expect, beforeEach, afterEach, vi } from "vitest"
import { clearSnooze, holdsWork, readSnooze, resetSnoozeMemory, snoozeOver, writeSnooze } from "../railSnooze"

const base = "https://legato.test:8080"

describe("rail snooze", () => {
  beforeEach(() => { window.localStorage.clear(); resetSnoozeMemory() })
  afterEach(() => vi.restoreAllMocks())

  it("is kept per server and read back until it expires", () => {
    writeSnooze(base, { until: 2_000, seen: ["a"] })
    expect(readSnooze(base, 1_000)).toEqual({ until: 2_000, seen: ["a"] })
    expect(readSnooze("https://other.test", 1_000)).toBeNull()
    expect(readSnooze(base, 2_000)).toBeNull()
    // An expired record is removed.
    expect(window.localStorage.length).toBe(0)
  })

  it("reads an unreadable record as no snooze, and removes it", () => {
    window.localStorage.setItem(`legato:rail-snooze:${base}`, "{not json")
    expect(readSnooze(base, 0)).toBeNull()
    expect(window.localStorage.length).toBe(0)
  })

  it("clears", () => {
    writeSnooze(base, { until: 5_000, seen: [] })
    clearSnooze(base)
    expect(readSnooze(base, 0)).toBeNull()
  })

  it("holds a snooze for the page when storage refuses writes", () => {
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => { throw new Error("quota") })
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => { throw new Error("blocked") })
    writeSnooze(base, { until: 5_000, seen: ["a"] })
    expect(readSnooze(base, 1_000)).toEqual({ until: 5_000, seen: ["a"] })
  })

  it("holds a snooze for the page when only storage writes fail", () => {
    writeSnooze(base, { until: 3_000, seen: ["old"] })
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => { throw new Error("quota") })
    writeSnooze(base, { until: 9_000, seen: ["new"] })
    expect(readSnooze(base, 1_000)).toEqual({ until: 9_000, seen: ["new"] })
  })

  it("is no snooze when storage cannot be read and nothing was snoozed on this page", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => { throw new Error("blocked") })
    expect(readSnooze(base, 0)).toBeNull()
  })

  it("says which items hold work: approvals, questions, plans and gates", () => {
    for (const signal_kind of ["approval", "operator_question", "plan_decision", "workflow_manual_gate", "workflow_trigger_input"]) {
      expect(holdsWork({ signal_kind })).toBe(true)
    }
    for (const signal_kind of ["agent_waiting", "agent_error", "operator_done", "operator_action", "review_ready", "update_available"]) {
      expect(holdsWork({ signal_kind })).toBe(false)
    }
  })

  it("is over when its time passes, or a new item that holds work arrives", () => {
    const snooze = { until: 10_000, seen: ["q1", "w1"] }
    expect(snoozeOver(null, [], 0)).toBe(true)
    expect(snoozeOver(snooze, [{ id: "q1", holdsWork: true }, { id: "w1", holdsWork: false }], 5_000)).toBe(false)
    expect(snoozeOver(snooze, [{ id: "w2", holdsWork: false }], 5_000)).toBe(false)
    expect(snoozeOver(snooze, [{ id: "q2", holdsWork: true }], 5_000)).toBe(true)
    expect(snoozeOver(snooze, [], 10_000)).toBe(true)
  })
})
