import { afterEach, describe, expect, it, vi } from "vitest"
import { fetchMacros, macrosFor, prIsMine, splitFavorites, startMacro, suggestedFirst, type Macro } from "../macros"

afterEach(() => vi.unstubAllGlobals())

describe("startMacro", () => {
  it("posts the macro, and the pull request when there is one, to the task", async () => {
    const fetch = vi.fn().mockImplementation(async () => new Response(JSON.stringify({ task_id: "T-1", started: true, queued: false, agent_kind: "codex" }), { status: 201 }))
    vi.stubGlobal("fetch", fetch)
    expect(await startMacro("http://legato", "T-1", "Plan")).toEqual({ task_id: "T-1", started: true, queued: false, agent_kind: "codex" })
    expect(fetch.mock.calls[0][0]).toBe("http://legato/api/tasks/T-1/macro")
    expect(fetch.mock.calls[0][1].method).toBe("POST")
    expect(JSON.parse(fetch.mock.calls[0][1].body)).toEqual({ name: "Plan" })
    await startMacro("", "T 2", "Review", { repo: "acme/web", pr_number: 4, url: "https://x" })
    expect(fetch.mock.calls[1][0]).toBe("/api/tasks/T%202/macro")
    expect(JSON.parse(fetch.mock.calls[1][1].body)).toEqual({ name: "Review", pr: { repo: "acme/web", pr_number: 4, url: "https://x" } })
  })

  it("throws the server's reason", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify({ error: "no macro of that name" }), { status: 404 })))
    await expect(startMacro("", "T-1", "nope")).rejects.toThrow("no macro of that name")
  })
})

describe("fetchMacros and macrosFor", () => {
  it("lists the macros and keeps those for a task or a pull request", async () => {
    const list: Macro[] = [
      { name: "Plan", keys: "p", applies_to: "task", builtin: true },
      { name: "Review", keys: "r", applies_to: "pr", builtin: true },
      { name: "tests", keys: "t" },
    ]
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify({ macros: list }))))
    const got = await fetchMacros("")
    expect(got).toEqual(list)
    expect(macrosFor(got, "task").map(m => m.name)).toEqual(["Plan", "tests"])
    expect(macrosFor(got, "pr").map(m => m.name)).toEqual(["Review", "tests"])
  })
})

describe("suggestedFirst", () => {
  const list: Macro[] = [
    { name: "Plan", keys: "p", applies_to: "task" },
    { name: "Implement", keys: "i", applies_to: "task" },
    { name: "Review", keys: "r", applies_to: "pr" },
    { name: "Address feedback", keys: "a", applies_to: "pr" },
    { name: "tests", keys: "t" },
  ]
  const suggested = { task: "Plan", my_pr: "Address feedback", others_pr: "Review" }
  it("puts the suggested macro first for each target", () => {
    expect(suggestedFirst(list, "task", suggested).list.map(m => m.name)).toEqual(["Plan", "Implement", "tests"])
    const mine = suggestedFirst(list, "my_pr", suggested)
    expect(mine.suggested?.name).toBe("Address feedback")
    expect(mine.list.map(m => m.name)).toEqual(["Address feedback", "Review", "tests"])
    expect(suggestedFirst(list, "others_pr", suggested).suggested?.name).toBe("Review")
    expect(suggestedFirst(list.filter(m => m.name !== "Review"), "others_pr", suggested).suggested).toBeUndefined()
  })
  it("takes a PR found from the task's branch as the operator's own, and one linked by number as not", () => {
    expect(prIsMine({ pinned: false })).toBe(true)
    expect(prIsMine({ pinned: true })).toBe(false)
  })
})

describe("splitFavorites", () => {
  const plan = { name: "Plan", keys: "p", favorite: true }
  const ticket = { name: "Ticket to PR", keys: "t" }
  const tests = { name: "Tests", keys: "x", favorite: true }

  it("lists the starred macros in order and keeps the rest apart", () => {
    const { favorites, rest } = splitFavorites([plan, ticket, tests])
    expect(favorites.map(m => m.name)).toEqual(["Plan", "Tests"])
    expect(rest.map(m => m.name)).toEqual(["Ticket to PR"])
  })

  it("counts every macro as a favourite while none is starred", () => {
    expect(splitFavorites([ticket, { name: "Other", keys: "o" }])).toEqual({ favorites: [ticket, { name: "Other", keys: "o" }], rest: [] })
  })

  it("follows a star elsewhere in the full list", () => {
    expect(splitFavorites([ticket], true)).toEqual({ favorites: [], rest: [ticket] })
  })
})
