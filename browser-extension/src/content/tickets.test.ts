// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest"
import type { TicketProvider } from "../ticket-providers"
import { parseTicketKey, providerForOrigin, reconcileTicketAction } from "./tickets"

const KEY_PATTERN = "^[A-Z][A-Z0-9_]*-[0-9]+$"
const jira: TicketProvider = { name: "jira", label: "Jira", web_origin: "https://example.atlassian.net", key_pattern: KEY_PATTERN }
const burnlite: TicketProvider = { name: "burnlite", label: "Burnlite", web_origin: "https://burnlite.test:8443", key_pattern: KEY_PATTERN }

describe("providerForOrigin", () => {
  it("finds the provider whose web origin is the page's origin", () => {
    expect(providerForOrigin([jira, burnlite], "https://burnlite.test:8443")).toBe(burnlite)
    expect(providerForOrigin([jira, burnlite], "https://example.atlassian.net")).toBe(jira)
    expect(providerForOrigin([jira, burnlite], "https://other.atlassian.net")).toBeUndefined()
  })

  it("skips providers the extension cannot read keys for", () => {
    const linear = { ...jira, name: "linear", web_origin: "https://linear.app" }
    expect(providerForOrigin([linear], "https://linear.app")).toBeUndefined()
  })
})

describe("parseTicketKey for Jira", () => {
  it("extracts an uppercase issue key from a browse URL on the provider's site", () => {
    expect(parseTicketKey(jira, "https://example.atlassian.net/browse/AG-9804", document)).toBe("AG-9804")
    expect(parseTicketKey(jira, "https://example.atlassian.net/browse/ag-9804", document)).toBe("AG-9804")
    expect(parseTicketKey(jira, "https://example.atlassian.net/browse/AG-9804/comments", document)).toBe("AG-9804")
  })

  it.each([
    "https://example.atlassian.net/issues/AG-9804",
    "https://other.atlassian.net/browse/AG-9804",
    "https://example.atlassian.net/browse/not-a-ticket",
    "not a url",
  ])("rejects %s", (url) => expect(parseTicketKey(jira, url, document)).toBeNull())

  it("rejects a key the provider's pattern does not accept", () => {
    expect(parseTicketKey({ ...jira, key_pattern: "^AG-[0-9]+$" }, "https://example.atlassian.net/browse/XY-1", document)).toBeNull()
  })
})

describe("parseTicketKey for Burnlite", () => {
  beforeEach(() => { document.body.innerHTML = "<main></main>" })

  it("reads the key from a key link", () => {
    expect(parseTicketKey(burnlite, "https://burnlite.test:8443/EST-42", document)).toBe("EST-42")
  })

  it("reads the key a project page keeps in ?item=", () => {
    expect(parseTicketKey(burnlite, "https://burnlite.test:8443/project/0b7c6c1e-2f4a-4c55-9c1e-5d3e0f2a1b9c?item=EST-7", document)).toBe("EST-7")
  })

  it("reads the key from the open task drawer when the address has none", () => {
    document.body.innerHTML = `<div data-testid="task-drawer"><span data-testid="item-key"> EST-9 </span></div>`
    expect(parseTicketKey(burnlite, "https://burnlite.test:8443/project/0b7c6c1e", document)).toBe("EST-9")
  })

  it("ignores an item key outside the task drawer", () => {
    document.body.innerHTML = `<ul><li data-testid="item-key">EST-9</li></ul>`
    expect(parseTicketKey(burnlite, "https://burnlite.test:8443/project/0b7c6c1e", document)).toBeNull()
  })

  it.each([
    "https://other.test/EST-42",
    "http://burnlite.test:8443/EST-42",
    "https://burnlite.test:8443/projects",
    "https://burnlite.test:8443/EST-42/edit",
    "https://burnlite.test:8443/project/0b7c6c1e?item=not-a-key",
  ])("rejects %s", (url) => expect(parseTicketKey(burnlite, url, document)).toBeNull())

  it("rejects a drawer key the provider's pattern does not accept", () => {
    document.body.innerHTML = `<div data-testid="task-drawer"><span data-testid="item-key">est-9</span></div>`
    expect(parseTicketKey(burnlite, "https://burnlite.test:8443/project/0b7c6c1e", document)).toBeNull()
  })
})

