import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { AuditPage } from "../AuditPage"

const { apiFetch, server } = vi.hoisted(() => ({ apiFetch: vi.fn(), server: { baseUrl: "" } }))
vi.mock("../../hooks/useServer", () => ({ useServer: () => server }))
vi.mock("../../lib/api", () => ({ apiFetch }))

const read = (seq: number) => ({
  seq, at: "2026-10-29T22:39:5" + (seq % 10) + ".000Z", event: "use", credential: "github", operation: "api.read",
  resource: "github:autopilot-dev/autopilot", task_id: "AG-10102", task_title: "Comparable sales", execution_id: "ex-a", detail: "auto · TOKEN autopilot-dev/autopilot",
})
// A full page (100 rows) has older rows behind it.
const firstPage = [
  read(200), read(199), read(198),
  { seq: 197, at: "2026-10-29T14:22:26.000Z", event: "grant", credential: "proxmox_vms", operation: "ssh.login", resource: "ssh://cameron@psi.pavey.network", task_id: "yjpvdfmy", approval_id: "ap-e1e2d310", decided_by: "web" },
  ...Array.from({ length: 96 }, (_, i) => ({ seq: 196 - i, at: "2026-10-29T12:00:00.000Z", event: "request", credential: "github", operation: "git.push", task_id: "rehzkrch" })),
]
const olderPage = [{ seq: 4, at: "2026-10-29T09:54:21.000Z", event: "refuse", credential: "github", operation: "read", task_id: "AG-10102", detail: "no rule allows read" }]

function json(body: unknown) {
  return Promise.resolve(new Response(JSON.stringify(body), { status: 200 }))
}

function calls(path: string) {
  return apiFetch.mock.calls.map(([, called]) => called as string).filter((called) => called.startsWith(path))
}

let grants: { id: string; [key: string]: unknown }[] = []

