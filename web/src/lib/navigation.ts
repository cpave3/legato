/** Navigation kinds shared with the attention registry and task-activity. */
export type LegatoNavigationKind = "plan" | "review" | "workflow_run" | "task"

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
      return "/board"
    default:
      return null
  }
}
