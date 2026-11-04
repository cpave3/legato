import { invoke } from "@tauri-apps/api/core"

// The app's commands (desktop/src-tauri/src/app.rs), typed.

export type Server = { id: string; name: string; url: string; tint?: string | null; pinned: boolean; keys: string[] }

export type Approval = {
  id: string
  title: string
  summary?: string
  source: string
  signed: boolean
  created_at?: string
}

export type Status = {
  id: string
  online: boolean
  error?: string | null
  version?: string | null
  tint?: string | null
  approvals: Approval[]
}

// A security key, or Touch ID on a Mac. plugged: the key is plugged in, or
// Touch ID is set up.
export type Key = { id: string; name: string; kind: "security-key" | "touch-id"; plugged: boolean; servers: Record<string, "active" | "pending"> }

export type TouchId = { mac: boolean; available: boolean; reason?: string }

type Added = { key: string; servers: Record<string, { status?: string; error?: string }> }

export type Option = { id: string; label: string; kind: "allow" | "deny" | string }

// The request a key signs, as the server gave it: what the sheet draws.
export type Request = {
  approval: string
  server: string
  source: string
  title: string
  summary?: string
  details?: Record<string, unknown> & {
    credential?: string
    operations?: string[]
    resources?: string[]
    reason?: string
    task_title?: string
    agent_kind?: string
    device_name?: string
  }
  options: Option[]
}

export type Signed = { device: string; message_sha256: string; signature: string; verified: boolean; counter: number }

export type UpdateStatus = {
  version: string
  latest?: { version: string; page: string } | null
  available: boolean
  can_install: boolean
}

export const api = {
  servers: () => invoke<Server[]>("servers"),
  showServer: (id: string) => invoke<void>("show_server", { id }),
  setContentVisible: (visible: boolean) => invoke<void>("set_content_visible", { visible }),
  reloadServer: (id: string) => invoke<void>("reload_server", { id }),
  pairServer: (url: string, phrase: string) => invoke<Server>("pair_server", { url, phrase }),
  discovered: () => invoke<{ name: string; url: string }[]>("discovered"),
  renameServer: (id: string, name: string) => invoke<void>("rename_server", { id, name }),
  removeServer: (id: string) => invoke<void>("remove_server", { id }),
  poll: () => invoke<Status[]>("poll"),
  keys: () => invoke<Key[]>("keys"),
  addKey: (name: string, pin: string | null) => invoke<Added>("add_key", { name, pin }),
  touchId: () => invoke<TouchId>("touch_id"),
  addTouchId: (name: string) => invoke<Added>("add_touch_id", { name }),
  enrollKey: (key: string, server: string) => invoke<string>("enroll_key", { key, server }),
  revokeKey: (key: string) => invoke<Record<string, unknown>>("revoke_key", { key }),
  prepare: (server: string, approval: string) => invoke<{ request: Request; device: string; with: "security-key" | "touch-id" }>("prepare", { server, approval }),
  sign: (server: string, approval: string, option: string) => invoke<Signed>("sign", { server, approval, option }),
  deny: (server: string, approval: string) => invoke<void>("deny", { server, approval }),
  checkUpdate: () => invoke<UpdateStatus>("check_update"),
  applyUpdate: () => invoke<string>("apply_update"),
  restartApp: () => invoke<void>("restart_app"),
}

// The colours a Legato can be tinted, as web/src/lib/tints.ts has them.
const tints: Record<string, string> = {
  red: "#ef4444",
  orange: "#f97316",
  yellow: "#eab308",
  green: "#22c55e",
  teal: "#14b8a6",
  blue: "#3b82f6",
  purple: "#a855f7",
  pink: "#ec4899",
}

export function tintColor(name?: string | null): string {
  return (name && tints[name]) || "#6D5EF6"
}

export function initials(name: string): string {
  const words = name.split(/[\s._-]+/).filter(Boolean)
  if (words.length >= 2) return (words[0][0] + words[1][0]).toUpperCase()
  return name.slice(0, 2).toUpperCase()
}

export function errorText(e: unknown): string {
  return typeof e === "string" ? e : e instanceof Error ? e.message : String(e)
}
