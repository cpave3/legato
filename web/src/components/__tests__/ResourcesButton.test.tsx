import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { MemoryRouter } from "react-router-dom"
import { ResourcesButton } from "../ResourcesButton"

function renderButton() {
  return render(<MemoryRouter><ResourcesButton agentId="task-1" baseUrl="" /></MemoryRouter>)
}

const resources = [
  { name: "Obsidian vault", configured: true, paths: [{ path: "/home/me/My Vault", access: "write" }] },
  { id: 3, name: "Specs", configured: false, paths: [{ path: "/srv/specs", access: "read" }, { path: "/srv/rfcs", access: "read" }] },
]

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })
}

let attached: string[]
let posted: unknown[]

beforeEach(() => {
  attached = ["Obsidian vault"]
  posted = []
  vi.stubGlobal("fetch", vi.fn(async (url: string, init?: RequestInit) => {
    if (url.endsWith("/api/resources")) return json({ resources })
    if (url.endsWith("/api/agents/task-1/resources") && init?.method === "POST") {
      const body = JSON.parse(String(init.body))
      posted.push(body)
      attached = [...attached, ...body.names]
      return json({ attached })
    }
    if (url.endsWith("/api/agents/task-1/resources")) return json({ attached })
    return json({ error: "not found" }, 404)
  }))
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe("ResourcesButton", () => {
  it("lists the resources and marks the ones the agent already has", async () => {
    renderButton()
    fireEvent.click(screen.getByRole("button", { name: /^Resources/ }))

    const list = await screen.findByRole("list", { name: "Resources" })
    await waitFor(() => expect(within(list).getByText("Specs")).toBeTruthy())
    expect(within(list).getByRole("button", { name: "Obsidian vault, attached" })).toHaveProperty("disabled", true)
    expect(within(list).getByText("/home/me/My Vault")).toBeTruthy()
  })

  it("attaches a resource when it is chosen", async () => {
    renderButton()
    fireEvent.click(screen.getByRole("button", { name: /^Resources/ }))

    fireEvent.click(await screen.findByRole("button", { name: "Attach Specs" }))

    await waitFor(() => expect(posted).toEqual([{ names: ["Specs"] }]))
    expect(await screen.findByRole("button", { name: "Specs, attached" })).toBeTruthy()
  })

  it("says why a resource could not be attached", async () => {
    vi.stubGlobal("fetch", vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith("/api/resources")) return json({ resources })
      if (init?.method === "POST") return json({ error: "resources can be added to a running agent only through its native panel" }, 409)
      return json({ attached: [] })
    }))
    renderButton()
    fireEvent.click(screen.getByRole("button", { name: /^Resources/ }))

    fireEvent.click(await screen.findByRole("button", { name: "Attach Specs" }))

    expect(await screen.findByRole("alert")).toHaveProperty("textContent", expect.stringContaining("only through its native panel"))
  })

  it("points to Settings when there are no resources yet", async () => {
    vi.stubGlobal("fetch", vi.fn(async (url: string) => url.endsWith("/api/resources") ? json({ resources: [] }) : json({ attached: [] })))
    renderButton()
    fireEvent.click(screen.getByRole("button", { name: /^Resources/ }))

    expect(await screen.findByText(/No resources yet/)).toBeTruthy()
    expect(screen.getByRole("link", { name: "Settings" }).getAttribute("href")).toBe("/settings#resources")
  })
})
