import { QueryClientProvider } from "@tanstack/react-query"
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { createQueryClient } from "../../lib/query"
import { MacrosPage } from "../MacrosPage"
import type { Macro } from "../../lib/macros"

vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "", activeServerName: "Local" }) }))

const builtins: Macro[] = [
  { name: "Plan", keys: "Plan this task.", applies_to: "task", source: "builtin", builtin: true },
  { name: "Implement", keys: "Implement it test first.", applies_to: "task", agent: "codex", source: "override", builtin: true, overridden: true },
  { name: "Review", keys: "Review this pull request.", applies_to: "pr", source: "builtin", builtin: true },
  { name: "Address feedback", keys: "Address the feedback.", applies_to: "pr", source: "builtin", builtin: true },
]
const yours: Macro[] = [
  { name: "Run tests", keys: "Run the tests and fix what fails.", source: "database" },
  { name: "Deep review", keys: "Review in depth.", applies_to: "pr", agent: "claude-code", tier: "large", source: "database" },
  { name: "Commit changes", keys: "Commit everything.", source: "config.yaml", pinned: true },
]
let macros: Macro[] = []

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })
}
function respond(url: string, init?: RequestInit) {
  if (url === "/api/macros" && !init?.method) return json({ macros, tiers: { "claude-code": [{ name: "large", description: "Opus" }] } })
  if (url === "/api/adapters") return json({ adapters: ["claude-code", "codex"], default: "claude-code" })
  return json({ status: "ok" })
}
function requests(method: string) {
  return vi.mocked(fetch).mock.calls.filter(([, init]) => init?.method === method).map(([url]) => String(url))
}
function page() {
  return render(<QueryClientProvider client={createQueryClient()}><MemoryRouter><MacrosPage /></MemoryRouter></QueryClientProvider>)
}

beforeEach(() => {
  macros = [...builtins, ...yours]
  vi.stubGlobal("fetch", vi.fn(async (url: string, init?: RequestInit) => respond(String(url), init)))
})
afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks() })

it("says what a macro is and where to run one", async () => {
  page()
  expect(screen.getByRole("heading", { name: "Macros", level: 1 })).toBeTruthy()
  expect(screen.getByText(/A saved message for an agent/)).toBeTruthy()
  const where = screen.getByRole("list", { name: "Where to run one" })
  for (const place of ["Card menu", "Pull request line", "Spawn dialog", "Agents page · M", "TUI · m", "legato macro run", "Workflows · with.macro"]) {
    expect(within(where).getByText((_, el) => el?.tagName === "LI" && el.textContent === place)).toBeTruthy()
  }
})

it("lists the built-ins and yours, with what each is offered on and starts", async () => {
  page()
  const builtIn = await screen.findByRole("region", { name: "Built in" })
  const plan = within(builtIn).getByRole("listitem", { name: "Plan" })
  expect(within(plan).getByText("Tasks")).toBeTruthy()
  expect(within(plan).getByRole("button", { name: "Edit Plan" })).toBeTruthy()
  expect(within(plan).queryByRole("button", { name: /Reset|Delete/ })).toBeNull()
  const implement = within(builtIn).getByRole("listitem", { name: "Implement" })
  expect(within(implement).getByText("Edited")).toBeTruthy()
  expect(within(implement).getByText("Codex")).toBeTruthy()
  expect(within(implement).getByRole("button", { name: "Reset Implement" })).toBeTruthy()
  expect(within(within(builtIn).getByRole("listitem", { name: "Review" })).getByText("Pull requests")).toBeTruthy()

  const mine = screen.getByRole("region", { name: "Yours" })
  const tests = within(mine).getByRole("listitem", { name: "Run tests" })
  expect(within(tests).getByText("Tasks and pull requests")).toBeTruthy()
  expect(within(tests).getByRole("button", { name: "Delete Run tests" })).toBeTruthy()
  expect(within(within(mine).getByRole("listitem", { name: "Deep review" })).getByText("Claude Code · large tier")).toBeTruthy()
  const pinned = within(mine).getByRole("listitem", { name: "Commit changes" })
  expect(within(pinned).getByText("config.yaml")).toBeTruthy()
  expect(within(pinned).getByText("Edit in config.yaml")).toBeTruthy()
  expect(within(pinned).queryByRole("button", { name: /Edit|Reset|Delete/ })).toBeNull()
})

it("resets an edited built-in", async () => {
  page()
  fireEvent.click(await screen.findByRole("button", { name: "Reset Implement" }))
  await waitFor(() => expect(requests("POST")).toEqual(["/api/macros/Implement/reset"]))
})

it("asks before deleting one of yours, and says what breaks", async () => {
  page()
  fireEvent.click(await screen.findByRole("button", { name: "Delete Run tests" }))
  const row = screen.getByRole("listitem", { name: "Run tests" })
  expect(within(row).getByText("Delete Run tests? Workflows that use it will fail.")).toBeTruthy()
  fireEvent.click(within(row).getByRole("button", { name: "Cancel" }))
  expect(requests("DELETE")).toEqual([])
  fireEvent.click(within(row).getByRole("button", { name: "Delete Run tests" }))
  macros = builtins
  fireEvent.click(within(row).getByRole("button", { name: "Delete" }))
  await waitFor(() => expect(requests("DELETE")).toEqual(["/api/macros/Run%20tests"]))
  expect(await screen.findByText("No macros of your own")).toBeTruthy()
})

it("shows a server's refusal", async () => {
  vi.mocked(fetch).mockImplementation(async (url, init) => init?.method === "POST" ? json({ error: "only a built-in macro can be reset" }, 409) : respond(String(url), init))
  page()
  fireEvent.click(await screen.findByRole("button", { name: "Reset Implement" }))
  expect((await screen.findByRole("alert")).textContent).toContain("only a built-in macro can be reset")
})

it("with none of your own, offers to make one", async () => {
  macros = builtins
  page()
  const mine = await screen.findByRole("region", { name: "Yours" })
  expect(within(mine).getByText("No macros of your own")).toBeTruthy()
  expect(within(mine).getByRole("button", { name: "New macro" })).toBeTruthy()
})

it("stars a macro and takes the star away", async () => {
  macros = [{ ...builtins[0], favorite: true }, ...builtins.slice(1), ...yours]
  page()
  const builtIn = await screen.findByRole("region", { name: "Built in" })
  const star = within(within(builtIn).getByRole("listitem", { name: "Plan" })).getByRole("button", { name: "Favourite: Plan" })
  expect(star.getAttribute("aria-pressed")).toBe("true")
  fireEvent.click(star)
  await waitFor(() => expect(vi.mocked(fetch)).toHaveBeenCalledWith("/api/macros/Plan", expect.objectContaining({ method: "PUT", body: JSON.stringify({ favorite: false }) })))

  const mine = screen.getByRole("region", { name: "Yours" })
  const tests = within(within(mine).getByRole("listitem", { name: "Run tests" })).getByRole("button", { name: "Favourite: Run tests" })
  expect(tests.getAttribute("aria-pressed")).toBe("false")
  fireEvent.click(tests)
  await waitFor(() => expect(vi.mocked(fetch)).toHaveBeenCalledWith("/api/macros/Run%20tests", expect.objectContaining({ method: "PUT", body: JSON.stringify({ favorite: true }) })))

  const pinned = within(within(mine).getByRole("listitem", { name: "Commit changes" })).getByRole("button", { name: "Favourite: Commit changes" }) as HTMLButtonElement
  expect(pinned.disabled).toBe(true)
  expect(pinned.title).toMatch(/config\.yaml/)
})
