import { afterEach, describe, expect, it, vi } from "vitest"
import { fetchMacros, macrosFor, pickerMacros, prIsMine, splitFavorites, saveMacro, startMacro, suggestedFirst, type Macro } from "../macros"

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

describe("pickerMacros", () => {
  const fixCI: Macro = { name: "Fix CI", keys: "f", applies_to: "task", favorite: true }
  const review: Macro = { name: "Review", keys: "r", applies_to: "pr", favorite: true }
  const plan: Macro = { name: "Plan", keys: "p" }
  const ticket: Macro = { name: "Ticket to PR", keys: "t", applies_to: "task" }
  const all = [fixCI, review, plan, ticket]
  const names = (got: { favorites: Macro[]; rest: Macro[] }) => ({ favorites: got.favorites.map(m => m.name), rest: got.rest.map(m => m.name) })

  it("lists every macro, favourites first, when no targets are given", () => {
    expect(names(pickerMacros(all))).toEqual({ favorites: ["Fix CI", "Review"], rest: ["Plan", "Ticket to PR"] })
  })

  it("keeps the macros for any of the targets, and those that name none", () => {
    expect(names(pickerMacros(all, { targets: ["task", "pr"] }))).toEqual({ favorites: ["Fix CI", "Review"], rest: ["Plan", "Ticket to PR"] })
    expect(names(pickerMacros(all, { targets: ["pr"] }))).toEqual({ favorites: ["Review"], rest: ["Plan"] })
    expect(names(pickerMacros(all, { targets: ["task"] }))).toEqual({ favorites: ["Fix CI"], rest: ["Plan", "Ticket to PR"] })
  })

  it("decides whether anything is starred over every macro, not the ones kept", () => {
    expect(names(pickerMacros([fixCI, plan, { ...review, favorite: false }], { targets: ["pr"] }))).toEqual({ favorites: [], rest: ["Plan", "Review"] })
  })

  it("lists every kept macro as a favourite while none is starred", () => {
    expect(names(pickerMacros([plan, ticket], { targets: ["task"] }))).toEqual({ favorites: ["Plan", "Ticket to PR"], rest: [] })
  })

  it("puts the suggested macro first in its group", () => {
    expect(names(pickerMacros(all, { targets: ["task"], suggested: "ticket to pr" }))).toEqual({ favorites: ["Fix CI"], rest: ["Ticket to PR", "Plan"] })
  })
})

describe("macrosFor several targets", () => {
  it("keeps the macros for any of them", () => {
    const list: Macro[] = [{ name: "A", keys: "a", applies_to: "task" }, { name: "B", keys: "b", applies_to: "pr" }, { name: "C", keys: "c" }]
    expect(macrosFor(list, ["task", "pr"]).map(m => m.name)).toEqual(["A", "B", "C"])
  })
})

describe("saveMacro", () => {
  it("sends the selected subscription in the replacement HTTP body", async () => {
    const fetch = vi.fn().mockResolvedValue(new Response(null, { status: 204 }))
    vi.stubGlobal("fetch", fetch)
    const fields = { message: "Build", agent: "claude-code", account: "work" }
    await saveMacro("", "Work macro", fields)
    expect(fetch.mock.calls[0][0]).toBe("/api/macros/Work%20macro")
    expect(fetch.mock.calls[0][1].method).toBe("PUT")
    expect(JSON.parse(fetch.mock.calls[0][1].body)).toEqual(fields)
  })
})
