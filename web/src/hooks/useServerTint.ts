import { useEffect, useState } from "react"
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

// The build version of the server at url ("" for this page's own), from the
// same public /api/meta. It needs no query client; an unreachable or older
// server has none.
export function useServerVersion(url: string): string {
  const [shown, setShown] = useState({ url: "", version: "" })
  useEffect(() => {
    let live = true
    const read = async () => {
      try {
        const response = await fetch(`${url}/api/meta`)
        if (!response.ok) return ""
        const meta = await response.json()
        return typeof meta?.version === "string" ? meta.version : ""
      } catch {
        return ""
      }
    }
    void read().then((version) => { if (live) setShown({ url, version }) })
    return () => { live = false }
  }, [url])
  return shown.url === url ? shown.version : ""
}
