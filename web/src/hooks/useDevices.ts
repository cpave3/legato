import { useQuery } from "@tanstack/react-query"
import { listDevices, type DeviceList } from "../lib/devices"
import { queryKeys } from "../lib/query"
import { useServer } from "./useServer"

/**
 * The devices that sign approval decisions on the server in use. A device
 * is enrolled or removed by a decided approval, so approvals_changed
 * refreshes them.
 */
export function useDevices(options: { enabled?: boolean; baseUrl?: string } = {}) {
  const server = useServer()
  const baseUrl = options.baseUrl ?? server.baseUrl
  return useQuery<DeviceList, Error>({
    queryKey: queryKeys.devices(baseUrl),
    queryFn: () => listDevices(baseUrl),
    enabled: options.enabled ?? true,
  })
}
