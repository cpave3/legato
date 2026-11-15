import { useQuery } from "@tanstack/react-query"
import { fetchConfigLists, fetchConfigSettings } from "../../lib/configSettings"
import { queryKeys } from "../../lib/query"

export function useSettingsCatalog(baseUrl: string) {
  return useQuery({
    queryKey: queryKeys.configSettings(baseUrl),
    queryFn: () => fetchConfigSettings(baseUrl),
    retry: false,
  })
}
export function useSettingsLists(baseUrl: string) {
  return useQuery({ queryKey: queryKeys.configLists(baseUrl), queryFn: () => fetchConfigLists(baseUrl), retry: false })
}
