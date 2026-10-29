import { passkeyForThisPage, passkeysSupported } from "../lib/passkeys"
import { useDevices } from "./useDevices"

/** The passkey made on this page that signs decisions for the server in use, if any. */
export function usePasskeyDevice() {
  const supported = passkeysSupported()
  const devices = useDevices({ enabled: supported })
  return supported ? passkeyForThisPage(devices.data?.devices ?? []) : undefined
}
