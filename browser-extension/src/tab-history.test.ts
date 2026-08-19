import { describe, expect, it } from "vitest"
import { predecessorFor, recordActivation, type WindowTabHistory } from "./tab-history"

const oldTab = { tabId: 10, windowId: 2, url: "http://localhost:3080/agents", activatedAt: 100 }
const newTab = { tabId: 11, windowId: 2, url: "http://localhost:3080/review/x?legato_source=streamdeck", activatedAt: 200 }

describe("tab activation history", () => {
  it("retains the prior activation when a new tab becomes active", () => {
    expect(recordActivation({ current: oldTab }, newTab)).toEqual({ current: newTab, previous: oldTab })
  })

  it("finds the predecessor before or after the marked tab activation event", () => {
    expect(predecessorFor({ current: oldTab }, 11)).toEqual(oldTab)
    expect(predecessorFor({ current: newTab, previous: oldTab }, 11)).toEqual(oldTab)
  })

  it("does not use a non-immediate or same-tab snapshot", () => {
    expect(predecessorFor({ current: newTab } satisfies WindowTabHistory, 11)).toBeUndefined()
  })
})
