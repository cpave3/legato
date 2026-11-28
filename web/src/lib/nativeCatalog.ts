import type { ModelCapabilities, ModelSettings } from "./modelSettings"
import { reasonText } from "./nativeAgentView"
import type { SlashItem } from "./slashMenu"

// What a native session offers its operator: its modes, its models, its
// commands and skills, and Legato's macros.

export interface ModeInfo {
  name: string
  description: string
  cycle: boolean
}

export interface CommandInfo {
  name: string
  description?: string
  body: string
  path: string
  source: string
}

export interface SkillInfo {
  name: string
  description: string
  path: string
  source: string
}

export interface ModelInfo {
  capabilities?: ModelCapabilities
  defaults?: ModelSettings
  ref: string
  contextWindow?: number
  maxOutputTokens?: number
  vision?: boolean
  isDefault: boolean
}

export interface MacroInfo {
  name: string
  keys: string
  favorite?: boolean
}

// The commands and skills of one session (key) as far as they have loaded,
// and why either could not.
export interface Catalog {
  key: string
  commands: CommandInfo[]
  skills: SkillInfo[]
  loading: boolean
  errors: string[]
}

export type CatalogKind = "commands" | "skills"

// A catalog loading again: what it held is kept while it is the same
// session's.
export function startCatalog(previous: Catalog, key: string): Catalog {
  return {
    key,
    commands: previous.key === key ? previous.commands : [],
    skills: previous.key === key ? previous.skills : [],
    loading: true, errors: [],
  }
}

export function catalogError(kind: CatalogKind, error: unknown): string {
  return `${kind === "commands" ? "Commands" : "Skills"} unavailable: ${reasonText(error)}`
}

// The list a catalog endpoint answered, or why it is not one.
export async function catalogItems(response: Response): Promise<unknown[]> {
  if (!response.ok) throw new Error(`HTTP ${response.status}`)
  const items = await response.json()
  if (!Array.isArray(items)) throw new Error("Invalid catalog response")
  return items
}

// The slash menu's items: the session's commands, then its skills. A
// catalog of another session offers none.
export function slashItemsOf(catalog: Catalog, key: string): SlashItem[] {
  const current = catalog.key === key
  return [
    ...(current ? catalog.commands : []).map((c) => ({ type: "command" as const, name: c.name, description: c.description || "", body: c.body, source: c.source })),
    ...(current ? catalog.skills : []).map((s) => ({ type: "skill" as const, name: s.name, description: s.description || "", source: s.source })),
  ]
}

// Whether the session's catalog is still loading, and its errors.
export function catalogStatus(catalog: Catalog, key: string): { loading: boolean; errors: string[] } {
  return { loading: catalog.key !== key || catalog.loading, errors: catalog.key === key ? catalog.errors : [] }
}

export function listOrEmpty<T>(data: unknown): T[] {
  return Array.isArray(data) ? data : []
}
