import { apiFetch } from "./api"

// One thing on the setup checklist (service.SetupItem).
export interface SetupItem {
  id: string
  title: string
  done: boolean
  // Listed but not counted, such as a ticket tracker.
  optional?: boolean
  // The part of Settings that does it (#<settings>).
  settings?: string
}

// The setup checklist (GET /api/setup, service.SetupStatus).
export interface SetupStatus {
  items: SetupItem[]
  done: number
  total: number
  dismissed: boolean
  show: boolean
}

// The chip's part of it, carried by /api/settings.
export interface SetupSummary {
  show: boolean
  done: number
  total: number
}

async function status(response: Response): Promise<SetupStatus> {
  if (!response.ok) {
    const body = await response.json().catch(() => null) as { error?: string } | null
    throw new Error(body?.error || response.statusText)
  }
  return response.json()
}

export async function fetchSetup(baseUrl: string): Promise<SetupStatus> {
  return status(await apiFetch(baseUrl, "/api/setup"))
}

// setSetupDismissed hides the chip on every device of this Legato, or
// brings it back.
export async function setSetupDismissed(baseUrl: string, dismissed: boolean): Promise<SetupStatus> {
  return status(await apiFetch(baseUrl, "/api/setup/dismissed", {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ dismissed }),
  }))
}
