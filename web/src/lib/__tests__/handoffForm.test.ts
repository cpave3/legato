import { describe, expect, it } from "vitest"
import { chosenWorkerView, handOffReady, handOffRequest, localToolOrder, repositoryOptionLabel, reviewFieldValues, type HandOffDraft } from "../handoffForm"
import type { ReviewPassesAdvert } from "../away"
import type { Worker } from "../workers"

const advert: ReviewPassesAdvert = {
  defaults: { enabled: true, agent: "codex", tier: "careful", model: "gpt-5.5", max_passes: 3 },
  adapters: [{ name: "codex", tiers: ["careful"] }, { name: "claude-code" }],
  skills: ["code-review"],
}

const draft: HandOffDraft = { worker: "w1", purpose: "make-change", kind: "new-branch", repo: " acme/widget ", branch: "", tool: "codex", advert: null, review: {} }

describe("localToolOrder", () => {
  it("puts the default first and never offers the shell", () => {
    expect(localToolOrder({ adapters: ["claude-code", "shell", "codex"], default: "codex" })).toEqual(["codex", "claude-code"])
  })
  it("starts with the first tool when the default is the shell or missing", () => {
    expect(localToolOrder({ adapters: ["shell", "chimera", "codex"], default: "shell" })).toEqual(["chimera", "codex"])
    expect(localToolOrder({})).toEqual([])
  })
})

describe("handOffReady", () => {
  it("needs a worker and an owner/name repository", () => {
    expect(handOffReady(draft, false)).toBe(true)
    expect(handOffReady({ ...draft, worker: "" }, false)).toBe(false)
    expect(handOffReady({ ...draft, repo: "widget" }, false)).toBe(false)
    expect(handOffReady(draft, true)).toBe(false)
  })
  it("needs a branch's name to start from a pushed branch", () => {
    expect(handOffReady({ ...draft, kind: "pushed-branch", branch: "  " }, false)).toBe(false)
    expect(handOffReady({ ...draft, kind: "pushed-branch", branch: "fix" }, false)).toBe(true)
  })
})

describe("handOffRequest", () => {
  it("trims the repository and branch and sends the branch only from a pushed branch", () => {
    expect(handOffRequest({ ...draft, branch: "x" })).toEqual({ worker: "w1", purpose: "make-change", kind: "new-branch", repo: "acme/widget", agent_kind: "codex" })
    expect(handOffRequest({ ...draft, kind: "pushed-branch", branch: " fix " }).branch).toBe("fix")
  })
  it("sends review changes only for a change, to a worker with review passes, when some changed", () => {
    expect(handOffRequest({ ...draft, advert, review: { max_passes: 5 } }).review).toEqual({ max_passes: 5 })
    expect(handOffRequest({ ...draft, advert, review: {} }).review).toBeUndefined()
    expect(handOffRequest({ ...draft, review: { max_passes: 5 } }).review).toBeUndefined()
    expect(handOffRequest({ ...draft, advert, purpose: "find-out", review: { max_passes: 5 } }).review).toBeUndefined()
  })
})

describe("chosenWorkerView", () => {
  const vm1 = { instance_id: "w1", name: "vm-1", advert: { repositories: ["acme/widget"] } } as unknown as Worker
  it("names the chosen worker and says whether it can clone the repository", () => {
    expect(chosenWorkerView([vm1], "w1", "acme/other")).toMatchObject({ name: "vm-1", repoBlocked: true })
    expect(chosenWorkerView([vm1], "w1", "acme/widget").repoBlocked).toBe(false)
    expect(chosenWorkerView([vm1], "w1", "  ").repoBlocked).toBe(false)
  })
  it("says the worker before the workers are read", () => {
    expect(chosenWorkerView(null, "", "")).toEqual({ advert: undefined, name: "the worker", repoBlocked: false })
  })
})

describe("repositoryOptionLabel", () => {
  it("names a repository once when its name is its GitHub name", () => {
    expect(repositoryOptionLabel("acme/widget", "acme/widget")).toBe("acme/widget")
    expect(repositoryOptionLabel("Widget", "acme/widget")).toBe("Widget · acme/widget")
  })
})

describe("reviewFieldValues", () => {
  it("starts at the worker's settings", () => {
    expect(reviewFieldValues(advert, {})).toEqual({ enabled: true, agent: "codex", tiers: ["careful"], tier: "careful", model: "gpt-5.5", skill: "", maxPasses: 3 })
  })
  it("gives another reviewer neither the default tier nor model", () => {
    expect(reviewFieldValues(advert, { agent: "claude-code" })).toMatchObject({ agent: "claude-code", tiers: [], tier: "", model: "" })
  })
  it("shows the handoff's changes", () => {
    expect(reviewFieldValues(advert, { enabled: false, skill: "code-review", max_passes: 7, model: "m" })).toMatchObject({ enabled: false, skill: "code-review", maxPasses: 7, model: "m" })
  })
})
