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

  it("opens the desktop app's link from the bubble too", async () => {
    const opened: string[] = []
    const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(function (this: HTMLAnchorElement) { opened.push(this.href) })
    const send = vi.fn().mockResolvedValue({ ok: true, desktopLink: "legato-desktop://import?provider=jira&key=AG-1" })
    reconcileTicketAction(document, "https://example.atlassian.net/browse/AG-1", jira, send)
    document.querySelector<HTMLButtonElement>("#legato-ticket-action button")!.click()
    await vi.waitFor(() => expect(opened).toEqual(["legato-desktop://import?provider=jira&key=AG-1"]))
    await vi.waitFor(() => expect(document.querySelector("#legato-ticket-action")?.textContent).toContain("Sent to the Legato desktop app"))
    click.mockRestore()
  })

  it("keeps the widget while its import runs, even when the page stops naming the ticket", async () => {
    let finish: (value: unknown) => void = () => undefined
    const send = vi.fn().mockReturnValue(new Promise((resolve) => { finish = resolve }))
    reconcileTicketAction(document, "https://example.atlassian.net/browse/AG-1", jira, send)
    document.querySelector<HTMLButtonElement>("#legato-ticket-action button")!.click()
    expect(send).toHaveBeenCalledWith({ type: "importTicket", context: { provider: "jira", ticketId: "AG-1" } })

    reconcileTicketAction(document, "https://example.atlassian.net/issues", jira, send)
    expect(document.querySelector("#legato-ticket-action")).not.toBeNull()

    finish({ ok: true, taskUrl: "http://localhost:3080/board?highlight_task=AG-1" })
    await vi.waitFor(() => expect(document.querySelector("#legato-ticket-action button")?.textContent).toBe("Imported to Legato"))
    // Then the widget follows the page again.
    reconcileTicketAction(document, "https://example.atlassian.net/issues", jira, send)
    expect(document.querySelector("#legato-ticket-action")).toBeNull()
  })
})