describe("reconcileTicketAction", () => {
  beforeEach(() => { document.body.innerHTML = "<main>Ticket</main>" })

  it("renders one body-level bottom-right import action only on ticket pages", () => {
    const send = vi.fn()
    const href = "https://example.atlassian.net/browse/AG-9804"
    reconcileTicketAction(document, href, jira, send)
    reconcileTicketAction(document, href, jira, send)

    const action = document.querySelector<HTMLElement>("#legato-ticket-action")!
    expect(document.querySelectorAll("#legato-ticket-action")).toHaveLength(1)
    expect(action.parentElement).toBe(document.body)
    expect(action.style.position).toBe("fixed")
    expect(action.style.right).toBe("24px")
    expect(action.style.bottom).toBe("24px")
    expect(action.textContent).toContain("legato")
    expect(action.textContent).toContain("Jira ticket import")
    expect(action.textContent).toContain("AG-9804")
    expect(action.querySelector("button")?.textContent).toBe("Import AG-9804 to Legato")

    reconcileTicketAction(document, "https://example.atlassian.net/issues", jira, send)
    expect(document.querySelector("#legato-ticket-action")).toBeNull()
  })

  it("names a Burnlite task and follows the drawer as it opens and closes", () => {
    const send = vi.fn()
    const href = "https://burnlite.test:8443/project/0b7c6c1e"
    reconcileTicketAction(document, href, burnlite, send)
    expect(document.querySelector("#legato-ticket-action")).toBeNull()

    document.body.insertAdjacentHTML("beforeend", `<div data-testid="task-drawer"><span data-testid="item-key">EST-3</span></div>`)
    reconcileTicketAction(document, href, burnlite, send)
    const action = document.querySelector<HTMLElement>("#legato-ticket-action")!
    expect(action.textContent).toContain("Burnlite task import")
    expect(action.querySelector("button")?.textContent).toBe("Import EST-3 to Legato")

    document.querySelector("[data-testid=task-drawer]")!.remove()
    reconcileTicketAction(document, href, burnlite, send)
    expect(document.querySelector("#legato-ticket-action")).toBeNull()
  })

  it("imports once, reports success, and permits retry after an error", async () => {
    const send = vi.fn()
      .mockRejectedValueOnce(new Error("Burnlite is not connected"))
      .mockResolvedValueOnce({ ok: true, taskUrl: "http://localhost:3080/board?highlight_task=task-1" })
    reconcileTicketAction(document, "https://burnlite.test:8443/EST-42", burnlite, send)
    const button = document.querySelector<HTMLButtonElement>("#legato-ticket-action button")!

    button.click()
    button.click()
    expect(send).toHaveBeenCalledTimes(1)
    await vi.waitFor(() => expect(button.textContent).toBe("Retry import EST-42"))
    expect(document.querySelector("#legato-ticket-action")?.textContent).toContain("Burnlite is not connected")

    button.click()
    button.click()
    await vi.waitFor(() => expect(send).toHaveBeenCalledTimes(2))
    expect(send.mock.calls[1][0]).toEqual({ type: "importTicket", context: { provider: "burnlite", ticketId: "EST-42" } })
    await vi.waitFor(() => expect(button.textContent).toBe("Imported to Legato"))
  })

  it("shows the error a failed response carries", async () => {
    const send = vi.fn().mockResolvedValue({ ok: false, error: "EST-42 is already a task from jira" })
    reconcileTicketAction(document, "https://burnlite.test:8443/EST-42", burnlite, send)
    document.querySelector<HTMLButtonElement>("#legato-ticket-action button")!.click()
    await vi.waitFor(() => expect(document.querySelector("#legato-ticket-action")?.textContent).toContain("EST-42 is already a task from jira"))
  })
})
