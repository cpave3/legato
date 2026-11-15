import { useLayoutEffect, type ReactNode } from "react"
import { useQuery } from "@tanstack/react-query"
import { useServer } from "../hooks/useServer"
import { apiFetch } from "../lib/api"
import { queryKeys } from "../lib/query"
import { activateThemeConnection, cacheServerTheme, themeConnectionEpoch, type ThemePreference } from "../lib/serverTheme"
import { applyAppTheme, THEME_CHANGED } from "../lib/appTheme"

// A failed request retains the connection's cached preference; a successful
// response without the field means an older server and clears that preference.
export function ServerThemeProvider({ children }: { children: ReactNode }) {
  const { baseUrl } = useServer()
  useLayoutEffect(() => {
    activateThemeConnection(baseUrl)
    return () => activateThemeConnection(undefined)
  }, [baseUrl])
  const meta = useQuery({
    queryKey: [...queryKeys.meta(baseUrl), themeConnectionEpoch(baseUrl)],
    queryFn: async ({ signal }) => {
      const response = await apiFetch(baseUrl, "/api/meta", { signal, cache: "no-store" })
      if (!response.ok) throw new Error("Could not load server metadata")
      return response.json() as Promise<{ theme_preference?: ThemePreference }>
    },
  })
  useLayoutEffect(() => {
    if (meta.data) cacheServerTheme(baseUrl, meta.data.theme_preference)
    applyAppTheme()
    window.dispatchEvent(new Event(THEME_CHANGED))
  }, [baseUrl, meta.data])
  return children
}
