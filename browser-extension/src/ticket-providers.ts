// The ticket providers Legato is connected to, as GET /api/ticket-providers
// reports them: where each one's pages are and what its keys look like. The
// extension keeps the last list it read in chrome.storage.local, where the
// ticket content script reads it.
export type TicketProvider = {
  name: string
  label: string
  web_origin: string
  key_pattern: string
}

export const TICKET_PROVIDERS_KEY = "ticketProviders"

// The providers whose pages the content script knows how to read a key from.
export const SUPPORTED_TICKET_PROVIDERS: readonly string[] = ["jira", "burnlite"]

const DEFAULT_KEY_PATTERN = "^[A-Z][A-Z0-9_]*-[0-9]+$"

// The origin of an http(s) address that names only an origin, or null.
export function webOrigin(value: unknown): string | null {
  if (typeof value !== "string") return null
  let url: URL
  try { url = new URL(value) } catch { return null }
  if (url.protocol !== "http:" && url.protocol !== "https:") return null
  if (url.username || url.password) return null
  return url.origin
}

// The providers of a stored or fetched list the extension can use: a
// supported name and a web origin. Others are dropped.
export function normalizeTicketProviders(value: unknown): TicketProvider[] {
  if (!Array.isArray(value)) return []
  const providers: TicketProvider[] = []
  for (const entry of value) {
    if (!entry || typeof entry !== "object") continue
    const candidate = entry as Partial<Record<keyof TicketProvider, unknown>>
    if (typeof candidate.name !== "string" || !SUPPORTED_TICKET_PROVIDERS.includes(candidate.name)) continue
    const origin = webOrigin(candidate.web_origin)
    if (!origin) continue
    providers.push({
      name: candidate.name,
      label: typeof candidate.label === "string" && candidate.label ? candidate.label : candidate.name,
      web_origin: origin,
      key_pattern: typeof candidate.key_pattern === "string" ? candidate.key_pattern : "",
    })
  }
  return providers
}

// Whether key is one of the provider's keys. A provider that gives no pattern,
// or one JavaScript cannot read, gets the usual PROJECT-123 shape.
export function matchesKeyPattern(provider: TicketProvider, key: string): boolean {
  let pattern: RegExp
  try { pattern = new RegExp(provider.key_pattern || DEFAULT_KEY_PATTERN) } catch { pattern = new RegExp(DEFAULT_KEY_PATTERN) }
  return pattern.test(key)
}

export function hostPatternFor(origin: string): string {
  return `${origin}/*`
}
