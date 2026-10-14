import { apiFetch } from "./api"

// Where an agent can be started on the machine Legato runs on: the directory
// presets, and the directories that complete a path as it is typed. The
// server answers both (internal/server/directories.go).

export interface DirectoryPreset {
  name: string
  // Absolute.
  path: string
  // The path with the home directory written as a tilde.
  display: string
  // Saved by the person, as opposed to a repository of the configuration.
  saved: boolean
}

export interface DirectoryPresets {
  presets: DirectoryPreset[]
  // Where an agent starts when no directory is given.
  default: { path: string; display: string }
}

export interface DirectorySuggestion {
  name: string
  path: string
  // Ends in a slash, so that it can replace what was typed.
  display: string
  git: boolean
  // The directory's place among the presets, counted from 1, or 0.
  preset: number
}

export interface DirectorySuggestions {
  query: string
  resolved: string
  exists: boolean
  // The directory that was searched, and the start of a name typed after it.
  base: string
  prefix: string
  items: DirectorySuggestion[]
  total: number
}

// The presets, or null from a server that has none to give (an older Legato).
export async function fetchDirectoryPresets(baseUrl: string): Promise<DirectoryPresets | null> {
  const response = await apiFetch(baseUrl, "/api/directories/presets")
  if (!response.ok) return null
  const listed = await response.json().catch(() => null)
  if (!Array.isArray(listed?.presets) || typeof listed?.default?.display !== "string") return null
  return listed
}

export async function fetchDirectorySuggestions(baseUrl: string, query: string, signal?: AbortSignal): Promise<DirectorySuggestions | null> {
  const response = await apiFetch(baseUrl, `/api/directories?q=${encodeURIComponent(query)}`, { signal })
  if (!response.ok) return null
  return response.json()
}

// Saves a directory as a preset. It fails in the server's words.
export async function saveDirectoryPreset(baseUrl: string, path: string): Promise<DirectoryPreset> {
  const response = await apiFetch(baseUrl, "/api/directories/presets", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ path }),
  })
  const body = await response.json().catch(() => null)
  if (!response.ok) throw new Error(body?.error ?? "The preset was not saved")
  return body
}

function withoutTrailingSlash(path: string): string {
  const trimmed = path.trim()
  return trimmed.length > 1 ? trimmed.replace(/\/+$/, "") : trimmed
}

// The preset a typed path names, written either way.
export function presetFor(path: string, presets: DirectoryPreset[]): DirectoryPreset | undefined {
  const typed = withoutTrailingSlash(path)
  if (!typed) return undefined
  return presets.find((preset) => preset.display === typed || preset.path === typed)
}

// What a suggestion adds to the typed path, or "" when it does not continue it.
export function completionOf(typed: string, suggestion: DirectorySuggestion | undefined): string {
  if (!suggestion || suggestion.display.length <= typed.length) return ""
  if (suggestion.display.slice(0, typed.length).toLowerCase() !== typed.toLowerCase()) return ""
  return suggestion.display.slice(typed.length)
}

// A suggestion in three pieces: the directory it is in (base), the part of
// its name that was typed (prefix), and the rest.
export function suggestionParts(base: string, prefix: string, item: DirectorySuggestion): { base: string; typed: string; rest: string } {
  if (!base || !item.display.startsWith(base)) return { base: "", typed: "", rest: item.display }
  const name = item.display.slice(base.length)
  const typed = name.slice(0, prefix.length).toLowerCase() === prefix.toLowerCase() ? prefix.length : 0
  return { base, typed: name.slice(0, typed), rest: name.slice(typed) }
}

// The last part of a path, for naming where an agent will start.
export function directoryName(path: string): string {
  const trimmed = withoutTrailingSlash(path)
  return trimmed.slice(trimmed.lastIndexOf("/") + 1) || trimmed
}

// What to list for a path while the answer for it may still be on its way.
// The last answer is given as it is for the path it was asked for. For a
// longer path it is narrowed to what still matches, which is exact: more
// typing can only take suggestions away. When none of it matches (another
// directory was typed), the last answer is held, to be shown out of reach
// until the next one replaces it; a list that empties between answers is a
// list that flickers. prefix is the part of a name that is typed now.
export function suggestionsWhile(found: DirectorySuggestions | null, path: string): { items: DirectorySuggestion[]; held: boolean; prefix: string } {
  if (!found) return { items: [], held: false, prefix: "" }
  if (found.query === path) return { items: found.items, held: false, prefix: found.prefix }
  const typed = path.toLowerCase()
  const narrowed = found.items.filter((item) => item.display.length > path.length && item.display.toLowerCase().startsWith(typed))
  if (narrowed.length === 0) return { items: found.items, held: true, prefix: found.prefix }
  return { items: narrowed, held: false, prefix: path.slice(found.base.length) }
}
