import { describe, expect, it } from "vitest"
import { prRange, stackSummary } from "../stackSummary"
import { leg386, pr } from "./fixtures/prStacks"

describe("stackSummary", () => {
  it("folds the LEG-386 stack", () => {
    const s = stackSummary(leg386())
    expect(s.title).toBe("Stack of 13 PRs")
    expect(s.counts).toBe("2 merged")
    expect(s.layers.map(l => l.kind)).toEqual(["merged", "merged", "ready", "ready", "ready", "ready", "failing", "held", "held", "running", "held", "held", "held"])
    expect(s.barLabel).toBe("2 merged, 4 ready to merge, 1 failing, 1 with CI running, 5 waiting on a layer below")
    expect(s.needsYou?.layer.pr.pr_number).toBe(200)
    expect(s.needsYou?.holdsBack).toBe(6)
    expect(prRange(s.mergeable)).toBe("#196–#199")
    expect(s.groups).toHaveLength(1)
    expect(s.groups[0]).toMatchObject({ repo: "cpave3/legato", base: "main" })
    expect(s.layers.filter(l => l.heldBelow).map(l => l.pr.pr_number)).toEqual([201, 202, 203, 204, 205, 207])
  })

  it("names the repos, the closed layers and the forks", () => {
    const prs = [
      pr(10, "merged", "main", { repo: "acme/api" }),
      pr(11, "closed", "main", { repo: "acme/api" }),
      pr(12, "approved", "b10", { repo: "acme/api" }),
      pr(13, "run", "b10", { repo: "acme/api" }),
      pr(4, "draft", "main", { repo: "acme/web" }),
    ]
    const s = stackSummary(prs)
    expect(s.title).toBe("5 PRs in 2 repos")
    expect(s.counts).toBe("1 merged · 1 closed")
    expect(s.groups.map(g => `${g.repo} · ${g.base}`)).toEqual(["acme/api · main", "acme/web · main"])
    expect(s.layers.filter(l => l.fork).map(l => l.pr.pr_number)).toEqual([12, 13])
    expect(s.layers.find(l => l.pr.pr_number === 12)?.status.word).toBe("Ready to merge")
    expect(prRange(s.mergeable)).toBe("#12")
  })

  it("says a stack is all merged", () => {
    const s = stackSummary([pr(1, "merged", "main"), pr(2, "merged", "main")])
    expect(s.allMerged).toBe(true)
    expect(s.needsYou).toBeUndefined()
    expect(s.mergeable).toEqual([])
  })

  it("says a stack of drafts is drafts", () => {
    const s = stackSummary([pr(1, "draft", "main"), pr(2, "draft", "b1"), pr(3, "draft", "b2")])
    expect(s.counts).toBe("3 drafts")
    expect(s.draftsLine).toBe("Drafts. CI passing on all 3.")
    expect(s.needsYou).toBeUndefined()
  })

  it("holds back what stands on a layer that waits for review, without needing the operator", () => {
    const s = stackSummary([pr(1, "awaiting", "main"), pr(2, "ready", "b1")])
    expect(s.layers.map(l => l.status.word)).toEqual(["Awaiting review", "Approved"])
    expect(s.mergeable).toEqual([])
    expect(s.needsYou).toBeUndefined()
  })

  it("falls back to the server's order for PRs with no base yet", () => {
    const s = stackSummary([pr(1, "fail", ""), pr(2, "ready", "")])
    expect(s.layers[1].heldBelow).toBe(true)
    expect(s.needsYou?.holdsBack).toBe(1)
  })
})
