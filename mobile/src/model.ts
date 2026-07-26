export type Compatibility = "legacy" | "compatible" | "unsupported"

export type LegatoConnection = {
  id: string
  name: string
  baseUrl: string
  createdAt: string
  lastConnectedAt?: string
  serverVersion?: string
  compatibility?: Compatibility
  caCertificate?: string
}

export function normalizeServerUrl(value: string): string {
  const url = new URL(value.trim())
  if (!["http:", "https:"].includes(url.protocol) || url.username || url.password || url.search || url.hash) {
    throw new Error("Use an HTTP or HTTPS server URL without credentials, query, or fragment")
  }
  if (url.pathname !== "" && url.pathname !== "/") throw new Error("Server URL must not contain a path")
  return url.origin
}

export function parsePairUri(value: string): { baseUrl: string; token: string; suggestedName: string; caCertificate?: string } {
  const url = new URL(value.trim())
  if (url.protocol !== "legato:" || url.hostname !== "pair") throw new Error("Not a Legato pairing URI")
  if (url.searchParams.getAll("url").length !== 1 || url.searchParams.getAll("token").length !== 1) {
    throw new Error("Pairing URI must contain one URL and token")
  }
  const baseUrl = normalizeServerUrl(url.searchParams.get("url") ?? "")
  const token = url.searchParams.get("token")?.trim() ?? ""
  if (!token) throw new Error("Pairing token is missing")
  const caCertificate = url.searchParams.get("ca") ?? undefined
  if (caCertificate && !/^-----BEGIN CERTIFICATE-----[\s\S]+-----END CERTIFICATE-----\s*$/.test(caCertificate)) {
    throw new Error("Pairing certificate is invalid")
  }
  return { baseUrl, token, suggestedName: new URL(baseUrl).hostname, caCertificate }
}
