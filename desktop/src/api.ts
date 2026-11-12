import { invoke } from "@tauri-apps/api/core"

// The app's commands (desktop/src-tauri/src/app.rs), typed.

export type Server = { id: string; name: string; url: string; tint?: string | null; pinned: boolean; keys: string[] }

// An open approval, as the server lists it.
export type Approval = {
  id: string
  title: string
  summary?: string
  source: string
  signed: boolean
  created_at?: string
  task_id?: string
  details?: { task_title?: string; agent_kind?: string }
}

// How the sheet on an approval came out, as a server's page hears it.
export type SheetOutcome = { status: "decided" | "cancelled" | "error"; option?: string; error?: string }

// What a server's page asked of the app (src-tauri/src/bridge.rs).
export type PageMessage = { server: string; kind: "approval"; id: string } | { server: string; kind: "enroll" }

// A server's page did not load (Linux; src-tauri/src/app.rs).
export type PageLoadFailed = { server: string; uri: string }

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
  // Why this copy cannot install it, such as a Mac copy not in Applications.
  reason?: string | null
}

// A ticket the browser extension handed the app (src-tauri/src/applink.rs).
export type ImportRequest = { provider: string; key: string }

// Where such a ticket goes: the app asks which server, or uses the one in front.
export type ImportTarget = "ask" | "current"

// The key that starts talking to the Reeve (src-tauri/src/voicekey.rs).
// key is the bound key as the person reads it; problem says why it does not
// work, when it does not, and link then still does what it would.
// backend is the way the key is had now: grabbed by the app, or the
// desktop's portal; choice is the way chosen for a Wayland desktop, which
// applies from the next start (can_choose: this desktop can). talk and
// cancel are the grabbed keys, as the plugin reads them.
export type KeyBackend = "grab" | "portal"
export type VoiceKeyInfo = {
  backend: KeyBackend | "link"
  choice: KeyBackend
  can_choose: boolean
  key?: string | null
  talk?: string | null
  cancel?: string | null
  hold_to_talk: boolean
  can_change: boolean
  problem?: string | null
  link: string
}

// The voice widget: shown when at rest, and the corner or edge it keeps
// to; dropped when the person dragged it to a spot of its own there.
export type Anchor = "bottom-right" | "bottom-left" | "top-right" | "top-left" | "top" | "bottom" | "left" | "right"
export type VoiceWidgetSettings = { shown: boolean; anchor: Anchor; dropped: boolean }

// Which server's Reeve the key talks to (src-tauri/src/voicemain.rs): each
// server with the instance it last said it is, the one talked to now, and
// whether the person must choose.
export type VoiceTargets = {
  servers: { server: string; name: string; instance?: { id: string; name: string; role: string } | null }[]
  talking_to?: string | null
  must_choose: boolean
  chosen?: string | null
}

export const api = {
  servers: () => invoke<Server[]>("servers"),
  takeImportRequests: () => invoke<ImportRequest[]>("take_import_requests"),
  importPreference: () => invoke<ImportTarget>("import_preference"),
  setImportPreference: (target: ImportTarget) => invoke<void>("set_import_preference", { target }),
  importTicket: (server: string, provider: string, key: string) => invoke<{ id: string; title?: string }>("import_ticket", { server, provider, key }),
  openTaskInPage: (server: string, task: string) => invoke<void>("open_task_in_page", { server, task }),
  showServer: (id: string) => invoke<void>("show_server", { id }),
  pageApprovalDone: (server: string, id: string, outcome: SheetOutcome) => invoke<void>("page_approval_done", { server, detail: { id, ...outcome } }),
  openApprovalInPage: (server: string, approval: string) => invoke<void>("open_approval_in_page", { server, approval }),
  // Whether this computer's web view will load the server's page: on a Mac,
  // whether the system trusts its certificate. Always true on Linux.
  pageTrusted: (id: string) => invoke<boolean>("page_trusted", { id }),
  trustServerCertificate: (id: string) => invoke<boolean>("trust_server_certificate", { id }),
  saveServerCertificate: (id: string) => invoke<string>("save_server_certificate", { id }),
  openCertificate: (path: string) => invoke<void>("open_certificate", { path }),
  setContentVisible: (visible: boolean) => invoke<void>("set_content_visible", { visible }),
  reloadServer: (id: string) => invoke<void>("reload_server", { id }),
  // Loads a page that did not load again: at uri when it is the server's own,
  // else at the server's root.
  loadServerPage: (id: string, uri?: string) => invoke<void>("load_server_page", { id, uri: uri || null }),
  pairServer: (url: string, phrase: string) => invoke<Server>("pair_server", { url, phrase }),
  discovered: () => invoke<{ name: string; url: string }[]>("discovered"),
  renameServer: (id: string, name: string) => invoke<void>("rename_server", { id, name }),
  reorderServers: (ids: string[]) => invoke<void>("reorder_servers", { ids }),
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
  voiceKeyInfo: () => invoke<VoiceKeyInfo>("voice_key_info"),
  changeVoiceKey: () => invoke<void>("change_voice_key"),
  setHoldToTalk: (on: boolean) => invoke<void>("set_hold_to_talk", { on }),
  // A key as recordedKey writes it; null is the default.
  setVoiceKeys: (talk: string | null, cancel: string | null) => invoke<void>("set_voice_keys", { talk, cancel }),
  setVoiceKeyBackend: (backend: KeyBackend) => invoke<void>("set_voice_key_backend", { backend }),
  voiceTargets: () => invoke<VoiceTargets>("voice_targets"),
  setVoiceServer: (instance: string | null) => invoke<void>("set_voice_server", { instance }),
  // The widget's buttons: talk (the key), stop, reply, send, cancel, end.
  voiceWidgetSettings: () => invoke<VoiceWidgetSettings>("voice_widget_settings"),
  setVoiceWidget: (shown: boolean, anchor: Anchor) => invoke<void>("set_voice_widget", { shown, anchor }),
  voiceAction: (action: "talk" | "stop" | "reply" | "send" | "cancel" | "end") => invoke<void>("voice_action", { action }),
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
