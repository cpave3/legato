import type { Tone } from "./workflowView"

// The colours of the board's five states, as the workflow screens use them.
export const toneText: Record<Tone, string> = {
  live: "text-lw-live",
  needs: "text-lw-needs",
  done: "text-lw-done",
  error: "text-lw-error",
  idle: "text-lw-text-2",
}

export const toneGlyph: Record<Tone, string> = { live: "●", needs: "◆", done: "✓", error: "✕", idle: "–" }
