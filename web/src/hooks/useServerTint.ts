import { useQuery } from "@tanstack/react-query"

// The tint of the server at url ("" for this page's own), read from its
// public /api/meta, so a switcher shows it before it has a token for it.
// A server that cannot be reached, or an older one, has none.
export function useServerTint(url: string): string {
  const { data } = useQuery({
    queryKey: [url, "server-tint"],
    queryFn: async () => {
      try {
        const response = await fetch(`${url}/api/meta`)
        if (!response.ok) return ""
        const meta = await response.json()
        return typeof meta?.tint === "string" ? meta.tint : ""
      } catch {
        return ""
      }
    },
  })
  return data ?? ""
}
