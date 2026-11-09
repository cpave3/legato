import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { RepositoryGuidanceSettings } from "../RepositoryGuidanceSettings"

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })
}

type Stored = { repo: string; text: string; configured: boolean }
let stored: Stored[]
let requests: Array<{ method: string; url: string; body?: Record<string, unknown> }>

beforeEach(() => {
  stored = [
    { repo: "github.com/acme/web-app", text: "From the config file.", configured: true },
    { repo: "github.com/acme/api", text: "Run the API checks.", configured: false },
  ]
  requests = []
  vi.stubGlobal("fetch", vi.fn(async (url: string, init?: RequestInit) => {
    const method = init?.method ?? "GET"
    const body = init?.body ? JSON.parse(String(init.body)) : undefined
    requests.push({ method, url, body })
    if (method === "GET") return json({ guidance: stored })
    if (method === "PUT") {
      if (body.repo === "acme/web-app") return json({ error: "this repository's guidance is set in config.yaml: github.com/acme/web-app; change it there" }, 409)
      const repo = String(body.repo).includes("/") && !String(body.repo).startsWith("github.com/") ? `github.com/${body.repo}` : String(body.repo)
      const saved = { repo, text: String(body.text).trim(), configured: false }
      stored = [...stored.filter((g) => g.repo !== repo), saved]
      return json(saved)
    }
    if (method === "DELETE") {
      const repo = new URL(url, "http://x").searchParams.get("repo")
      stored = stored.filter((g) => g.repo !== repo)
      return new Response(null, { status: 204 })
    }
    return json({ error: "nope" }, 400)
  }))
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe("RepositoryGuidanceSettings", () => {
  it("lists each repository's guidance and leaves config.yaml's to the file", async () => {
    render(<RepositoryGuidanceSettings baseUrl="" />)

    const configured = await screen.findByRole("listitem", { name: "github.com/acme/web-app" })
    expect(within(configured).getByText("config.yaml")).toBeTruthy()
    expect(within(configured).getByText("From the config file.")).toBeTruthy()
    expect(within(configured).queryByRole("button", { name: /Edit|Remove/ })).toBeNull()

    const saved = screen.getByRole("listitem", { name: "github.com/acme/api" })
    expect(within(saved).getByText("Run the API checks.")).toBeTruthy()
    expect(within(saved).getByRole("button", { name: "Edit guidance for github.com/acme/api" })).toBeTruthy()
    expect(within(saved).getByRole("button", { name: "Remove guidance for github.com/acme/api" })).toBeTruthy()
  })

  it("adds guidance for a repository", async () => {
    render(<RepositoryGuidanceSettings baseUrl="" />)
    fireEvent.click(await screen.findByRole("button", { name: /Add guidance/ }))
    fireEvent.change(screen.getByRole("textbox", { name: "Repository" }), { target: { value: "acme/billing" } })
    fireEvent.change(screen.getByRole("textbox", { name: "Guidance" }), { target: { value: "Run the prove-it skill at the end." } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))

    const added = await screen.findByRole("listitem", { name: "github.com/acme/billing" })
    expect(within(added).getByText("Run the prove-it skill at the end.")).toBeTruthy()
    const put = requests.find((r) => r.method === "PUT")
    expect(put?.url).toBe("/api/settings/repository-guidance")
    expect(put?.body).toEqual({ repo: "acme/billing", text: "Run the prove-it skill at the end." })
  })

  it("edits saved guidance in place", async () => {
    render(<RepositoryGuidanceSettings baseUrl="" />)
    fireEvent.click(await screen.findByRole("button", { name: "Edit guidance for github.com/acme/api" }))
    const text = screen.getByRole("textbox", { name: "Guidance" }) as HTMLTextAreaElement
    expect(text.value).toBe("Run the API checks.")
    expect(screen.queryByRole("textbox", { name: "Repository" })).toBeNull()
    fireEvent.change(text, { target: { value: "Run the API checks, then the contract tests." } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))

    await screen.findByText("Run the API checks, then the contract tests.")
    expect(requests.find((r) => r.method === "PUT")?.body).toEqual({ repo: "github.com/acme/api", text: "Run the API checks, then the contract tests." })
  })

  it("removes saved guidance after asking", async () => {
    render(<RepositoryGuidanceSettings baseUrl="" />)
    fireEvent.click(await screen.findByRole("button", { name: "Remove guidance for github.com/acme/api" }))
    fireEvent.click(screen.getByRole("button", { name: "Remove" }))

    await waitFor(() => expect(screen.queryByRole("listitem", { name: "github.com/acme/api" })).toBeNull())
    expect(requests.find((r) => r.method === "DELETE")?.url).toBe("/api/settings/repository-guidance?repo=github.com%2Facme%2Fapi")
  })

  it("shows why guidance was not saved and keeps the form", async () => {
    render(<RepositoryGuidanceSettings baseUrl="" />)
    fireEvent.click(await screen.findByRole("button", { name: /Add guidance/ }))
    fireEvent.change(screen.getByRole("textbox", { name: "Repository" }), { target: { value: "acme/web-app" } })
    fireEvent.change(screen.getByRole("textbox", { name: "Guidance" }), { target: { value: "Other." } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))

    expect((await screen.findByRole("alert")).textContent).toMatch(/set in config.yaml/)
    expect((screen.getByRole("textbox", { name: "Repository" }) as HTMLInputElement).value).toBe("acme/web-app")
  })
})
