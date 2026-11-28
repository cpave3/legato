import { describe, expect, it, vi } from "vitest"
import { cardActionItems, reviewMenuItems, runReviewItem, type ReviewMenuItem } from "../cardMenu"
import type { BoardCard, PRMetaView } from "../board-types"
import type { FoundReviewLoop } from "../reviewLoops"

const card = { id: "T-1", title: "Card", provider: "" } as BoardCard
const pr = (n: number, repo = "acme/widget") => ({ repo, pr_number: n }) as PRMetaView
const loop = { loop_id: "rl-1", link: "/workflows/runs/wr-1" } as FoundReviewLoop

describe("cardActionItems", () => {
  it("lists a task's actions, Hand off only with a worker", () => {
    expect(cardActionItems(card, false).map(i => i.action)).toEqual(["open", "move", "delete"])
    expect(cardActionItems(card, true).map(i => i.action)).toEqual(["open", "move", "handoff", "delete"])
  })
  it("adds the ticket's source after Open details", () => {
    expect(cardActionItems({ ...card, provider: "burnlite", source_url: "https://b/1" } as BoardCard, false)[1]).toEqual({ action: "openSource", label: "Open in Burnlite" })
  })
  it("offers an away task its worker and Recall only when they apply", () => {
    const away = (more: object) => ({ ...card, away: { worker: "vm-1", ...more } }) as BoardCard
    expect(cardActionItems(away({ url: "https://vm-1", view: "working" }), true).map(i => i.label)).toEqual(["Open details", "Open on vm-1", "Recall…"])
    expect(cardActionItems(away({ view: "unreachable" }), true).map(i => i.label)).toEqual(["Open details"])
  })
})

describe("reviewMenuItems", () => {
  const lookups = (entries: [number, boolean, FoundReviewLoop?][], repo = "acme/widget") =>
    Object.fromEntries(entries.map(([n, agent, held]) => [`${repo}#${n}`, { loop: held ?? null, agent_author: agent }]))

  it("offers nothing for pull requests not looked up", () => {
    expect(reviewMenuItems([pr(1)], null, true)).toEqual([])
    expect(reviewMenuItems([pr(1), pr(2)], lookups([[1, true]]), true).map(i => i.label)).toEqual(["Review passes · #1 · agent"])
  })
  it("offers one pull request without a scope", () => {
    expect(reviewMenuItems([pr(12)], lookups([[12, false]]), true)).toEqual([{ key: "acme/widget-12", label: "Review passes · #12 · person", pr: pr(12), scope: undefined }])
  })
  it("offers a stack first, an agent's only when every layer is, then each alone", () => {
    const items = reviewMenuItems([pr(1), pr(2)], lookups([[1, true], [2, false]]), true)
    expect(items.map(i => [i.label, i.scope])).toEqual([["Review passes · stack #1–#2 · person", "stack"], ["Review passes · #1 · agent", "pr"], ["Review passes · #2 · person", "pr"]])
    expect(items[0].agentAuthor).toBe(false)
  })
  it("offers no stack while a loop holds a layer, and the held one's loop", () => {
    const items = reviewMenuItems([pr(1), pr(2)], lookups([[1, true], [2, true, loop]]), true)
    expect(items.map(i => i.label)).toEqual(["Review passes · #1 · agent", "Open review loop · #2"])
    expect(items[0].scope).toBe("pr")
    expect(items[1].loop).toBe(loop)
  })
  it("offers no stack without a scope to take, or across repositories", () => {
    expect(reviewMenuItems([pr(1), pr(2)], lookups([[1, true], [2, true]]), false).map(i => i.scope)).toEqual([undefined, undefined])
    const mixed = { ...lookups([[1, true]]), ...lookups([[2, true]], "acme/gadget") }
    expect(reviewMenuItems([pr(1), pr(2, "acme/gadget")], mixed, true).map(i => i.scope)).toEqual([undefined, undefined])
  })
})

describe("runReviewItem", () => {
  const item = (more: Partial<ReviewMenuItem>): ReviewMenuItem => ({ key: "k", label: "l", pr: pr(1), ...more })
  it("opens a held loop instead of starting anything", () => {
    const start = vi.fn(), open = vi.fn()
    runReviewItem(item({ loop }), start, open)
    expect(open).toHaveBeenCalledWith(loop)
    expect(start).not.toHaveBeenCalled()
  })
  it("starts with only the arguments the scope needs", () => {
    const start = vi.fn()
    runReviewItem(item({ scope: "stack", agentAuthor: true }), start)
    runReviewItem(item({ scope: "pr" }), start)
    runReviewItem(item({}), start)
    expect(start.mock.calls).toEqual([[pr(1), "stack", true], [pr(1), "pr"], [pr(1)]])
  })
})
