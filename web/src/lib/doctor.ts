import { apiFetch } from "./api"

// One check of what Legato needs from its machine (service.DoctorCheck).
export interface DoctorCheck {
  id: string
  title: string
  required: boolean
  required_by?: string
  result: "ok" | "missing" | "not_signed_in" | "failed" | "skipped"
  detail?: string
  fix?: string
}

// The doctor's report (GET /api/doctor): ok when every required check passed.
export interface DoctorReport {
  checks: DoctorCheck[]
  online: boolean
  checked_at: string
  ok: boolean
}

export function checkFailed(check: DoctorCheck): boolean {
  return check.result !== "ok" && check.result !== "skipped"
}

export function requiredFailed(report: DoctorReport | undefined): DoctorCheck[] {
  return (report?.checks ?? []).filter((check) => check.required && checkFailed(check))
}

async function report(response: Response): Promise<DoctorReport> {
  if (!response.ok) {
    const body = await response.json().catch(() => null) as { error?: string } | null
    throw new Error(body?.error || response.statusText)
  }
  return response.json()
}

export async function fetchDoctor(baseUrl: string): Promise<DoctorReport> {
  return report(await apiFetch(baseUrl, "/api/doctor"))
}

// recheckDoctor runs every check again (Check again).
export async function recheckDoctor(baseUrl: string): Promise<DoctorReport> {
  return report(await apiFetch(baseUrl, "/api/doctor/check", { method: "POST" }))
}
