/** Navigation kinds shared with the attention registry and task-activity. */
export type LegatoNavigationKind = "plan" | "review" | "workflow_run" | "task" | "approval"

export interface LegatoNavigation {
  kind: LegatoNavigationKind
  target: string
}

/**
 * Maps a Legato navigation descriptor (used by the attention registry and
 * task-activity) to a frontend route. Returns null for unknown kinds so
 * callers can skip rendering instead of producing a broken link.
 *
 * Accepts a loose `kind: string` because the attention registry's
 * `AttentionNavigation.kind` is typed as `string` on the wire.
 */
export function legatoHref(navigation: { kind: string; target: string }): string | null {
  switch (navigation.kind) {
    case "plan":
      return `/plans/${encodeURIComponent(navigation.target)}`
    case "review":
      return `/review/${encodeURIComponent(navigation.target)}`
    case "workflow_run":
      return `/workflows/runs/${encodeURIComponent(navigation.target)}`
    case "task":
      // The board picks the task out wherever it is, the Away lane too.
      return `/board?highlight_task=${encodeURIComponent(navigation.target)}`
    case "approval":
      return `/attention?approval=${encodeURIComponent(navigation.target)}`
    case "settings":
      return `/settings#${encodeURIComponent(navigation.target)}`
    case "agents":
      // The Agents page, where interrupted agents are listed.
      return "/agents"
    default:
      return null
  }
}

/**
 * The address of an `external` navigation (an integration's own page, such
 * as a case in Argus), or null for any other kind or a target that is not an
 * http(s) address.
 */
export function externalHref(navigation: { kind: string; target: string }): string | null {
  if (navigation.kind !== "external") return null
  try {
    const url = new URL(navigation.target)
    return url.protocol === "https:" || url.protocol === "http:" ? url.href : null
  } catch {
    return null
  }
}
