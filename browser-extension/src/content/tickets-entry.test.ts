// @vitest-environment jsdom
// @vitest-environment-options {"url":"https://burnlite.test/project/0b7c6c1e?item=EST-42"}
import { afterEach, expect, it, vi } from "vitest"

const providers = [
  { name: "jira", label: "Jira", web_origin: "https://example.atlassian.net", key_pattern: "^[A-Z][A-Z0-9_]*-[0-9]+$" },
  { name: "burnlite", label: "Burnlite", web_origin: "https://burnlite.test", key_pattern: "^[A-Z][A-Z0-9_]*-[0-9]+$" },
]

afterEach(() => {
  document.body.replaceChildren()
  vi.unstubAllGlobals()
  vi.resetModules()
})

function stubChrome(stored: Record<string, unknown>) {
  const sendMessage = vi.fn().mockResolvedValue({ ok: true, taskUrl: "http://localhost:3080/board?highlight_task=EST-42" })
  const get = vi.fn().mockResolvedValue(stored)
  vi.stubGlobal("chrome", { runtime: { id: "", sendMessage }, storage: { local: { get } } })
  return { sendMessage, get }
}

it("boots on a provider's page from the stored providers and sends the import through Chrome messaging", async () => {
  const { sendMessage, get } = stubChrome({ ticketProviders: providers })
  document.body.innerHTML = "<main>EST-42</main>"

  const module = await import("./tickets")
  const cleanup = await module.initializeTicketContentScript()
  expect(get).toHaveBeenCalledWith("ticketProviders")
  await vi.waitFor(() => {
    const button = document.querySelector<HTMLButtonElement>("#legato-ticket-action button")
    expect(button?.textContent).toBe("Import EST-42 to Legato")
    button?.click()
  })

  await vi.waitFor(() => expect(sendMessage).toHaveBeenCalledWith({
    type: "importTicket",
    context: { provider: "burnlite", ticketId: "EST-42" },
  }))
  await vi.waitFor(() => expect(document.querySelector("#legato-ticket-action button")?.textContent).toBe("Imported to Legato"))
  cleanup()
})

it("re-checks the page when the drawer changes the address without a navigation event", async () => {
  stubChrome({ ticketProviders: providers })
  const module = await import("./tickets")
  const cleanup = await module.initializeTicketContentScript()
  await vi.waitFor(() => expect(document.querySelector("#legato-ticket-action button")?.textContent).toBe("Import EST-42 to Legato"))

  history.replaceState(null, "", "/project/0b7c6c1e?item=EST-43")
  document.body.append(document.createElement("aside"))
  await vi.waitFor(() => expect(document.querySelector("#legato-ticket-action button")?.textContent).toBe("Import EST-43 to Legato"))

  history.replaceState(null, "", "/project/0b7c6c1e?item=EST-42")
  cleanup()
})

it("adds nothing on a site no stored provider names", async () => {
  stubChrome({ ticketProviders: providers.filter((provider) => provider.name !== "burnlite") })
  document.body.innerHTML = "<main>EST-42</main>"
  const module = await import("./tickets")
  const cleanup = await module.initializeTicketContentScript()
  await new Promise((resolve) => setTimeout(resolve, 0))
  expect(document.querySelector("#legato-ticket-action")).toBeNull()
  cleanup()
})

it("adds nothing when nothing is stored", async () => {
  stubChrome({})
  const module = await import("./tickets")
  const cleanup = await module.initializeTicketContentScript()
  await new Promise((resolve) => setTimeout(resolve, 0))
  expect(document.querySelector("#legato-ticket-action")).toBeNull()
  cleanup()
})
