import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { ManagedHooksSettings } from "../settings/ManagedHooksSettings"

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })
}

type Stored = { name: string; description: string; events: string[]; agent_tools: string[]; read_only: boolean; path: string; error?: string; enabled: boolean }
const dir = "/home/me/.legato/managed-hooks"
let stored: Stored[]
let requests: Array<{ method: string; url: string; body?: Record<string, unknown> }>

beforeEach(() => {
  stored = [
    { name: "broken", description: "", events: [], agent_tools: [], read_only: false, path: `${dir}/broken`, error: "HOOK.yaml: events.stop: match is for pre_tool and post_tool only", enabled: true },
    { name: "codeeval", description: "Net-debt gate.", events: ["session_start", "stop"], agent_tools: ["claude-code", "chimera"], read_only: false, path: `${dir}/codeeval`, enabled: true },
    { name: "observer", description: "Logs tool calls.", events: ["post_tool"], agent_tools: [], read_only: true, path: `${dir}/observer`, enabled: false },
  ]
  requests = []
  vi.stubGlobal("fetch", vi.fn(async (url: string, init?: RequestInit) => {
    const method = init?.method ?? "GET"
    const body = init?.body ? JSON.parse(String(init.body)) : undefined
    requests.push({ method, url, body })
    if (method === "GET") return json({ dir, hooks: stored, repositories: [] })
    if (method === "PUT") {
      const name = decodeURIComponent(String(url).split("/").pop() ?? "")
      const hook = stored.find((h) => h.name === name && !h.error)
      if (!hook) return json({ error: `no usable managed hook named ${name} in the managed-hooks folder` }, 404)
      hook.enabled = Boolean(body?.enabled)
      return json(hook)
    }
    return json({ error: "nope" }, 400)
  }))
})

afterEach(() => {
  cleanup()
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe("ManagedHooksSettings", () => {
  it("lists each hook with its description, events, agent tools, folder and state", async () => {
    render(<ManagedHooksSettings baseUrl="" />)

    const gate = await screen.findByRole("listitem", { name: "codeeval" })
    expect(within(gate).getByText("Net-debt gate.")).toBeTruthy()
    expect(within(gate).getByText(/session_start, stop/)).toBeTruthy()
    expect(within(gate).getByText(/claude-code, chimera/)).toBeTruthy()
    expect(within(gate).getByText(`${dir}/codeeval`)).toBeTruthy()
    expect(within(gate).getByRole("button", { name: "Give agents codeeval" }).getAttribute("aria-pressed")).toBe("true")

    const observer = screen.getByRole("listitem", { name: "observer" })
    expect(within(observer).getByText(/every agent tool/)).toBeTruthy()
    expect(within(observer).getByRole("button", { name: "Give agents observer" }).getAttribute("aria-pressed")).toBe("false")

    expect(screen.getByText(dir, { selector: "code" })).toBeTruthy()
  })

  it("marks a broken hook with its error and offers no toggle for it", async () => {
    render(<ManagedHooksSettings baseUrl="" />)
    const broken = await screen.findByRole("listitem", { name: "broken" })
    expect(within(broken).getByText("error")).toBeTruthy()
    expect(within(broken).getByText(/match is for pre_tool and post_tool only/)).toBeTruthy()
    expect(within(broken).queryByRole("button", { name: /Give agents/ })).toBeNull()
  })

  it("turns a hook off and on for every agent", async () => {
    render(<ManagedHooksSettings baseUrl="" />)
    const toggle = await screen.findByRole("button", { name: "Give agents codeeval" })

    fireEvent.click(toggle)
    await vi.waitFor(() => expect(screen.getByRole("button", { name: "Give agents codeeval" }).getAttribute("aria-pressed")).toBe("false"))
    expect(requests.find((r) => r.method === "PUT")).toEqual({ method: "PUT", url: "/api/settings/managed-hooks/codeeval", body: { enabled: false } })

    fireEvent.click(screen.getByRole("button", { name: "Give agents codeeval" }))
    await vi.waitFor(() => expect(screen.getByRole("button", { name: "Give agents codeeval" }).getAttribute("aria-pressed")).toBe("true"))
  })

  it("names the folder when there are no hooks", async () => {
    stored = []
    render(<ManagedHooksSettings baseUrl="" />)
    const empty = await screen.findByText(/No managed hooks yet/)
    expect(empty.textContent).toContain(dir)
  })

  it("picks up a hook folder added while the page is open", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    stored = []
    render(<ManagedHooksSettings baseUrl="" />)
    await screen.findByText(/No managed hooks yet/)

    stored = [{ name: "added", description: "Dropped in.", events: ["stop"], agent_tools: [], read_only: false, path: `${dir}/added`, enabled: true }]
    await act(async () => { await vi.advanceTimersByTimeAsync(11_000) })
    expect(await screen.findByRole("listitem", { name: "added" })).toBeTruthy()
  })
})
