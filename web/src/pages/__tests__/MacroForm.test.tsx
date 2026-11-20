import { QueryClientProvider } from "@tanstack/react-query"
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { createQueryClient } from "../../lib/query"
import { MacrosPage } from "../MacrosPage"
import type { Macro } from "../../lib/macros"

vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "", activeServerName: "Local" }) }))

let macros: Macro[] = []
function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })
}
function writes() {
  return vi.mocked(fetch).mock.calls
    .filter(([, init]) => init?.method && init.method !== "GET")
    .map(([url, init]) => ({ method: init?.method, url: String(url), body: init?.body ? JSON.parse(String(init.body)) : undefined }))
}
async function open(button: string) {
  render(<QueryClientProvider client={createQueryClient()}><MemoryRouter><MacrosPage /></MemoryRouter></QueryClientProvider>)
  fireEvent.click((await screen.findAllByRole("button", { name: button }))[0])
  return screen.getByRole("dialog")
}

beforeEach(() => {
  macros = [
    { name: "Review", keys: "Review this pull request.\n", applies_to: "pr", source: "builtin", builtin: true },
    { name: "Deep review", keys: "Review in depth.", applies_to: "pr", agent: "codex", tier: "large", source: "database" },
  ]
  vi.stubGlobal("fetch", vi.fn(async (url: string, init?: RequestInit) => {
    if (String(url) === "/api/macros" && !init?.method) return json({ macros, tiers: { "claude-code": [{ name: "small", description: "Haiku" }, { name: "large", description: "Opus" }] } })
    if (String(url) === "/api/adapters") return json({ adapters: ["claude-code", "codex"], default: "claude-code" })
    return json({ status: "ok" })
  }))
})
afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks() })

it("says which fields are required and which optional", async () => {
  const form = await open("New macro")
  expect(within(form).getByRole("heading", { name: "New macro" })).toBeTruthy()
  expect(within(form).getByRole("textbox", { name: "Name required" })).toBeTruthy()
  expect(within(form).getByRole("textbox", { name: "Message required" })).toBeTruthy()
  expect(within(form).getByRole("group", { name: "Offer it on optional" })).toBeTruthy()
  expect(within(form).getByRole("group", { name: "Agent to start optional" })).toBeTruthy()
  expect(within(form).getByText("Sent as written, below the task’s key and title.")).toBeTruthy()
  expect(within(form).queryByText(/keys|tier ·|agent tool/i)).toBeNull()
  expect((within(form).getByRole("button", { name: "Save" }) as HTMLButtonElement).disabled).toBe(true)
})

it("saves a new macro with its message, what it is offered on, its agent and tier", async () => {
  const form = await open("New macro")
  fireEvent.change(within(form).getByRole("textbox", { name: "Name required" }), { target: { value: "Thorough" } })
  fireEvent.change(within(form).getByRole("textbox", { name: "Message required" }), { target: { value: "Look hard." } })
  fireEvent.click(within(form).getByRole("radio", { name: "Pull requests" }))
  expect(within(form).getByRole("radio", { name: "Your default (Claude Code)" })).toHaveProperty("checked", true)
  fireEvent.change(within(form).getByRole("combobox", { name: "Model tier optional" }), { target: { value: "large" } })
  expect(within(form).getByRole("option", { name: "large · Opus" })).toBeTruthy()
  fireEvent.click(within(form).getByRole("button", { name: "Save" }))
  await waitFor(() => expect(writes()).toEqual([{ method: "PUT", url: "/api/macros/Thorough", body: { message: "Look hard.", applies_to: "pr", tier: "large" } }]))
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())
})

it("offers a tier only for an agent that has tiers", async () => {
  const form = await open("New macro")
  expect(within(form).getByRole("combobox", { name: "Model tier optional" })).toBeTruthy()
  fireEvent.click(within(form).getByRole("radio", { name: "Codex" }))
  expect(within(form).queryByRole("combobox", { name: "Model tier optional" })).toBeNull()
  fireEvent.click(within(form).getByRole("radio", { name: "Claude Code" }))
  expect(within(form).getByRole("combobox", { name: "Model tier optional" })).toBeTruthy()
})

it("says when a saved tier is one its agent lacks", async () => {
  const form = await open("Edit Deep review")
  expect(within(form).getByRole("alert").textContent).toBe("Codex has no tier “large”. It starts without one.")
})

it("keeps a built-in's name, and saves an edit under it", async () => {
  const form = await open("Edit Review")
  expect(within(form).getByText(/Built-in macro\./)).toBeTruthy()
  expect((within(form).getByRole("textbox", { name: "Name required" }) as HTMLInputElement).disabled).toBe(true)
  expect(within(form).getByText("Built-in names can’t change.")).toBeTruthy()
  expect((within(form).getByRole("textbox", { name: "Message required" }) as HTMLTextAreaElement).value).toBe("Review this pull request.")
  fireEvent.click(within(form).getByRole("button", { name: "Save" }))
  await waitFor(() => expect(writes()).toEqual([{ method: "PUT", url: "/api/macros/Review", body: { message: "Review this pull request.", applies_to: "pr" } }]))
})

it("renames one of yours, warning about workflows that use the old name", async () => {
  const form = await open("Edit Deep review")
  const name = within(form).getByRole("textbox", { name: "Name required" })
  fireEvent.change(name, { target: { value: "review" } })
  expect(within(form).getByText("Another macro has that name.")).toBeTruthy()
  expect((within(form).getByRole("button", { name: "Save" }) as HTMLButtonElement).disabled).toBe(true)
  fireEvent.change(name, { target: { value: "Thorough review" } })
  expect(within(form).getByText("Workflows that use the old name, Deep review, will fail.")).toBeTruthy()
  fireEvent.click(within(form).getByRole("button", { name: "Save" }))
  await waitFor(() => expect(writes()).toEqual([
    { method: "PUT", url: "/api/macros/Deep%20review", body: { message: "Review in depth.", applies_to: "pr", agent: "codex", tier: "large" } },
    { method: "POST", url: "/api/macros/Deep%20review/rename", body: { name: "Thorough review" } },
  ]))
})
