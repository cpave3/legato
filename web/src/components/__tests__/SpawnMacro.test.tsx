import { QueryClientProvider } from "@tanstack/react-query"
import { createQueryClient } from "../../lib/query"
import type { ReactNode } from "react"
import { cleanup, fireEvent, render as rtlRender, screen } from "@testing-library/react"
import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { SpawnEphemeralModal } from "../SpawnEphemeralModal"

const render = (ui: ReactNode) => rtlRender(<QueryClientProvider client={createQueryClient()}>{ui}</QueryClientProvider>)

const { apiFetch } = vi.hoisted(() => ({ apiFetch: vi.fn() }))

vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))
vi.mock("../../hooks/useSettings", () => ({
  useSettings: () => ({ data: { working_dir: "/default", sandbox_enabled: false, fresh_clones_enabled: false, repositories: [] } }),
}))
vi.mock("../../hooks/useToast", () => ({ useToast: () => ({ addToast: vi.fn() }) }))
vi.mock("../../lib/api", () => ({ apiFetch }))

function json(body: unknown, status = 200) {
  return Promise.resolve(new Response(JSON.stringify(body), { status }))
}

beforeEach(() => {
  apiFetch.mockReset()
  apiFetch.mockImplementation((_: string, path: string, init?: RequestInit) => {
    const key = `${init?.method ?? "GET"} ${path.split("?")[0]}`
    if (key === "GET /api/adapters") return json({ adapters: ["claude-code", "codex"], default: "claude-code" })
    if (key === "GET /api/directories/presets") return json({ presets: [], default: { path: "/default", display: "/default" } })
    if (key === "GET /api/directories") return json({ query: "", resolved: "", exists: false, base: "", prefix: "", total: 0, items: [] })
    if (key === "GET /api/macros") return json({ macros: [
      { name: "Plan", keys: "Plan this task.\n", applies_to: "task" },
      { name: "Review", keys: "Review it.", applies_to: "pr" },
      { name: "deep", keys: "Go deep.", agent: "codex", tier: "smart" },
    ] })
    if (key === "POST /api/agents/spawn") return json({ status: "ok" }, 201)
    return json({}, 200)
  })
})
afterEach(cleanup)

it("fills the first message, agent tool and tier from a macro", async () => {
  render(<SpawnEphemeralModal open onClose={() => {}} />)
  await screen.findByRole("dialog", { name: "Spawn agent" })
  const choice = await screen.findByRole("combobox", { name: "Start with macro" })
  await vi.waitFor(() => expect(screen.getByRole("option", { name: "deep" })).toBeTruthy())
  expect(screen.queryByRole("option", { name: "Review" })).toBeNull()
  fireEvent.change(choice, { target: { value: "Plan" } })
  expect((screen.getByRole("textbox", { name: "First message" }) as HTMLTextAreaElement).value).toBe("Plan this task.")
  expect(screen.queryByText(/^Model tier:/)).toBeNull()
  fireEvent.change(choice, { target: { value: "deep" } })
  expect((screen.getByRole("textbox", { name: "First message" }) as HTMLTextAreaElement).value).toBe("Go deep.")
  expect(screen.getByText("Model tier: smart")).toBeTruthy()
  await vi.waitFor(() => expect((screen.getByRole("button", { name: /^Spawn in/ }) as HTMLButtonElement).disabled).toBe(false))
  fireEvent.click(screen.getByRole("button", { name: /^Spawn in/ }))
  await vi.waitFor(() => expect(apiFetch.mock.calls.some(([, path, init]) => path === "/api/agents/spawn" && init?.method === "POST")).toBe(true))
  const body = JSON.parse(apiFetch.mock.calls.find(([, path, init]) => path === "/api/agents/spawn" && init?.method === "POST")![2].body)
  expect(body).toMatchObject({ prompt: "Go deep.", agent_kind: "codex", tier: "smart" })
})

it("groups the favourite macros first, and the rest after them", async () => {
  apiFetch.mockImplementation((_: string, path: string, init?: RequestInit) => {
    const key = `${init?.method ?? "GET"} ${path.split("?")[0]}`
    if (key === "GET /api/adapters") return json({ adapters: ["claude-code"], default: "claude-code" })
    if (key === "GET /api/directories/presets") return json({ presets: [], default: { path: "/default", display: "/default" } })
    if (key === "GET /api/directories") return json({ query: "", resolved: "", exists: false, base: "", prefix: "", total: 0, items: [] })
    if (key === "GET /api/macros") return json({ macros: [
      { name: "Ticket to PR", keys: "ship it" },
      { name: "Plan", keys: "plan it", favorite: true },
    ] })
    return json({}, 200)
  })
  render(<SpawnEphemeralModal open onClose={() => {}} />)
  await screen.findByRole("dialog", { name: "Spawn agent" })
  await vi.waitFor(() => expect(screen.getByRole("option", { name: "Plan" })).toBeTruthy())
  const favourites = screen.getByRole("group", { name: "Favourites" })
  const rest = screen.getByRole("group", { name: "Other macros" })
  expect(favourites.textContent).toBe("Plan")
  expect(rest.textContent).toBe("Ticket to PR")
})
