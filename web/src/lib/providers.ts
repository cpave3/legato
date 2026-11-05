// How the web UI names the places tasks come from. A card shows a task's
// provider by its id ("jira", "burnlite"); prose and buttons use its name.

const known: { id: string; label: string; item: string }[] = [
  { id: "jira", label: "Jira", item: "issue" },
  { id: "burnlite", label: "Burnlite", item: "task" },
  { id: "github", label: "GitHub", item: "PR" },
  // A sync's result also names the workers a main pulled.
  { id: "workers", label: "Workers", item: "task" },
]

/**
 * The provider a worker's copy of a handed-off task came from ("burnlite"
 * for "burnlite-copy"), or "" for a provider that is not a copy. The copy's
 * link is its provider's.
 */
export function handedOffFrom(provider: string): string {
  return provider.endsWith("-copy") ? provider.slice(0, -"-copy".length) : ""
}

/** A provider's name for people: Jira, Burnlite, GitHub; a copy by its provider's; any other by its own id. */
export function providerLabel(provider: string): string {
  const id = handedOffFrom(provider) || provider
  return known.find((p) => p.id === id)?.label ?? id
}

/** What one of a provider's items is called: a Jira issue, a Burnlite task. */
export function providerItem(provider: string): string {
  const id = handedOffFrom(provider) || provider
  return known.find((p) => p.id === id)?.item ?? "ticket"
}

/** The known providers first, in a fixed order, then the rest by id. */
export function sortProviders(providers: string[]): string[] {
  const rank = (id: string) => {
    const index = known.findIndex((p) => p.id === id)
    return index === -1 ? known.length : index
  }
  return [...providers].sort((a, b) => rank(a) - rank(b) || a.localeCompare(b))
}
