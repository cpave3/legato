// @vitest-environment jsdom
// @vitest-environment-options {"url":"https://urbanx-development.atlassian.net/browse/AG-9804"}
import { afterEach, expect, it, vi } from "vitest"

afterEach(() => {
  document.body.replaceChildren()
  vi.unstubAllGlobals()
  vi.resetModules()
})

it("boots on a Jira ticket page and sends the import through Chrome messaging", async () => {
  const sendMessage = vi.fn().mockResolvedValue({ ok: true, taskUrl: "http://localhost:3080/board?highlight_task=AG-9804" })
  vi.stubGlobal("chrome", { runtime: { id: "", sendMessage } })
  document.body.innerHTML = "<main>AG-9804</main>"

  const module = await import("./jira")
  const cleanup = module.initializeJiraContentScript()
  await vi.waitFor(() => {
    const button = document.querySelector<HTMLButtonElement>("#legato-jira-action button")
    expect(button?.textContent).toBe("Import AG-9804 to Legato")
    button?.click()
  })

  await vi.waitFor(() => expect(sendMessage).toHaveBeenCalledWith({
    type: "importJiraTicket",
    context: { ticketId: "AG-9804" },
  }))
  await vi.waitFor(() => expect(document.querySelector("#legato-jira-action button")?.textContent).toBe("Imported to Legato"))
  cleanup()
})
