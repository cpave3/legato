// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest"
import type { TicketSite } from "./ticket-sites"
import { allowTicketSite, renderTicketSites } from "./ticket-sites-view"

const KEY_PATTERN = "^[A-Z][A-Z0-9_]*-[0-9]+$"
const jira: TicketSite = { name: "jira", label: "Jira", web_origin: "https://example.atlassian.net", key_pattern: KEY_PATTERN, allowed: true }
const burnlite: TicketSite = { name: "burnlite", label: "Burnlite", web_origin: "https://burnlite.test", key_pattern: KEY_PATTERN, allowed: false }

let container: HTMLElement
beforeEach(() => {
  document.body.innerHTML = `<section id="sites"></section>`
  container = document.querySelector("#sites")!
})

describe("renderTicketSites", () => {
  it("lists each site with its label, origin and state, and an Allow button where access is missing", () => {
    renderTicketSites(container, { sites: [jira, burnlite] }, vi.fn())
    const rows = [...container.querySelectorAll("li")]
    expect(rows).toHaveLength(2)
    expect(rows[0].textContent).toContain("Jira")
    expect(rows[0].textContent).toContain("https://example.atlassian.net")
    expect(rows[0].textContent).toContain("Allowed")
    expect(rows[0].querySelector("button")).toBeNull()
    expect(rows[1].textContent).toContain("Burnlite")
    expect(rows[1].textContent).toContain("Not allowed")
    expect(rows[1].querySelector("button")?.textContent).toBe("Allow")
  })

  it("calls back with the site when Allow is clicked", () => {
    const onAllow = vi.fn()
    renderTicketSites(container, { sites: [burnlite] }, onAllow)
    container.querySelector("button")!.click()
    expect(onAllow).toHaveBeenCalledWith(burnlite)
  })

  it("says when Legato reports no sites, and shows an error beside the list", () => {
    renderTicketSites(container, { sites: [], error: "Could not reach Legato" }, vi.fn())
    expect(container.textContent).toContain("Legato reports no ticket sites")
    expect(container.textContent).toContain("Could not reach Legato")
  })

  it("renders site text as text, not markup", () => {
    renderTicketSites(container, { sites: [{ ...burnlite, label: "<img src=x onerror=alert(1)>" }] }, vi.fn())
    expect(container.querySelector("img")).toBeNull()
  })
})

describe("allowTicketSite", () => {
  it("asks Chrome for the site at once, then syncs the sites", async () => {
    const calls: string[] = []
    const request = vi.fn((origins: string[]) => { calls.push(`request ${origins.join(",")}`); return Promise.resolve(true) })
    const sync = vi.fn(() => { calls.push("sync"); return Promise.resolve({ sites: [{ ...burnlite, allowed: true }] }) })
    const result = allowTicketSite(burnlite, { request, sync })
    expect(request).toHaveBeenCalledWith(["https://burnlite.test/*"])
    await expect(result).resolves.toEqual({ sites: [{ ...burnlite, allowed: true }] })
    expect(calls).toEqual(["request https://burnlite.test/*", "sync"])
  })

  it("says so when the person refuses, and still syncs", async () => {
    const sync = vi.fn().mockResolvedValue({ sites: [burnlite] })
    await expect(allowTicketSite(burnlite, { request: vi.fn().mockResolvedValue(false), sync }))
      .resolves.toEqual({ sites: [burnlite], error: "Access to https://burnlite.test was not granted" })
  })
})
