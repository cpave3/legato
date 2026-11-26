import { describe, expect, it } from "vitest"
import { needsYouKind } from "../needsYou"

describe("needsYouKind", () => {
  // A steward held, its approval dismissed or ended idle (LEG-593) waits
  // on the operator as a review loop waiting on its author does; a stuck
  // one asks like a gate.
  it("draws a review loop's items by what they need", () => {
    expect(needsYouKind({ signal_kind: "workflow_review_steward" })).toBe("waiting")
    expect(needsYouKind({ signal_kind: "workflow_review_author" })).toBe("waiting")
    expect(needsYouKind({ signal_kind: "workflow_review_stuck" })).toBe("gate")
  })
})
