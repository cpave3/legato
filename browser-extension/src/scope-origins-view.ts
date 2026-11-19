import type { ScopeOrigin } from "./scope-origins"

export type ScopeOriginsResult = { origins: ScopeOrigin[]; error?: string }

// The options page's Scope sites: each origin the toolbar button allowed,
// with Remove.
export function renderScopeOrigins(container: HTMLElement, result: ScopeOriginsResult, onRemove: (origin: string) => void): void {
  const doc = container.ownerDocument
  const children: HTMLElement[] = []
  if (result.origins.length === 0) {
    const empty = doc.createElement("p")
    empty.className = "scope-origins-empty"
    empty.textContent = "No sites yet. Open a page of your site and click the extension's toolbar button (or press its shortcut) to allow Scope there."
    children.push(empty)
  } else {
    const list = doc.createElement("ul")
    list.className = "ticket-sites scope-origins"
    for (const entry of result.origins) {
      const row = doc.createElement("li")
      const origin = doc.createElement("code")
      origin.textContent = entry.origin
      row.append(origin)
      if (!entry.allowed) {
        const state = doc.createElement("span")
        state.className = "ticket-site-state"
        state.dataset.allowed = "false"
        state.textContent = "Access removed in Chrome"
        row.append(state)
      }
      const remove = doc.createElement("button")
      remove.type = "button"
      remove.textContent = "Remove"
      remove.addEventListener("click", () => onRemove(entry.origin))
      row.append(remove)
      list.append(row)
    }
    children.push(list)
  }
  if (result.error) {
    const error = doc.createElement("p")
    error.className = "ticket-sites-error scope-origins-error"
    error.textContent = result.error
    children.push(error)
  }
  container.replaceChildren(...children)
}
