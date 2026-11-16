import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { SkillsSettings } from "../settings/SkillsSettings"

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })
}

type Stored = { name: string; description: string; path: string; error?: string; enabled: boolean }
let stored: Stored[]
let requests: Array<{ method: string; url: string; body?: Record<string, unknown> }>

beforeEach(() => {
  stored = [
    { name: "broken", description: "", path: "/home/me/.legato/skills/broken", error: "no SKILL.md in the folder", enabled: true },
    { name: "tdd", description: "Tests first.", path: "/home/me/.legato/skills/tdd", enabled: true },
  ]
  requests = []
  vi.stubGlobal("fetch", vi.fn(async (url: string, init?: RequestInit) => {
    const method = init?.method ?? "GET"
    const body = init?.body ? JSON.parse(String(init.body)) : undefined
    requests.push({ method, url, body })
    if (method === "GET") return json({ dir: "/home/me/.legato/skills", skills: stored })
    if (method === "PUT") {
      const name = decodeURIComponent(String(url).split("/").pop() ?? "")
      const skill = stored.find((s) => s.name === name)
      if (!skill) return json({ error: `no skill named ${name} in the skills folder` }, 404)
      skill.enabled = Boolean(body?.enabled)
      return json(skill)
    }
    return json({ error: "nope" }, 400)
  }))
})

afterEach(() => {
  cleanup()
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe("SkillsSettings", () => {
  it("lists each skill with its description, folder and state, and a broken one with its error", async () => {
    render(<SkillsSettings baseUrl="" />)

    const tdd = await screen.findByRole("listitem", { name: "tdd" })
    expect(within(tdd).getByText("Tests first.")).toBeTruthy()
    expect(within(tdd).getByText("/home/me/.legato/skills/tdd")).toBeTruthy()
    expect(within(tdd).getByRole("button", { name: "Give agents tdd" }).getAttribute("aria-pressed")).toBe("true")

    const broken = screen.getByRole("listitem", { name: "broken" })
    expect(within(broken).getByText("no SKILL.md in the folder")).toBeTruthy()
    expect(within(broken).queryByRole("button", { name: /Give agents/ })).toBeNull()

    expect(screen.getByText("/home/me/.legato/skills", { selector: "code" })).toBeTruthy()
  })

  it("turns a skill off and on", async () => {
    render(<SkillsSettings baseUrl="" />)
    const toggle = await screen.findByRole("button", { name: "Give agents tdd" })

    fireEvent.click(toggle)
    await vi.waitFor(() => expect(screen.getByRole("button", { name: "Give agents tdd" }).getAttribute("aria-pressed")).toBe("false"))
    expect(requests.find((r) => r.method === "PUT")).toEqual({ method: "PUT", url: "/api/settings/skills/tdd", body: { enabled: false } })

    fireEvent.click(screen.getByRole("button", { name: "Give agents tdd" }))
    await vi.waitFor(() => expect(screen.getByRole("button", { name: "Give agents tdd" }).getAttribute("aria-pressed")).toBe("true"))
  })

  it("shows a refusal and keeps the skill as it was", async () => {
    render(<SkillsSettings baseUrl="" />)
    await screen.findByRole("listitem", { name: "tdd" })
    stored = stored.filter((s) => s.name !== "tdd")

    fireEvent.click(screen.getByRole("button", { name: "Give agents tdd" }))
    expect((await screen.findByRole("alert")).textContent).toContain("no skill named tdd")
  })

  it("says where to put skills when there are none", async () => {
    stored = []
    render(<SkillsSettings baseUrl="" />)
    expect(await screen.findByText(/No skills yet/)).toBeTruthy()
  })

  it("picks up a skill folder added while the page is open", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    stored = []
    render(<SkillsSettings baseUrl="" />)
    await screen.findByText(/No skills yet/)

    stored = [{ name: "added", description: "Dropped in.", path: "/home/me/.legato/skills/added", enabled: true }]
    await act(async () => { await vi.advanceTimersByTimeAsync(11_000) })
    expect(await screen.findByRole("listitem", { name: "added" })).toBeTruthy()
  })
})
