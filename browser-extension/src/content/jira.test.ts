// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest"
import { parseJiraTicket, reconcileJiraAction } from "./jira"

describe("parseJiraTicket", () => {
  it("extracts an uppercase issue key from the configured Jira browse URL", () => {
    expect(parseJiraTicket("https://urbanx-development.atlassian.net/browse/AG-9804")).toEqual({ ticketId: "AG-9804" })
    expect(parseJiraTicket("https://urbanx-development.atlassian.net/browse/ag-9804")).toEqual({ ticketId: "AG-9804" })
  })

  it.each([
    "https://urbanx-development.atlassian.net/issues/AG-9804",
    "https://other.atlassian.net/browse/AG-9804",
    "https://urbanx-development.atlassian.net/browse/not-a-ticket",
    "not a url",
  ])("rejects unsupported URL %s", (url) => expect(parseJiraTicket(url)).toBeNull())
})

describe("reconcileJiraAction", () => {
  beforeEach(() => { document.body.innerHTML = "<main>Ticket</main>" })

  it("renders one body-level bottom-right import action only on ticket pages", () => {
    const send = vi.fn()
    const href = "https://urbanx-development.atlassian.net/browse/AG-9804"
    reconcileJiraAction(document, href, send)
    reconcileJiraAction(document, href, send)

    const action = document.querySelector<HTMLElement>("#legato-jira-action")!
    expect(document.querySelectorAll("#legato-jira-action")).toHaveLength(1)
    expect(action.parentElement).toBe(document.body)
    expect(action.style.position).toBe("fixed")
    expect(action.style.right).toBe("24px")
    expect(action.style.bottom).toBe("24px")
    expect(action.textContent).toContain("LEGATO")
    expect(action.textContent).toContain("Jira ticket import")
    expect(action.textContent).toContain("AG-9804")
    expect(action.querySelector("button")?.textContent).toBe("Import AG-9804 to Legato")

    reconcileJiraAction(document, "https://urbanx-development.atlassian.net/issues", send)
    expect(document.querySelector("#legato-jira-action")).toBeNull()
  })

  it("imports once, reports success, and permits retry after an error", async () => {
    const send = vi.fn()
      .mockRejectedValueOnce(new Error("Jira ticket was not found"))
      .mockResolvedValueOnce({ ok: true, taskUrl: "http://localhost:3080/agents?task=task-1" })
    reconcileJiraAction(document, "https://urbanx-development.atlassian.net/browse/AG-9804", send)
    const button = document.querySelector<HTMLButtonElement>("button")!

    button.click()
    button.click()
    expect(send).toHaveBeenCalledTimes(1)
    await vi.waitFor(() => expect(button.textContent).toBe("Retry import AG-9804"))

    button.click()
    button.click()
    await vi.waitFor(() => expect(send).toHaveBeenCalledTimes(2))
    expect(send.mock.calls[1][0]).toEqual({ type: "importJiraTicket", context: { ticketId: "AG-9804" } })
    await vi.waitFor(() => expect(button.textContent).toBe("Imported to Legato"))
  })
})
