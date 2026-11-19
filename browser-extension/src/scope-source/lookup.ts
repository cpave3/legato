import type { PinSource } from "../scope-types"
import { viteResolver } from "./vite-resolver"
import {
  MAX_FRAMES, MAX_NAME_LENGTH, MAX_OWNERS, MAX_URL_LENGTH,
  type ComponentReading, type FetchText, type RawFrame, type SourceResolver,
} from "./types"

// The service worker's half of a pin's source (see types.ts).

export const RESOLVERS: SourceResolver[] = [viteResolver]

// A component's name as the agent will read it: a name, not a sentence.
const NAME = new RegExp(`^[\\p{L}\\p{N}_$.:()<>\\[\\], -]{1,${MAX_NAME_LENGTH}}$`, "u")
const MAX_POSITION = 10_000_000

const isRecord = (value: unknown): value is Record<string, unknown> => Boolean(value) && typeof value === "object" && !Array.isArray(value)
const isPosition = (value: unknown, min: number): value is number => Number.isSafeInteger(value) && (value as number) >= min && (value as number) <= MAX_POSITION

function validFrame(value: unknown): RawFrame | null {
  if (!isRecord(value)) return null
  const { url, line, column, original } = value
  if (typeof url !== "string" || url.length > MAX_URL_LENGTH || !isPosition(line, 1) || !isPosition(column, 0) || typeof original !== "boolean") return null
  return { url, line, column, original }
}

// What the page returned, if it has the shape the reader gives and keeps
// within its bounds; else null. Frames on another origin are dropped: only
// the page's own origin is fetched, and a file on disk is taken only as a
// name with a position already in it.
export function validateReading(value: unknown, origin: string): ComponentReading | null {
  if (!isRecord(value)) return null
  const { framework, name, owners, frames } = value
  if (typeof framework !== "string" || !/^[a-z][a-z0-9-]{0,31}$/.test(framework)) return null
  if (name !== undefined && (typeof name !== "string" || !NAME.test(name))) return null
  if (!Array.isArray(owners) || owners.length > MAX_OWNERS || !owners.every((owner) => typeof owner === "string" && NAME.test(owner))) return null
  if (!Array.isArray(frames) || frames.length > MAX_FRAMES) return null
  const kept: RawFrame[] = []
  for (const candidate of frames) {
    const frame = validFrame(candidate)
    if (!frame) return null
    let url: URL
    try {
      url = new URL(frame.url)
    } catch {
      return null
    }
    if (url.origin === origin && (url.protocol === "http:" || url.protocol === "https:")) kept.push(frame)
    else if (url.protocol === "file:" && frame.original) kept.push(frame)
  }
  return { framework, ...(name !== undefined ? { name } : {}), owners: owners as string[], frames: kept }
}

export type LookupContext = { origin: string; fetchText: FetchText; resolvers?: SourceResolver[] }

// The pin's source: the first frame a resolver places in the project, with
// the component's name and owners; name-only when no frame is placed; none
// when the page gave nothing usable.
export async function resolvePinSource(reading: unknown, context: LookupContext): Promise<PinSource> {
  const valid = validateReading(reading, context.origin)
  if (!valid) return { confidence: "none" }
  const named: Omit<PinSource, "confidence"> = {
    ...(valid.name ? { name: valid.name } : {}),
    ...(valid.owners.length ? { owners: valid.owners } : {}),
  }
  for (const frame of valid.frames) {
    for (const resolver of context.resolvers ?? RESOLVERS) {
      const place = await resolver.resolve(frame, context).catch(() => null)
      if (!place) continue
      const exact = place.line !== undefined
      return {
        ...named,
        file: place.file,
        ...(exact ? { line: place.line } : {}),
        ...(exact && place.column ? { column: place.column } : {}),
        confidence: exact ? "exact" : "file-only",
      }
    }
  }
  return valid.name ? { ...named, confidence: "name-only" } : { confidence: "none" }
}
