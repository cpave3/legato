import { useQuery } from "@tanstack/react-query"
import { fetchConfigSettings } from "../lib/configSettings"
import { queryKeys } from "../lib/query"

// useConfigSettings is the server's registry of settings, refetched when a
// settings_changed message says another browser changed one.
export function useConfigSettings(baseUrl: string) {
  return useQuery({
    queryKey: queryKeys.configSettings(baseUrl),
    queryFn: () => fetchConfigSettings(baseUrl),
    retry: false,
  })
}
