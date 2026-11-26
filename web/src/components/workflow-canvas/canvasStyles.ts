import type { Tone } from "../../lib/workflowView"

// cardLook is a card's border and fill for a tone.
export const cardLook: Record<Tone, string> = {
  live: "border-lw-live/30 bg-lw-live-soft",
  needs: "border-lw-needs/30 bg-lw-needs-soft",
  done: "border-lw-done/30 bg-lw-done-soft",
  error: "border-lw-error/30 bg-lw-error-soft",
  idle: "border-lw-line bg-lw-surface",
}

// cardTitle is the look of a card's title.
export const cardTitle = "flex items-baseline gap-2 text-xs font-semibold uppercase tracking-wide text-lw-text-2"

// floatingPanel is the look of a side panel docked over the graph.
export const floatingPanel = "max-h-full overflow-y-auto shadow-[var(--shadow-lw-panel)]"

// redact hides each secret value in a text.
export function redact(text: string, secrets: string[]): string {
  return secrets.reduce((current, secret) => current.split(secret).join("••••••"), text)
}