describe("AuditPage", () => {
  beforeEach(() => {
    server.baseUrl = ""
    apiFetch.mockReset()
    grants = [
      { id: "cg-1", credential: "github", task_id: "AG-10102", task_title: "Comparable sales", execution_id: "ex-a", operations: ["pr.write"], resources: ["github:autopilot-dev/autopilot"], mode: "session", created_at: "2026-10-30T01:00:00Z" },
      { id: "cg-2", credential: "github", task_id: "AG-10102", execution_id: "ex-a", operations: ["api.read"], resources: ["github:autopilot-dev/autopilot"], mode: "auto", created_at: "2026-10-30T01:00:00Z" },
    ]
    apiFetch.mockImplementation((_: string, path: string, init?: RequestInit) => {
      if (path.startsWith("/api/credentials/grants/") && init?.method === "DELETE") {
        grants = grants.filter((g) => !path.endsWith(g.id))
        return Promise.resolve(new Response(null, { status: 204 }))
      }
      if (path.startsWith("/api/credentials/grants")) return json({ grants })
      if (path.startsWith("/api/credentials/audit/verify")) return json({ intact: true })
      if (path.startsWith("/api/credentials/audit")) return json({ entries: new URLSearchParams(path.split("?")[1]).get("before") ? olderPage : firstPage })
      if (path.startsWith("/api/credentials")) return json({ credentials: [{ name: "github" }, { name: "proxmox_vms" }] })
      return json({})
    })
  })
  afterEach(() => { cleanup(); vi.restoreAllMocks() })

  it("shows which agent used which credential for what, folding a burst of the same use", async () => {
    render(<MemoryRouter><AuditPage /></MemoryRouter>)
    const burst = await screen.findByRole("row", { name: /AG-10102.*api\.read/ })
    expect(within(burst).getByText("Comparable sales")).toBeTruthy()
    expect(within(burst).getByText("×3")).toBeTruthy()
    expect(within(burst).getByText("github:autopilot-dev/autopilot")).toBeTruthy()
    const ssh = screen.getByRole("row", { name: /yjpvdfmy.*ssh\.login/ })
    expect(within(ssh).getByText("grant")).toBeTruthy()
    expect(within(ssh).getByText(/ap-e1e2d310/)).toBeTruthy()
    expect(await screen.findByText(/Chain intact/)).toBeTruthy()
  })

  it("lists the grants the person gave, and revokes one so the agent must ask again", async () => {
    render(<MemoryRouter><AuditPage /></MemoryRouter>)
    const standing = await screen.findByRole("region", { name: "Standing grants" })
    const row = await within(standing).findByRole("listitem")
    expect(within(standing).getAllByRole("listitem")).toHaveLength(1) // not the automatic read
    expect(within(row).getByText("Comparable sales")).toBeTruthy()
    expect(within(row).getByText(/pr\.write/)).toBeTruthy()
    expect(within(row).getByText("this session")).toBeTruthy()
    fireEvent.click(within(row).getByRole("button", { name: "Revoke" }))
    await vi.waitFor(() => expect(apiFetch.mock.calls.some(([, path, init]) => path === "/api/credentials/grants/cg-1" && init?.method === "DELETE")).toBe(true))
    expect(await within(standing).findByText(/No standing grants/)).toBeTruthy()
  })

  it("asks again for one credential's rows when it is chosen", async () => {
    render(<MemoryRouter><AuditPage /></MemoryRouter>)
    await screen.findByRole("row", { name: /AG-10102/ })
    fireEvent.change(screen.getByRole("combobox", { name: "Credential" }), { target: { value: "proxmox_vms" } })
    await vi.waitFor(() => expect(calls("/api/credentials/audit?").some((c) => c.includes("credential=proxmox_vms"))).toBe(true))
  })

  it("loads older rows from where the page ends", async () => {
    render(<MemoryRouter><AuditPage /></MemoryRouter>)
    await screen.findByRole("row", { name: /AG-10102/ })
    fireEvent.click(screen.getByRole("button", { name: "Load older" }))
    expect(await screen.findByRole("row", { name: /refuse/ })).toBeTruthy()
    expect(calls("/api/credentials/audit?").some((c) => c.includes("before=101"))).toBe(true)
  })
  it("ignores an older page after changing the event filter", async () => {
    const fallback = apiFetch.getMockImplementation()!
    let finish!: (response: Response) => void
    apiFetch.mockImplementation((base, path, init) => path.includes("before=")
      ? new Promise<Response>((resolve) => { finish = resolve })
      : path.includes("event=refuse") ? json({ entries: olderPage }) : fallback(base, path, init))
    render(<MemoryRouter><AuditPage /></MemoryRouter>)
    fireEvent.click(await screen.findByRole("button", { name: "Load older" }))
    fireEvent.change(screen.getByRole("combobox", { name: "Event" }), { target: { value: "refuse" } })
    await screen.findByRole("row", { name: /refuse/ })
    await act(async () => { finish(await json({ entries: firstPage })) })
    expect(screen.queryByRole("row", { name: /yjpvdfmy/ })).toBeNull()
    expect(screen.queryByRole("button", { name: "Load older" })).toBeNull()
  })

  it("ends failed initial loads and retries the audit log and grants", async () => {
    const fallback = apiFetch.getMockImplementation()!
    let failing = true
    apiFetch.mockImplementation((base, path, init) => failing && (path.includes("audit?") || path === "/api/credentials/grants")
      ? Promise.resolve(new Response("Service unavailable", { status: 503 })) : fallback(base, path, init))
    render(<MemoryRouter><AuditPage /></MemoryRouter>)
    await screen.findAllByRole("alert")
    expect(screen.queryByText("Loading the audit log…")).toBeNull()
    expect(screen.queryByText("Loading grants…")).toBeNull()
    failing = false
    fireEvent.click(screen.getByRole("button", { name: "Refresh" }))
    fireEvent.click(screen.getByRole("button", { name: "Retry grants" }))
    await screen.findByRole("row", { name: /yjpvdfmy/ })
    await within(screen.getByRole("region", { name: "Standing grants" })).findByRole("button", { name: "Revoke" })
    expect(screen.queryAllByRole("alert")).toHaveLength(0)
  })

  it("does not refresh the previous server after its pending revoke completes", async () => {
    const fallback = apiFetch.getMockImplementation()!
    let finish!: (response: Response) => void
    apiFetch.mockImplementation((base, path, init) => init?.method === "DELETE"
      ? new Promise<Response>((resolve) => { finish = resolve }) : fallback(base, path, init))
    const view = render(<MemoryRouter><AuditPage /></MemoryRouter>)
    fireEvent.click(await screen.findByRole("button", { name: "Revoke" }))
    server.baseUrl = "https://other.test"
    view.rerender(<MemoryRouter><AuditPage /></MemoryRouter>)
    await screen.findByRole("button", { name: "Revoke" })
    const count = apiFetch.mock.calls.length
    await act(async () => { finish(new Response(null, { status: 204 })) })
    expect(apiFetch).toHaveBeenCalledTimes(count)
    expect(screen.getByRole("button", { name: "Revoke" }).hasAttribute("disabled")).toBe(false)
  })

  it("reports metadata failures and retries them with Refresh", async () => {
    const fallback = apiFetch.getMockImplementation()!
    let failing = true
    apiFetch.mockImplementation((base, path, init) => failing && (path === "/api/credentials" || path.endsWith("/verify"))
      ? Promise.resolve(new Response("Metadata unavailable", { status: 503 })) : fallback(base, path, init))
    render(<MemoryRouter><AuditPage /></MemoryRouter>)
    expect(await screen.findByText(/Could not load credentials: Metadata unavailable/)).toBeTruthy()
    expect(await screen.findByText(/Could not verify audit chain: Metadata unavailable/)).toBeTruthy()
    failing = false
    fireEvent.click(screen.getByRole("button", { name: "Refresh" }))
    await screen.findByText(/Chain intact/)
    expect(screen.getByRole("option", { name: "proxmox_vms" })).toBeTruthy()
    expect(screen.queryAllByRole("alert")).toHaveLength(0)
  })

  it("does not restore a revoked grant from an earlier poll", async () => {
    const timers = vi.spyOn(window, "setInterval")
    const fallback = apiFetch.getMockImplementation()!
    const previous = [...grants]
    let finish!: (response: Response) => void
    let delay = false
    apiFetch.mockImplementation((base, path, init) => {
      if (delay && path === "/api/credentials/grants") {
        delay = false
        return new Promise<Response>((resolve) => { finish = resolve })
      }
      return fallback(base, path, init)
    })
    render(<MemoryRouter><AuditPage /></MemoryRouter>)
    await screen.findByRole("button", { name: "Revoke" })
    delay = true
    await act(async () => { timers.mock.calls.forEach(([callback]) => { if (typeof callback === "function") callback() }) })
    fireEvent.click(screen.getByRole("button", { name: "Revoke" }))
    await screen.findByText("No standing grants.")
    await act(async () => { finish(await json({ grants: previous })) })
    expect(screen.queryByRole("button", { name: "Revoke" })).toBeNull()
  })

  it("keeps rows, credentials, chain and grants on the selected server", async () => {
    const fallback = apiFetch.getMockImplementation()!
    const pending: (() => Promise<void>)[] = []
    apiFetch.mockImplementation((base, path, init) => base === ""
      ? new Promise<Response>((resolve) => { pending.push(async () => resolve(await fallback(base, path, init))) })
      : path.includes("audit?") ? json({ entries: olderPage })
      : path.endsWith("/verify") ? json({ intact: false, broken_at: 9 })
      : path.endsWith("/grants") ? json({ grants: [] })
      : json({ credentials: [{ name: "other-key" }] }))
    const view = render(<MemoryRouter><AuditPage /></MemoryRouter>)
    server.baseUrl = "https://other.test"
    view.rerender(<MemoryRouter><AuditPage /></MemoryRouter>)
    await screen.findByRole("row", { name: /refuse/ })
    await act(async () => { await Promise.all(pending.map((finish) => finish())) })
    expect(screen.queryByRole("row", { name: /yjpvdfmy/ })).toBeNull()
    expect(screen.queryByRole("option", { name: "github" })).toBeNull()
    expect(screen.getByRole("option", { name: "other-key" })).toBeTruthy()
    expect(screen.getByText(/Chain broken at #9/)).toBeTruthy()
    expect(screen.queryByRole("button", { name: "Revoke" })).toBeNull()
  })

  it.each([false, true])("ignores a superseded refresh during pagination (failure: %s)", async (fail) => {
    const fallback = apiFetch.getMockImplementation()!
    let finish!: (response: Response) => void
    let delay = false
    apiFetch.mockImplementation((base, path, init) => {
      if (delay && path.includes("audit?") && !path.includes("before=")) {
        delay = false
        return new Promise<Response>((resolve) => { finish = resolve })
      }
      return fallback(base, path, init)
    })
    render(<MemoryRouter><AuditPage /></MemoryRouter>)
    await screen.findByRole("button", { name: "Load older" })
    delay = true
    fireEvent.click(screen.getByRole("button", { name: "Refresh" }))
    fireEvent.click(screen.getByRole("button", { name: "Load older" }))
    await screen.findByRole("row", { name: /refuse/ })
    await act(async () => { finish(fail ? new Response("Old failure", { status: 503 }) : await json({ entries: firstPage })) })
    expect(screen.getByRole("row", { name: /refuse/ })).toBeTruthy()
    expect(screen.queryAllByRole("alert")).toHaveLength(0)
    expect(screen.queryByRole("button", { name: "Load older" })).toBeNull()
  })

  it("refreshes the current filter when an earlier revoke completes", async () => {
    const fallback = apiFetch.getMockImplementation()!
    let finish!: (response: Response) => void
    apiFetch.mockImplementation((base, path, init) => init?.method === "DELETE"
      ? new Promise<Response>((resolve) => { finish = resolve })
      : path.includes("event=refuse") ? json({ entries: olderPage }) : fallback(base, path, init))
    render(<MemoryRouter><AuditPage /></MemoryRouter>)
    fireEvent.click(await screen.findByRole("button", { name: "Revoke" }))
    fireEvent.change(screen.getByRole("combobox", { name: "Event" }), { target: { value: "refuse" } })
    await screen.findByRole("row", { name: /refuse/ })
    await act(async () => { finish(new Response(null, { status: 204 })) })
    expect(screen.getByRole("row", { name: /refuse/ })).toBeTruthy()
    expect(screen.queryByRole("row", { name: /yjpvdfmy/ })).toBeNull()
  })

})
