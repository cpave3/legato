import { afterEach, describe, expect, it, vi } from "vitest"
import { fetchMacros, macrosFor, startMacro, type Macro } from "../macros"

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
