import type { ConfigList, ConfigSetting } from "./configSettings"
import { findSection, listPlace, settingPlace } from "./settingsCatalog"

// A change that waits for a restart: a setting, with what Legato started
// with, or a list entry by name.
export interface WaitingChange {
  id: string
  name: string
  setting?: ConfigSetting
}

export function waitingChanges(settings: ConfigSetting[], lists: ConfigList[]): WaitingChange[] {
  const out: WaitingChange[] = []
  for (const s of settings) {
    if (!s.pending_restart) continue
    out.push({ id: `setting-${s.key}`, name: `${findSection(settingPlace(s).section)?.title ?? "System"} › ${s.title}`, setting: s })
  }
  for (const l of lists) {
    for (const entry of l.pending_entries ?? []) {
      out.push({ id: `list-${l.key}:${entry}`, name: `${findSection(listPlace(l.key).section)?.title ?? "System"} › ${l.title} › ${entry}` })
    }
  }
  return out
}

// Undo goes back to the value Legato started with: a reset when that was
// the default, else the value itself.
export function undoIsReset(setting: ConfigSetting): boolean {
  return JSON.stringify(setting.started_value ?? null) === JSON.stringify(setting.default ?? null)
}
