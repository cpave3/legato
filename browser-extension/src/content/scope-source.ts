import type { PinSource } from "../scope-types"
import { PROBE_ATTRIBUTE } from "../scope-source/types"

// The component that drew a pinned element. The page script that reads it
// runs in the page's world, which cannot be handed an element: the element
// is marked with an attribute while the service worker runs that script and
// places what it read in the project (see scope-source/types.ts). A pin
// never waits on this for long: past the bound, or on any failure, the
// source is none.

export const SOURCE_TIMEOUT_MS = 1500

const NONE: PinSource = { confidence: "none" }

export async function lookupSource(
  element: Element,
  ask: (message: { type: "scopeSource" }) => Promise<unknown>,
  timeoutMs: number = SOURCE_TIMEOUT_MS,
): Promise<PinSource> {
  const doc = element.ownerDocument
  for (const stale of doc.querySelectorAll(`[${PROBE_ATTRIBUTE}]`)) stale.removeAttribute(PROBE_ATTRIBUTE)
  element.setAttribute(PROBE_ATTRIBUTE, "")
  let timer: number | undefined
  try {
    const answer = await Promise.race([
      ask({ type: "scopeSource" }).catch(() => undefined),
      new Promise<undefined>((resolve) => { timer = setTimeout(resolve, timeoutMs) as unknown as number }),
    ]) as { ok?: unknown; source?: unknown } | undefined
    const source = answer?.ok === true ? answer.source as PinSource | undefined : undefined
    return source && typeof source === "object" && typeof source.confidence === "string" ? source : NONE
  } finally {
    clearTimeout(timer)
    element.removeAttribute(PROBE_ATTRIBUTE)
  }
}
