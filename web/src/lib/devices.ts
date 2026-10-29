import { apiFetch } from "./api"

/** A device that signs approval decisions. */
export type Device = {
  id: string
  kind: "android" | "passkey" | string
  name: string
  security_level?: string
  status: "active" | "pending" | string
  approval_id?: string
  created_at: string
  last_used_at?: string
}

export type DeviceList = { devices: Device[]; enrolled: boolean; server_id: string }

async function errorOf(resp: Response): Promise<Error> {
  const body = await resp.json().catch(() => ({}))
  return new Error((body as { error?: string }).error || `HTTP ${resp.status}`)
}

export async function listDevices(baseUrl: string): Promise<DeviceList> {
  const resp = await apiFetch(baseUrl, "/api/devices")
  if (!resp.ok) throw await errorOf(resp)
  return resp.json()
}

/** Removing an enrolled device is an approval to sign on a phone. */
export async function removeDevice(baseUrl: string, id: string): Promise<{ removed?: boolean; approval_id?: string }> {
  const resp = await apiFetch(baseUrl, `/api/devices/${encodeURIComponent(id)}`, { method: "DELETE" })
  if (!resp.ok) throw await errorOf(resp)
  return resp.json()
}
