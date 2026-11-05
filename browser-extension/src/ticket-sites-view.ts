import { hostPatternFor } from "./ticket-providers"
import type { TicketSite, TicketSitesResult } from "./ticket-sites"

// The options page's Ticket sites list: each provider Legato reports, where
// its pages are, and whether the extension may add its import button there.
export function renderTicketSites(container: HTMLElement, result: TicketSitesResult, onAllow: (site: TicketSite) => void): void {
  const doc = container.ownerDocument
  const children: HTMLElement[] = []
  if (result.sites.length === 0) {
    const empty = doc.createElement("p")
    empty.className = "ticket-sites-empty"
    empty.textContent = "Legato reports no ticket sites. Connect Burnlite or set up Jira in Legato, then reload this page."
    children.push(empty)
  } else {
    const list = doc.createElement("ul")
    list.className = "ticket-sites"
    for (const site of result.sites) {
      const row = doc.createElement("li")
      const label = doc.createElement("strong")
      label.textContent = site.label
      const origin = doc.createElement("code")
      origin.textContent = site.web_origin
      const state = doc.createElement("span")
      state.className = "ticket-site-state"
      state.dataset.allowed = String(site.allowed)
      state.textContent = site.allowed ? "Allowed" : "Not allowed"
      row.append(label, origin, state)
      if (!site.allowed) {
        const allow = doc.createElement("button")
        allow.type = "button"
        allow.textContent = "Allow"
        allow.addEventListener("click", () => onAllow(site))
        row.append(allow)
      }
      list.append(row)
    }
    children.push(list)
  }
  if (result.error) {
    const error = doc.createElement("p")
    error.className = "ticket-sites-error"
    error.textContent = result.error
    children.push(error)
  }
  container.replaceChildren(...children)
}

// Chrome grants a host permission only from a click, so the request goes out
// before anything else is awaited; the sites are synced either way, so the
// list shows what Chrome now allows.
export async function allowTicketSite(
  site: TicketSite,
  deps: { request: (origins: string[]) => Promise<boolean>; sync: () => Promise<TicketSitesResult> },
): Promise<TicketSitesResult> {
  let granted = false
  let failure: string | undefined
  try {
    granted = await deps.request([hostPatternFor(site.web_origin)])
  } catch (error) {
    failure = error instanceof Error ? error.message : String(error)
  }
  const result = await deps.sync()
  if (granted) return result
  return { ...result, error: failure ?? `Access to ${site.web_origin} was not granted` }
}
