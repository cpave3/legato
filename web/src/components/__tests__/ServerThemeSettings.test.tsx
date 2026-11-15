import { afterEach, expect, it, vi } from "vitest"
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { QueryClientProvider } from "@tanstack/react-query"
import { ServerProvider } from "../ServerProvider"
import { ServerThemeSettings } from "../settings/ServerThemeSettings"
import { createQueryClient } from "../../lib/query"
import { applyAppTheme } from "../../lib/appTheme"

const clients: ReturnType<typeof createQueryClient>[] = []
function mount() {
  const client = createQueryClient(); clients.push(client)
  return render(<ServerProvider><QueryClientProvider client={client}><ServerThemeSettings /></QueryClientProvider></ServerProvider>)
}
afterEach(() => { cleanup(); clients.splice(0).forEach(client => client.clear()); vi.unstubAllGlobals(); localStorage.clear(); applyAppTheme() })

it("edits the server preference with its revision and No preference without changing a local browser theme", async () => {
  localStorage.setItem("legato:app-theme", "ocular-dark")
  applyAppTheme()
  const request = vi.fn(async (_url: string, init?: RequestInit) => {
    if (init?.method === "PUT") return Response.json({ id: JSON.parse(init.body as string).id, revision: 8 })
    return Response.json({ id: "dracula", revision: 7 })
  })
  vi.stubGlobal("fetch", request)
  mount()
  const select = await screen.findByRole("combobox", { name: "Preferred theme" })
  await waitFor(() => expect((select as HTMLSelectElement).value).toBe("dracula"))
  fireEvent.change(select, { target: { value: "" } })
  await waitFor(() => expect(request.mock.calls.some(([, init]) => init?.method === "PUT")).toBe(true))
  const [url, init] = request.mock.calls.find(([, init]) => init?.method === "PUT")!
  expect(url).toBe("/api/settings/theme")
  expect(JSON.parse(init!.body as string)).toEqual({ id: "", expected_revision: 7 })
  await waitFor(() => expect((select as HTMLSelectElement).value).toBe(""))
  expect(screen.getByRole("option", { name: "No preference" })).not.toBeNull()
  expect(document.documentElement.dataset.theme).toBe("ocular-dark")
  expect(localStorage.getItem("legato:app-theme")).toBe("ocular-dark")
})

it("shows load, conflict and persistence failures and reloads the authoritative revision before retry", async () => {
  let snapshot = { id: "dracula", revision: 2 }
  let failure = 503
  const request = vi.fn(async (_url: string, init?: RequestInit) => {
    if (init?.method === "PUT") return Response.json({ error: "Disk is full" }, { status: failure })
    return failure === 503 ? new Response("unavailable", { status: 503 }) : Response.json(snapshot)
  })
  vi.stubGlobal("fetch", request)
  mount()
  expect((await screen.findByRole("alert")).textContent).toContain("Could not load")
  failure = 409
  fireEvent.click(screen.getByRole("button", { name: "Reload preference" }))
  const select = await screen.findByRole("combobox", { name: "Preferred theme" })
  fireEvent.change(select, { target: { value: "ocular-dark" } })
  expect((await screen.findByRole("alert")).textContent).toContain("changed elsewhere")
  snapshot = { id: "ocular-light", revision: 3 }
  fireEvent.click(screen.getByRole("button", { name: "Reload preference" }))
  await waitFor(() => expect((select as HTMLSelectElement).value).toBe("ocular-light"))
  failure = 500
  fireEvent.change(select, { target: { value: "ocular-dark" } })
  expect((await screen.findByRole("alert")).textContent).toBe("Disk is full")
  const writes = request.mock.calls.filter(([, init]) => init?.method === "PUT")
  expect(JSON.parse(writes[1][1]!.body as string).expected_revision).toBe(3)
  expect((select as HTMLSelectElement).value).toBe("ocular-light")
})
