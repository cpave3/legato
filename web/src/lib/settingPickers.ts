import { createContext, useContext } from "react"
import type { ConfigList, ConfigSetting } from "./configSettings"

// The settings whose values are chosen from what Legato knows (agents and
// their models or tiers, board columns, workspaces, skills) rather than
// typed. See docs/ai/web-ui.md § Settings.

// An agent setting may carry its model or tier in a partner setting,
// drawn in the same row: `model` (a model or a tier, one key), or `tier`
// alone, or `models` and `tiers` in two keys of which one is set.
export interface AgentPicker {
  kind: "agent"
  modelOrTier?: string
  tier?: string
  model?: string
}

export type Picker =
  | AgentPicker
  | { kind: "column" }
  | { kind: "columns" }
  | { kind: "workspace"; sameAsNewTasks?: boolean }
  | { kind: "skill" }

export const settingPickers: Record<string, Picker> = {
  "swarm.conductor.agent": { kind: "agent", modelOrTier: "swarm.conductor.model" },
  "swarm.worker.agent": { kind: "agent", modelOrTier: "swarm.worker.model" },
  "learning.curator_adapter": { kind: "agent", tier: "learning.curator_tier" },
  "review_passes.reviewer.agent": { kind: "agent", model: "review_passes.reviewer.model", tier: "review_passes.reviewer.tier" },
  "reeve.agent": { kind: "agent", modelOrTier: "reeve.model" },
  "integrations.argus.agent": { kind: "agent" },
  "review_passes.review_column": { kind: "column" },
  "burnlite.columns.ready": { kind: "column" },
  "burnlite.columns.blocked": { kind: "column" },
  "burnlite.columns.claimed": { kind: "column" },
  "burnlite.columns.closed": { kind: "column" },
  "learning.transition_columns": { kind: "columns" },
  "default_workspace": { kind: "workspace" },
  "default_import_workspace": { kind: "workspace", sameAsNewTasks: true },
  "review_passes.reviewer.skill": { kind: "skill" },
}

// The keys drawn inside their agent's row, not as rows of their own.
export const partnerKeys: string[] = Object.values(settingPickers).flatMap((p) => p.kind === "agent" ? [p.modelOrTier, p.tier, p.model].filter((k): k is string => Boolean(k)) : [])

// What the pickers choose from.
export interface PickerData {
  agents: string[]
  defaultAgent: string
  tiers: Record<string, string[]>
  models: Record<string, string[]>
  columns: string[]
  workspaces: string[]
  skills: string[]
  // Every registry setting, for a row that draws its partner too.
  settings: Record<string, ConfigSetting>
}

export const emptyPickerData: PickerData = { agents: [], defaultAgent: "", tiers: {}, models: {}, columns: [], workspaces: [], skills: [], settings: {} }

export const PickerContext = createContext<PickerData>(emptyPickerData)

export function usePickerData(): PickerData {
  return useContext(PickerContext)
}

// pickerData reads the agents' tiers and models, the columns and the
// workspaces from the lists of named entries.
export function pickerData(lists: ConfigList[], settings: ConfigSetting[], adapters: { adapters: string[]; default: string }, skills: string[]): PickerData {
  const entries = (key: string) => lists.find((l) => l.key === key)?.entries ?? []
  const tiers: Record<string, string[]> = {}
  const models: Record<string, string[]> = {}
  for (const entry of entries("adapters")) {
    const value = (entry.value ?? {}) as { tiers?: Record<string, unknown>; models?: string[] }
    tiers[entry.name] = Object.keys(value.tiers ?? {})
    models[entry.name] = Array.isArray(value.models) ? value.models : []
  }
  return {
    agents: adapters.adapters,
    defaultAgent: adapters.default,
    tiers, models,
    columns: entries("board.columns").map((e) => e.name),
    workspaces: entries("workspaces").map((e) => e.name),
    skills,
    settings: Object.fromEntries(settings.map((s) => [s.key, s])),
  }
}
