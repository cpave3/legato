import { QueryClientProvider } from "@tanstack/react-query"
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { createQueryClient } from "../../lib/query"
import { RemotePage } from "../RemotePage"
import type { JobClone } from "../../lib/jobClones"
import { QuerySyncProvider } from "../../components/QuerySyncProvider"
import type { WSMessage } from "../../hooks/useWebSocket"

let server = { baseUrl: "", activeServerName: "Local" }
vi.mock("../../hooks/useServer", () => ({ useServer: () => server }))
let generation = 0
let onMessage: (message: WSMessage) => void
const subscribe = (listener: typeof onMessage) => { onMessage = listener; return () => {} }
vi.mock("../../hooks/useWebSocket", () => ({ useWebSocket: () => ({ connectionGeneration: generation, subscribe }) }))

const clone: JobClone = { id: 3, repo: "acme/widget", path: "/data/jobs/ex-1/widget", branch: "stack/two", task_ids: ["t1", "deleted-task"], busy: false, last_used_at: "2026-11-02T10:00:00Z", unpushed: 2, remove_after: "2026-11-09T10:00:00Z" }
function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })
}
function page(sync = false) {
  const client = createQueryClient()
  const tree = () => <QueryClientProvider client={client}><MemoryRouter>{sync ? <QuerySyncProvider><RemotePage /></QuerySyncProvider> : <RemotePage />}</MemoryRouter></QueryClientProvider>
  return { ...render(tree()), tree, client }
}
beforeEach(() => {
  server = { baseUrl: "", activeServerName: "Local" }
  generation = 0
  vi.stubGlobal("fetch", vi.fn(async () => json({ clones: [clone] })))
})
afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks() })

it("lists managed clone details without implying a clean working tree", async () => {
  page()
  expect(screen.getByText("Managed clones on Local")).toBeTruthy()
  const row = await screen.findByRole("listitem", { name: "acme/widget" })
  expect(within(row).getByText("stack/two")).toBeTruthy()
  expect(within(row).getByText(clone.path)).toBeTruthy()
  expect(within(row).getByRole("link", { name: "t1" }).getAttribute("href")).toBe("/board?highlight_task=t1")
  expect(within(row).getByRole("link", { name: "deleted-task" })).toBeTruthy()
  expect(within(row).getByText("2 local tips absent from cache")).toBeTruthy()
  expect(within(row).getByText("Working-tree changes: not checked")).toBeTruthy()
  expect(within(row).getByText(/Scheduled removal:/)).toBeTruthy()
  expect(screen.getByText(/Inventory updated/)).toBeTruthy()
})

it("separates loading and failures from empty inventory and retries", async () => {
  let resolve!: (value: Response) => void
  vi.mocked(fetch).mockImplementationOnce(() => new Promise<Response>((done) => { resolve = done }))
  page()
  expect(screen.getByText("Loading clones…")).toBeTruthy()
  expect(screen.queryByText("No managed clones on this server.")).toBeNull()
  resolve(json({ error: "Repository service unavailable" }, 503))
  expect(await screen.findByRole("alert")).toHaveProperty("textContent", expect.stringContaining("Repository service unavailable"))
  vi.mocked(fetch).mockResolvedValueOnce(json({ clones: [] }))
  fireEvent.click(screen.getByRole("button", { name: "Retry" }))
  expect(await screen.findByText("No managed clones on this server.")).toBeTruthy()
})

it("keeps without deleting, then removes once with warnings and returns focus", async () => {
  vi.mocked(fetch).mockImplementation(async (_url, init) => init?.method === "DELETE" ? new Response(null, { status: 204 }) : json({ clones: [clone] }))
  page()
  const remove = await screen.findByRole("button", { name: "Remove acme/widget" })
  remove.focus()
  fireEvent.click(remove)
  const dialog = screen.getByRole("dialog", { name: "Remove this clone?" })
  expect(within(dialog).getByText(/including changed, untracked and ignored files/)).toBeTruthy()
  expect(within(dialog).getByText(clone.path)).toBeTruthy()
  fireEvent.click(within(dialog).getByRole("button", { name: "Keep clone" }))
  expect(document.activeElement).toBe(remove)
  expect(vi.mocked(fetch).mock.calls.filter(([, init]) => init?.method === "DELETE")).toHaveLength(0)
  fireEvent.click(remove)
  vi.mocked(fetch).mockImplementation(async (_url, init) => init?.method === "DELETE" ? new Response(null, { status: 204 }) : json({ clones: [] }))
  const confirm = screen.getByRole("button", { name: "Remove entire folder" })
  fireEvent.click(confirm)
  fireEvent.click(confirm)
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())
  expect(await screen.findByText("No managed clones on this server.")).toBeTruthy()
  expect(vi.mocked(fetch).mock.calls.filter(([, init]) => init?.method === "DELETE").map(([url]) => url)).toEqual(["/api/job-clones/3"])
  expect(document.activeElement).toBe(screen.getByRole("button", { name: "Refresh" }))
})

