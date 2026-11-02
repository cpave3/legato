import { QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { MemoryRouter } from "react-router-dom"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { createQueryClient } from "../../lib/query"
import { navSections } from "../../lib/navSections"
import { tintColor } from "../../lib/tints"
import { NavRail } from "../NavRail"
import { TintBand } from "../TintBand"
import { TintPicker } from "../TintPicker"

vi.mock("../../hooks/useServer", () => ({ useServer: () => ({ baseUrl: "" }) }))

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })
}

let requests: Array<{ url: string; method: string; body?: string }>

beforeEach(() => {
  requests = []
  document.head.innerHTML = '<meta name="theme-color" content="#0a0a0f" />'
  vi.stubGlobal("fetch", vi.fn(async (url: string, init?: RequestInit) => {
    requests.push({ url, method: init?.method ?? "GET", body: init?.body as string | undefined })
    if (url === "/api/settings/tint") return json({ tint: JSON.parse(String(init?.body)).tint })
    if (url === "https://192.168.1.19:3080/api/meta") return json({ version: "x", tint: "purple" })
    if (url === "/api/meta") return json({ version: "x" })
    return json({}, 404)
  }))
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const themeColor = () => document.querySelector('meta[name="theme-color"]')?.getAttribute("content")

describe("a server's tint", () => {
  it("is a band across the window and the browser's theme colour", () => {
    const { rerender } = render(<TintBand tint="purple" />)
    expect(screen.getByTestId("tint-band").dataset.tint).toBe("purple")
    expect(themeColor()).toBe(tintColor("purple"))

    rerender(<TintBand tint="" />)
    expect(screen.queryByTestId("tint-band")).toBeNull()
    expect(themeColor()).toBe("#0a0a0f")
  })

  it("tints the rail, and the switcher shows each server's colour", async () => {
    render(
      <QueryClientProvider client={createQueryClient()}>
        <MemoryRouter>
          <NavRail sections={navSections()} connected servers={[{ name: "legato-1", url: "https://192.168.1.19:3080" }]}
            activeServerName="Local" baseUrl="" onSwitchServer={() => {}} tint="yellow" />
        </MemoryRouter>
      </QueryClientProvider>,
    )
    const rail = screen.getByRole("navigation", { name: "Primary" })
    expect(rail.dataset.tint).toBe("yellow")
    fireEvent.click(screen.getByRole("button", { name: "Server: Local" }))
    const remote = await screen.findByRole("button", { name: /legato-1/ })
    await waitFor(() => expect(within(remote).getByTitle("purple")).toBeTruthy())
    expect(within(screen.getByRole("button", { name: /^Local/ })).queryByTitle(/./)).toBeNull()
  })

  it("is chosen in Settings and saved on the server", async () => {
    const onChanged = vi.fn()
    render(<TintPicker tint="" onChanged={onChanged} />)
    const group = screen.getByRole("radiogroup", { name: "Tint" })
    expect(within(group).getByRole("radio", { name: "None" }).getAttribute("aria-checked")).toBe("true")
    fireEvent.click(within(group).getByRole("radio", { name: "Purple" }))
    await waitFor(() => expect(onChanged).toHaveBeenCalled())
    expect(requests).toEqual([{ url: "/api/settings/tint", method: "PUT", body: JSON.stringify({ tint: "purple" }) }])
  })
})
