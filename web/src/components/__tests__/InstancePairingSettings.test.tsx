import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { MainInstancesSettings, WorkersSettings } from "../InstancePairingSettings"

vi.mock("../../hooks/useWebSocket", () => ({
  useWebSocket: () => ({ connected: true, send: vi.fn(), subscribe: () => () => {} }),
}))

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })
}

type Request = { method: string; url: string; body?: Record<string, unknown>; auth?: string }
let requests: Request[]
let workers: Record<string, unknown>[]
let mains: Record<string, unknown>[]
let pairAnswer: Response | null
let pairingAnswer: Response | null

beforeEach(() => {
  requests = []
  workers = [{
    instance_id: "srv-0b8e2a91d4c7f356", address: "https://vm-1.lan:3080", name: "vm-1", main_name: "workstation",
    sandbox: true, handoff_version: 1, paired_at: "2026-11-03T09:00:00Z",
  }]
  mains = [{ name: "workstation", instance_id: "srv-2f7d61c0ab493e85", paired_at: "2026-11-03T09:00:00Z", last_pull_at: "2026-11-05T08:58:00Z", open_handoffs: 0 }]
  pairAnswer = null
  pairingAnswer = null
  vi.stubGlobal("fetch", vi.fn(async (url: string, init?: RequestInit) => {
    const method = init?.method ?? "GET"
    const body = init?.body ? JSON.parse(String(init.body)) : undefined
    requests.push({ method, url: String(url), body, auth: (init?.headers as Record<string, string> | undefined)?.Authorization })
    if (String(url).endsWith("/api/settings/workers")) {
      if (method === "POST") {
        if (pairAnswer) return pairAnswer
        const paired = { instance_id: "srv-1111222233334444", address: "https://" + body.address, name: "vm-2", main_name: body.name, sandbox: false, handoff_version: 1, paired_at: "2026-11-05T09:00:00Z" }
        workers = [...workers, paired]
        return json(paired)
      }
      return json({ workers, instance_id: "srv-2f7d61c0ab493e85", main_name: "workstation" })
    }
    if (String(url).includes("/api/settings/workers/") && method === "DELETE") {
      workers = workers.filter((w) => !String(url).endsWith(String(w.instance_id)))
      return json({ removed: { worker: "vm-1", recalled: [], taken_back: [{ task_id: "T-1" }], left: false } })
    }
    if (String(url).endsWith("/api/settings/mains")) return json({ mains })
    if (String(url).endsWith("/reeve") && method === "PUT") return json({ enabled: body.enabled })
    if (String(url).includes("/api/settings/mains/") && method === "DELETE") {
      mains = mains.filter((m) => !String(url).endsWith(String(m.instance_id)))
      return json({ status: "ok" })
    }
    if (String(url).endsWith("/api/pairing")) {
      if (pairingAnswer) return pairingAnswer
      return json({ state: "open", purpose: "main", phrase: ["bucket", "ladle", "spree", "oyster"], address: "https://vm-1.lan:3080",
        expires_at: new Date(Date.now() + 5 * 60_000).toISOString(), attempts_left: 5 })
    }
    return json({ error: "nope" }, 404)
  }))
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
  localStorage.clear()
  delete (window as { legatoShell?: unknown }).legatoShell
  delete (window as { ReactNativeWebView?: unknown }).ReactNativeWebView
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

  it("removes a worker after warning how many away tasks it holds", async () => {
    workers = [{ ...workers[0], away: 2 }]
    render(<WorkersSettings baseUrl="" />)
    const vm = await screen.findByRole("listitem", { name: "vm-1" })
    fireEvent.click(within(vm).getByRole("button", { name: "Remove vm-1" }))
    expect(within(vm).getByText(/vm-1 holds 2 away tasks/)).toBeTruthy()
    expect(requests.some((r) => r.method === "DELETE")).toBe(false)
    fireEvent.click(within(vm).getByRole("button", { name: "Remove" }))
    await waitFor(() => expect(requests.some((r) => r.method === "DELETE" && r.url === "/api/settings/workers/srv-0b8e2a91d4c7f356")).toBe(true))
    expect((await screen.findByRole("status")).textContent).toContain("1 taken back")
    await waitFor(() => expect(screen.queryByRole("listitem", { name: "vm-1" })).toBeNull())
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

  it("lets a main's Reeve message this one's, or stops it", async () => {
    render(<MainInstancesSettings baseUrl="https://worker.lan:3080" />)
    const toggle = await screen.findByRole("checkbox", { name: "Let the Reeve on workstation message this one" })
    expect((toggle as HTMLInputElement).checked).toBe(false)
    fireEvent.click(toggle)
    await waitFor(() => expect(requests.some((r) => r.method === "PUT" && r.url.endsWith("/api/settings/mains/srv-2f7d61c0ab493e85/reeve") && r.body?.enabled === true)).toBe(true))
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

// The Legato app's shell (desktop or Android) as the page sees it: it says it
// can pair workers (workers: 1) and answers each message with a
// legato:shell-worker event carrying the message's id.
type Posted = Record<string, unknown>
function fakeShell(answer: (message: Posted) => Record<string, unknown> | null, shell: Record<string, unknown> = { workers: 1, approval: 1, key: "k1" }) {
  const posted: Posted[] = []
  ;(window as { legatoShell?: unknown }).legatoShell = shell
  ;(window as { ReactNativeWebView?: unknown }).ReactNativeWebView = {
    postMessage: (text: string) => {
      const message = JSON.parse(text) as Posted
      posted.push(message)
      const detail = answer(message)
      if (detail) setTimeout(() => window.dispatchEvent(new CustomEvent("legato:shell-worker", { detail: { id: message.id, ...detail } })), 0)
    },
  }
  return posted
}

const appServers = [{ key: "srv-a", name: "legato-1", url: "https://legato-1.lan:3080" }, { key: "srv-b", name: "legato-2", url: "https://legato-2.lan:3080" }]
const opened = { address: "https://legato-1.lan:3080", phrase: ["bucket", "ladle", "spree", "oyster"], expires_at: "2026-11-05T09:05:00Z" }

async function openAddWorker(baseUrl = "") {
  render(<WorkersSettings baseUrl={baseUrl} />)
  await screen.findByRole("listitem", { name: "vm-1" })
  fireEvent.click(screen.getByRole("button", { name: "Add a worker" }))
}

describe("Add a worker from this app", () => {
  it("offers the app's other servers, which the shell lists", async () => {
    const posted = fakeShell((m) => (m.type === "workerCandidates" ? { servers: appServers } : null))
    await openAddWorker()
    const list = await screen.findByRole("list", { name: "From this app" })
    expect(within(list).getByRole("button", { name: /legato-1/ })).toBeTruthy()
    expect(within(list).getByRole("button", { name: /legato-2/ })).toBeTruthy()
    expect(posted[0]).toMatchObject({ type: "workerCandidates", legatoShell: "workerCandidates", key: "k1" })
    expect(typeof posted[0].id).toBe("string")
    // The manual form stays below as the fallback.
    expect(screen.getByLabelText("Worker address")).toBeTruthy()
  })

  it("pairs the chosen server after the shell's confirm, with the address and phrase the shell opened", async () => {
    const posted = fakeShell((m) => (m.type === "workerCandidates" ? { servers: appServers } : m.type === "openWorkerPairing" ? opened : null))
    await openAddWorker()
    fireEvent.click(await screen.findByRole("button", { name: /legato-1/ }))
    expect((await screen.findByRole("status")).textContent).toMatch(/Paired with vm-2/)
    expect(posted[1]).toMatchObject({ type: "openWorkerPairing", legatoShell: "openWorkerPairing", key: "k1", server: "srv-a", main: "workstation" })
    const post = requests.find((r) => r.method === "POST" && r.url.endsWith("/api/settings/workers"))
    expect(post?.body).toEqual({ address: "https://legato-1.lan:3080", phrase: "bucket ladle spree oyster", name: "workstation" })
    // The page never asks another server for anything.
    expect(requests.some((r) => r.url.includes("legato-1.lan"))).toBe(false)
  })

  it("does nothing more when the person says no in the shell", async () => {
    fakeShell((m) => (m.type === "workerCandidates" ? { servers: appServers } : { cancelled: true }))
    await openAddWorker()
    fireEvent.click(await screen.findByRole("button", { name: /legato-1/ }))
    await waitFor(() => expect((screen.getByRole("button", { name: /legato-1/ }) as HTMLButtonElement).disabled).toBe(false))
    expect(screen.queryByRole("alert")).toBeNull()
    expect(requests.some((r) => r.method === "POST")).toBe(false)
    expect((screen.getByLabelText("Worker address") as HTMLInputElement).value).toBe("")
  })

  it("shows the server's words when it cannot open a request, and fills in its address", async () => {
    fakeShell((m) => (m.type === "workerCandidates" ? { servers: appServers } : { error: "This server is too old to be a worker. Update Legato on it, then try again." }))
    await openAddWorker()
    fireEvent.click(await screen.findByRole("button", { name: /legato-1/ }))
    expect((await screen.findByRole("alert")).textContent).toMatch(/too old to be a worker/)
    expect((screen.getByLabelText("Worker address") as HTMLInputElement).value).toBe("https://legato-1.lan:3080")
  })

  it("says so when the main cannot reach the worker, and fills in its address", async () => {
    pairAnswer = json({ error: "the worker at https://legato-1.lan:3080 does not answer" }, 502)
    fakeShell((m) => (m.type === "workerCandidates" ? { servers: appServers } : opened))
    await openAddWorker()
    fireEvent.click(await screen.findByRole("button", { name: /legato-1/ }))
    expect((await screen.findByRole("alert")).textContent).toMatch(/does not answer/)
    expect((screen.getByLabelText("Worker address") as HTMLInputElement).value).toBe("https://legato-1.lan:3080")
  })

  it("offers nothing from the app when the shell knows no other server", async () => {
    const posted = fakeShell(() => ({ servers: [] }))
    await openAddWorker()
    await waitFor(() => expect(posted.length).toBe(1))
    expect(screen.queryByRole("list", { name: "From this app" })).toBeNull()
    expect(screen.getByLabelText("Worker address")).toBeTruthy()
  })
})

describe("Add a worker from this browser", () => {
  beforeEach(() => {
    localStorage.setItem("legato:servers", JSON.stringify([
      { name: "main", url: "https://main.lan:3080" },
      { name: "legato-1", url: "https://legato-1.lan:3080" },
      { name: "no-token", url: "https://legato-3.lan:3080" },
    ]))
    localStorage.setItem("legato:token:https://main.lan:3080", "tok-main")
    localStorage.setItem("legato:token:https://legato-1.lan:3080", "tok-1")
  })

  it("offers the browser's servers that have a token, never the one in front", async () => {
    await openAddWorker("https://main.lan:3080")
    const list = await screen.findByRole("list", { name: "From this browser" })
    expect(within(list).getAllByRole("button").map((b) => b.textContent)).toEqual([expect.stringContaining("legato-1")])
  })

  it("opens the request on the chosen server after a confirm, then pairs it", async () => {
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(true)
    await openAddWorker("https://main.lan:3080")
    fireEvent.click(await screen.findByRole("button", { name: /legato-1/ }))
    expect((await screen.findByRole("status")).textContent).toMatch(/Paired with vm-2/)
    expect(confirm.mock.calls[0][0]).toMatch(/^Make legato-1 a worker of workstation\? workstation will be able to hand tasks to legato-1/)
    const open = requests.find((r) => r.url === "https://legato-1.lan:3080/api/pairing")
    expect(open).toMatchObject({ method: "POST", body: { purpose: "main" }, auth: "Bearer tok-1" })
    const post = requests.find((r) => r.method === "POST" && r.url === "https://main.lan:3080/api/settings/workers")
    expect(post).toMatchObject({ body: { address: "https://legato-1.lan:3080", phrase: "bucket ladle spree oyster", name: "workstation" }, auth: "Bearer tok-main" })
  })

  it("says it could not reach a server that does not answer", async () => {
    vi.spyOn(window, "confirm").mockReturnValue(true)
    const answer = fetch as unknown as ReturnType<typeof vi.fn>
    const inner = answer.getMockImplementation() as (url: string, init?: RequestInit) => Promise<Response>
    answer.mockImplementation(async (url: string, init?: RequestInit) => {
      if (String(url).startsWith("https://legato-1.lan:3080")) throw new TypeError("Failed to fetch")
      return inner(url, init)
    })
    await openAddWorker("https://main.lan:3080")
    fireEvent.click(await screen.findByRole("button", { name: /legato-1/ }))
    expect((await screen.findByRole("alert")).textContent).toBe("Could not reach legato-1: Failed to fetch")
    expect((screen.getByLabelText("Worker address") as HTMLInputElement).value).toBe("https://legato-1.lan:3080")
  })

  it("asks nothing of the server when the person does not confirm", async () => {
    vi.spyOn(window, "confirm").mockReturnValue(false)
    await openAddWorker("https://main.lan:3080")
    fireEvent.click(await screen.findByRole("button", { name: /legato-1/ }))
    await waitFor(() => expect((screen.getByRole("button", { name: /legato-1/ }) as HTMLButtonElement).disabled).toBe(false))
    expect(requests.some((r) => r.url.endsWith("/api/pairing"))).toBe(false)
    expect(screen.queryByRole("alert")).toBeNull()
  })

  it("says a server too old to pair a main is too old, and cancels the phone request it opened", async () => {
    vi.spyOn(window, "confirm").mockReturnValue(true)
    pairingAnswer = json({ state: "open", phrase: ["bucket", "ladle", "spree", "oyster"], expires_at: "2026-11-05T09:05:00Z", attempts_left: 5 })
    await openAddWorker("https://main.lan:3080")
    fireEvent.click(await screen.findByRole("button", { name: /legato-1/ }))
    expect((await screen.findByRole("alert")).textContent).toBe("This server is too old to be a worker. Update Legato on it, then try again.")
    expect(requests.some((r) => r.method === "DELETE" && r.url === "https://legato-1.lan:3080/api/pairing")).toBe(true)
    expect(requests.some((r) => r.method === "POST" && r.url.endsWith("/api/settings/workers"))).toBe(false)
    expect((screen.getByLabelText("Worker address") as HTMLInputElement).value).toBe("https://legato-1.lan:3080")
  })
})
