import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { MainInstancesSettings, WorkersSettings } from "../InstancePairingSettings"

vi.mock("../../hooks/useWebSocket", () => ({
  useWebSocket: () => ({ connected: true, send: vi.fn(), subscribe: () => () => {} }),
}))

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })
}

type Request = { method: string; url: string; body?: Record<string, unknown> }
let requests: Request[]
let workers: Record<string, unknown>[]
let mains: Record<string, unknown>[]
let pairAnswer: Response | null

beforeEach(() => {
  requests = []
  workers = [{
    instance_id: "srv-0b8e2a91d4c7f356", address: "https://vm-1.lan:3080", name: "vm-1", main_name: "workstation",
    sandbox: true, handoff_version: 1, paired_at: "2026-11-03T09:00:00Z",
  }]
  mains = [{ name: "workstation", instance_id: "srv-2f7d61c0ab493e85", paired_at: "2026-11-03T09:00:00Z", last_pull_at: "2026-11-05T08:58:00Z", open_handoffs: 0 }]
  pairAnswer = null
  vi.stubGlobal("fetch", vi.fn(async (url: string, init?: RequestInit) => {
    const method = init?.method ?? "GET"
    const body = init?.body ? JSON.parse(String(init.body)) : undefined
    requests.push({ method, url: String(url), body })
    if (String(url).endsWith("/api/settings/workers")) {
      if (method === "POST") {
        if (pairAnswer) return pairAnswer
        const paired = { instance_id: "srv-1111222233334444", address: "https://" + body.address, name: "vm-2", main_name: body.name, sandbox: false, handoff_version: 1, paired_at: "2026-11-05T09:00:00Z" }
        workers = [...workers, paired]
        return json(paired)
      }
      return json({ workers, instance_id: "srv-2f7d61c0ab493e85", main_name: "workstation" })
    }
    if (String(url).endsWith("/api/settings/mains")) return json({ mains })
    if (String(url).includes("/api/settings/mains/") && method === "DELETE") {
      mains = mains.filter((m) => !String(url).endsWith(String(m.instance_id)))
      return json({ status: "ok" })
    }
    if (String(url).endsWith("/api/pairing")) {
      return json({ state: "open", purpose: "main", phrase: ["bucket", "ladle", "spree", "oyster"], address: "https://vm-1.lan:3080",
        expires_at: new Date(Date.now() + 5 * 60_000).toISOString(), attempts_left: 5 })
    }
    return json({ error: "nope" }, 404)
  }))
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe("WorkersSettings", () => {
  it("lists each worker's address, name, instance ID, last pull and sandbox", async () => {
    render(<WorkersSettings baseUrl="" />)
    const vm = await screen.findByRole("listitem", { name: "vm-1" })
    expect(within(vm).getByText("https://vm-1.lan:3080")).toBeTruthy()
    expect(within(vm).getByText("srv-0b8e2a91d4c7f356")).toBeTruthy()
    expect(within(vm).getByText(/not pulled yet/i)).toBeTruthy()
    expect(within(vm).getByText(/sandbox on/i)).toBeTruthy()
  })

  it("says when each worker was last pulled, and which one is down or refuses this main", async () => {
    workers = [
      { ...workers[0], last_pull_at: "2026-11-05T08:58:00Z" },
      { ...workers[0], instance_id: "srv-2", name: "vm-2", last_pull_at: "2026-11-05T08:00:00Z", unreachable_since: "2026-11-05T08:01:00Z" },
      { ...workers[0], instance_id: "srv-3", name: "vm-3", refused_at: "2026-11-05T08:30:00Z" },
    ]
    render(<WorkersSettings baseUrl="" />)
    expect(within(await screen.findByRole("listitem", { name: "vm-1" })).getByText(/^Pulled /)).toBeTruthy()
    expect(within(screen.getByRole("listitem", { name: "vm-2" })).getByText(/^Unreachable since /)).toBeTruthy()
    expect(within(screen.getByRole("listitem", { name: "vm-3" })).getByText(/no longer accepts this main; pair again/i)).toBeTruthy()
  })

  it("explains handing off before any worker is paired", async () => {
    workers = []
    render(<WorkersSettings baseUrl="" />)
    expect(await screen.findByText(/hand a task off to a worker/i)).toBeTruthy()
    expect(screen.getByText(/the task stays here, read-only/i)).toBeTruthy()
  })

  it("adds a worker by its address, its phrase and this main's name", async () => {
    render(<WorkersSettings baseUrl="" />)
    await screen.findByRole("listitem", { name: "vm-1" })
    fireEvent.click(screen.getByRole("button", { name: "Add a worker" }))
    const name = screen.getByLabelText("This main's name on the worker") as HTMLInputElement
    expect(name.value).toBe("workstation")
    fireEvent.change(screen.getByLabelText("Worker address"), { target: { value: "vm-2.lan:3080" } })
    fireEvent.change(screen.getByLabelText("Phrase"), { target: { value: "bucket ladle spree oyster" } })
    fireEvent.click(screen.getByRole("button", { name: "Pair" }))
    expect(await screen.findByRole("listitem", { name: "vm-2" })).toBeTruthy()
    const post = requests.find((r) => r.method === "POST")
    expect(post?.body).toEqual({ address: "vm-2.lan:3080", phrase: "bucket ladle spree oyster", name: "workstation" })
    expect(screen.getByRole("status").textContent).toMatch(/Paired with vm-2/)
    expect(within(screen.getByRole("listitem", { name: "vm-2" })).getByText(/sandbox off/i)).toBeTruthy()
  })

  it("says why a worker did not pair", async () => {
    pairAnswer = json({ error: "update the worker: it cannot pair a main instance (https://vm-2.lan:3080 runs Legato v0.9.0)" }, 409)
    render(<WorkersSettings baseUrl="" />)
    await screen.findByRole("listitem", { name: "vm-1" })
    fireEvent.click(screen.getByRole("button", { name: "Add a worker" }))
    fireEvent.change(screen.getByLabelText("Worker address"), { target: { value: "vm-2.lan:3080" } })
    fireEvent.change(screen.getByLabelText("Phrase"), { target: { value: "bucket ladle spree oyster" } })
    fireEvent.click(screen.getByRole("button", { name: "Pair" }))
    expect((await screen.findByRole("alert")).textContent).toMatch(/update the worker/)
    expect(screen.queryByRole("listitem", { name: "vm-2" })).toBeNull()
  })
})

describe("MainInstancesSettings", () => {
  it("lists each main's name, instance ID, when it paired, its last pull and open handoffs", async () => {
    render(<MainInstancesSettings baseUrl="" />)
    const main = await screen.findByRole("listitem", { name: "workstation" })
    expect(within(main).getByText("srv-2f7d61c0ab493e85")).toBeTruthy()
    expect(within(main).getByText(/paired/i)).toBeTruthy()
    expect(within(main).getByText(/last pulled/i)).toBeTruthy()
    expect(within(main).getByText(/0 open handoffs/)).toBeTruthy()
  })

  it("unpairs a main after asking", async () => {
    render(<MainInstancesSettings baseUrl="" />)
    const main = await screen.findByRole("listitem", { name: "workstation" })
    fireEvent.click(within(main).getByRole("button", { name: "Unpair workstation" }))
    expect(screen.getByText(/revokes its token/)).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Unpair" }))
    await waitFor(() => expect(screen.queryByRole("listitem", { name: "workstation" })).toBeNull())
    expect(requests.some((r) => r.method === "DELETE" && r.url.endsWith("/api/settings/mains/srv-2f7d61c0ab493e85"))).toBe(true)
  })

  it("opens a main pairing request with a phrase, this worker's address and a countdown", async () => {
    render(<MainInstancesSettings baseUrl="" />)
    await screen.findByRole("listitem", { name: "workstation" })
    fireEvent.click(screen.getByRole("button", { name: "Pair a main instance" }))
    expect(await screen.findByText("bucket")).toBeTruthy()
    expect(screen.getByText("https://vm-1.lan:3080")).toBeTruthy()
    expect(screen.getByText(/Expires in [45]:\d\d/)).toBeTruthy()
    const open = requests.find((r) => r.url.endsWith("/api/pairing") && r.method === "POST")
    expect(open?.body).toEqual({ purpose: "main" })
  })
})