it.each([409, 404, 500])("preserves the clone and shows a recoverable delete error (%s)", async (status) => {
  const reason = status === 409 ? "clone busy" : status === 404 ? "clone not found" : "disk error"
  vi.mocked(fetch).mockImplementation(async (_url, init) => init?.method === "DELETE" ? json({ error: reason }, status) : json({ clones: [clone] }))
  page()
  fireEvent.click(await screen.findByRole("button", { name: "Remove acme/widget" }))
  fireEvent.click(screen.getByRole("button", { name: "Remove entire folder" }))
  expect(await screen.findByText(new RegExp(`Could not remove clone: ${reason}`))).toBeTruthy()
  expect(screen.getByRole("button", { name: "Remove entire folder" })).toHaveProperty("disabled", false)
  expect(screen.getByRole("listitem", { name: "acme/widget" })).toBeTruthy()
  fireEvent.click(screen.getByRole("button", { name: "Keep clone" }))
  vi.mocked(fetch).mockResolvedValue(json({ clones: status === 404 ? [] : [{ ...clone, busy: status === 409 }] }))
  fireEvent.click(screen.getByRole("button", { name: "Refresh" }))
  if (status === 404) expect(await screen.findByText("No managed clones on this server.")).toBeTruthy()
  else if (status === 409) {
    await screen.findByText("Agent working")
    expect(screen.getByRole("button", { name: "Remove acme/widget" })).toHaveProperty("disabled", true)
  } else expect(await screen.findByRole("button", { name: "Remove acme/widget" })).toHaveProperty("disabled", false)
})

it("shows partial problems as unknown, disables busy removal, and identifies detached HEAD", async () => {
  vi.mocked(fetch).mockResolvedValue(json({ clones: [
    { ...clone, branch: "", problem: "Cannot read clone refs", unpushed: 0, busy: true },
    { ...clone, id: 4, repo: "acme/detached", branch: "", unpushed: 0 },
  ] }))
  page()
  const row = await screen.findByRole("listitem", { name: "acme/widget" })
  expect(within(row).getByText("Branch unavailable")).toBeTruthy()
  expect(within(row).getByText("Cache comparison unavailable")).toBeTruthy()
  expect(within(row).getByText("Cannot read clone refs")).toBeTruthy()
  expect(within(row).queryByText("0 local tips absent from cache")).toBeNull()
  expect(within(row).getByRole("button", { name: "Remove acme/widget" })).toHaveProperty("disabled", true)
  expect(screen.getByText("Detached HEAD")).toBeTruthy()
})

it("keeps a pending deletion on its original server and closes confirmation when switching", async () => {
  let resolveDelete!: (response: Response) => void
  const inventories = { local: [clone], other: [{ ...clone, repo: "other/repo" }] }
  vi.mocked(fetch).mockImplementation(async (url, init) => {
    if (init?.method === "DELETE") return new Promise<Response>((done) => { resolveDelete = done })
    return json({ clones: String(url).startsWith("https://other") ? inventories.other : inventories.local })
  })
  const view = page()
  fireEvent.click(await screen.findByRole("button", { name: "Remove acme/widget" }))
  fireEvent.click(screen.getByRole("button", { name: "Remove entire folder" }))
  expect(await screen.findByRole("button", { name: "Removing…" })).toHaveProperty("disabled", true)
  expect(screen.getByRole("button", { name: "Keep clone" })).toHaveProperty("disabled", true)
  server = { baseUrl: "https://other", activeServerName: "Other" }
  view.rerender(view.tree())
  expect(screen.queryByRole("dialog")).toBeNull()
  expect(await screen.findByRole("listitem", { name: "other/repo" })).toBeTruthy()
  const otherRequests = vi.mocked(fetch).mock.calls.filter(([url]) => String(url).startsWith("https://other")).length
  resolveDelete(new Response(null, { status: 204 }))
  await waitFor(() => expect(view.client.getQueryData(["", "job-clones"])).toEqual([]))
  expect(screen.getByRole("listitem", { name: "other/repo" })).toBeTruthy()
  expect(vi.mocked(fetch).mock.calls.filter(([url]) => String(url).startsWith("https://other"))).toHaveLength(otherRequests)
  expect(vi.mocked(fetch).mock.calls.filter(([, init]) => init?.method === "DELETE").map(([url]) => url)).toEqual(["/api/job-clones/3"])
})