// Burnlite's task drawer is a modal: a press outside it closes it. So on
// Burnlite the import is a button in the drawer, beside Copy reference.
describe("reconcileTicketAction on Burnlite", () => {
  const href = "https://burnlite.test:8443/project/0b7c6c1e?item=EST-3"
  const drawer = (key: string) => `<div data-testid="task-drawer"><div class="header"><span data-testid="item-key">${key}</span>` +
    `<div class="flex-1"></div><button class="copy-classes" data-testid="copy-task-reference">Copy reference</button></div></div>`
  const inline = () => document.querySelector<HTMLElement>("#legato-ticket-inline")
  const inlineButton = () => document.querySelector<HTMLButtonElement>("#legato-ticket-inline button")!

  beforeEach(() => { document.body.innerHTML = "<main>Board</main>" })

  it("puts an Import button in the drawer, before Copy reference, styled as it, and no bubble", () => {
    reconcileTicketAction(document, href, burnlite, vi.fn())
    expect(inline()).toBeNull()

    document.body.insertAdjacentHTML("beforeend", drawer("EST-3"))
    reconcileTicketAction(document, href, burnlite, vi.fn())
    const copy = document.querySelector("[data-testid=copy-task-reference]")!
    expect(document.querySelector("[data-testid=task-drawer]")!.contains(inline())).toBe(true)
    expect(inline()!.nextElementSibling).toBe(copy)
    expect(inlineButton().className).toBe("copy-classes")
    expect(inlineButton().textContent).toContain("Import to Legato")
    expect(inlineButton().title).toBe("Import EST-3 to Legato")
    expect(document.querySelector("#legato-ticket-action")).toBeNull()

    reconcileTicketAction(document, href, burnlite, vi.fn())
    expect(document.querySelectorAll("#legato-ticket-inline")).toHaveLength(1)
  })

  it("follows the drawer from task to task, and goes when it closes", () => {
    document.body.insertAdjacentHTML("beforeend", drawer("EST-3"))
    reconcileTicketAction(document, href, burnlite, vi.fn())
    document.querySelector("[data-testid=item-key]")!.textContent = "EST-4"
    reconcileTicketAction(document, href, burnlite, vi.fn())
    expect(inlineButton().title).toBe("Import EST-4 to Legato")

    document.querySelector("[data-testid=task-drawer]")!.remove()
    reconcileTicketAction(document, href, burnlite, vi.fn())
    expect(inline()).toBeNull()
  })

  it("puts the button in a milestone's drawer too, and imports the milestone", () => {
    const send = vi.fn().mockResolvedValue({ ok: true })
    document.body.insertAdjacentHTML("beforeend",
      `<div data-testid="milestone-drawer"><div class="header"><span data-testid="item-key">EST-9</span>` +
      `<span>Milestone</span><div class="flex-1"></div>` +
      `<button class="milestone-copy" data-testid="copy-milestone-reference">Copy reference</button></div></div>`)
    reconcileTicketAction(document, "https://burnlite.test:8443/project/0b7c6c1e?item=EST-9", burnlite, send)

    const copy = document.querySelector("[data-testid=copy-milestone-reference]")!
    expect(document.querySelector("[data-testid=milestone-drawer]")!.contains(inline())).toBe(true)
    expect(inline()!.nextElementSibling).toBe(copy)
    expect(inlineButton().className).toBe("milestone-copy")
    expect(inlineButton().title).toBe("Import EST-9 to Legato")

    inlineButton().click()
    expect(send).toHaveBeenCalledWith({ type: "importTicket", context: { provider: "burnlite", ticketId: "EST-9" } })
  })

  it("imports the drawer's task, reports success, and permits retry after an error", async () => {
    const send = vi.fn()
      .mockRejectedValueOnce(new Error("Burnlite is not connected"))
      .mockResolvedValueOnce({ ok: true, taskUrl: "http://localhost:3080/board?highlight_task=EST-3" })
    document.body.insertAdjacentHTML("beforeend", drawer("EST-3"))
    reconcileTicketAction(document, href, burnlite, send)

    inlineButton().click()
    inlineButton().click()
    expect(send).toHaveBeenCalledTimes(1)
    expect(send).toHaveBeenCalledWith({ type: "importTicket", context: { provider: "burnlite", ticketId: "EST-3" } })
    await vi.waitFor(() => expect(inlineButton().textContent).toContain("Retry import"))
    expect(inline()!.textContent).toContain("Burnlite is not connected")

    inlineButton().click()
    await vi.waitFor(() => expect(inlineButton().textContent).toContain("Imported to Legato"))
    expect(send).toHaveBeenCalledTimes(2)
  })

  it("opens the desktop app's link when the import goes there", async () => {
    const opened: string[] = []
    const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(function (this: HTMLAnchorElement) { opened.push(this.href) })
    const send = vi.fn().mockResolvedValue({ ok: true, desktopLink: "legato-desktop://import?provider=burnlite&key=EST-3" })
    document.body.insertAdjacentHTML("beforeend", drawer("EST-3"))
    reconcileTicketAction(document, href, burnlite, send)
    inlineButton().click()
    await vi.waitFor(() => expect(opened).toEqual(["legato-desktop://import?provider=burnlite&key=EST-3"]))
    await vi.waitFor(() => expect(inline()!.textContent).toContain("Sent to the Legato desktop app"))
    click.mockRestore()
  })

  it("shows the error a failed response carries", async () => {
    const send = vi.fn().mockResolvedValue({ ok: false, error: "EST-3 is already a task from jira" })
    document.body.insertAdjacentHTML("beforeend", drawer("EST-3"))
    reconcileTicketAction(document, href, burnlite, send)
    inlineButton().click()
    await vi.waitFor(() => expect(inline()!.textContent).toContain("EST-3 is already a task from jira"))
  })

  it("adds nothing for a drawer key Burnlite would never give", () => {
    document.body.insertAdjacentHTML("beforeend", drawer("not-a-key"))
    reconcileTicketAction(document, href, burnlite, vi.fn())
    expect(inline()).toBeNull()
  })
})
