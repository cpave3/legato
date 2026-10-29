import { useQuery } from "@tanstack/react-query"
import { listDevices } from "../lib/devices"
import { passkeyForThisPage, passkeysSupported } from "../lib/passkeys"
import { queryKeys } from "../lib/query"
import { useServer } from "./useServer"

/** The passkey made on this page that signs decisions for the server in use, if any. */
export function usePasskeyDevice() {
  const { baseUrl } = useServer()
  const supported = passkeysSupported()
  const query = useQuery({
    queryKey: queryKeys.devices(baseUrl),
    queryFn: () => listDevices(baseUrl),
    enabled: supported,
    staleTime: 60_000,
  })
  return supported ? passkeyForThisPage(query.data?.devices ?? []) : undefined
}