it("does not display a late inventory response from another server", async () => {
  let resolve!: (response: Response) => void
  vi.mocked(fetch).mockImplementation(async (url) => String(url).startsWith("https://other") ? json({ clones: [] }) : new Promise<Response>((done) => { resolve = done }))
  const view = page()
  server = { baseUrl: "https://other", activeServerName: "Other" }
  view.rerender(view.tree())
  expect(await screen.findByText("No managed clones on this server.")).toBeTruthy()
  resolve(json({ clones: [clone] }))
  await waitFor(() => expect(view.client.getQueryData(["", "job-clones"])).toEqual([clone]))
  expect(screen.queryByRole("listitem")).toBeNull()
})

it("refreshes changed files only as an inventory snapshot, including on page reentry", async () => {
  const view = page()
  await screen.findByRole("listitem", { name: "acme/widget" })
  vi.mocked(fetch).mockResolvedValue(json({ clones: [{ ...clone, unpushed: 4 }] }))
  fireEvent.click(screen.getByRole("button", { name: "Refresh" }))
  expect(await screen.findByText("4 local tips absent from cache")).toBeTruthy()
  vi.mocked(fetch).mockResolvedValue(json({ clones: [] }))
  view.rerender(<QueryClientProvider client={view.client}><MemoryRouter><div>Other page</div></MemoryRouter></QueryClientProvider>)
  view.rerender(view.tree())
  expect(await screen.findByText("No managed clones on this server.")).toBeTruthy()
})

it("refreshes the visible inventory on agent events and reconnect", async () => {
  const view = page(true)
  await screen.findByRole("listitem", { name: "acme/widget" })
  generation = 1
  view.rerender(view.tree())
  vi.mocked(fetch).mockResolvedValue(json({ clones: [{ ...clone, busy: true }] }))
  onMessage({ type: "agents_changed" })
  expect(await screen.findByText("Agent working")).toBeTruthy()
  vi.mocked(fetch).mockResolvedValue(json({ clones: [] }))
  generation = 2
  view.rerender(view.tree())
  expect(await screen.findByText("No managed clones on this server.")).toBeTruthy()
})

it("does not relabel a failed post-delete refresh as a successful inventory check", async () => {
  page()
  fireEvent.click(await screen.findByRole("button", { name: "Remove acme/widget" }))
  const checkedAt = screen.getByText(/Inventory updated at/).textContent
  const later = Date.now() + 3_600_000
  vi.spyOn(Date, "now").mockReturnValue(later)
  vi.mocked(fetch).mockImplementation(async (_url, init) => init?.method === "DELETE" ? new Response(null, { status: 204 }) : json({ error: "inventory offline" }, 503))
  fireEvent.click(screen.getByRole("button", { name: "Remove entire folder" }))
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())
  expect(await screen.findByRole("alert")).toHaveProperty("textContent", expect.stringContaining("inventory offline"))
  expect(screen.getByText(/Inventory updated at/).textContent).toBe(checkedAt)
})

it("updates open confirmation from newer inventory and stops removal if the clone disappears", async () => {
  page(true)
  fireEvent.click(await screen.findByRole("button", { name: "Remove acme/widget" }))
  vi.mocked(fetch).mockResolvedValue(json({ clones: [{ ...clone, branch: "stack/new", problem: "cache unreadable", unpushed: 0 }] }))
  onMessage({ type: "agents_changed" })
  const dialog = screen.getByRole("dialog")
  expect(await within(dialog).findByText("stack/new")).toBeTruthy()
  expect(within(dialog).getByText("Cache comparison unavailable: cache unreadable")).toBeTruthy()
  expect(within(dialog).queryByText(/2 local tips/)).toBeNull()
  vi.mocked(fetch).mockResolvedValue(json({ clones: [] }))
  onMessage({ type: "agents_changed" })
  expect(await within(dialog).findByText(/This clone is no longer in the inventory/)).toBeTruthy()
  expect(within(dialog).getByRole("button", { name: "Remove entire folder" })).toHaveProperty("disabled", true)
})
