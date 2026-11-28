import type { UITone } from "./agentUi"

// The classes Legato draws an agent's component cards with: a tone's text
// and badge colours, and what each heading level of a card becomes.

export const TONE_TEXT: Record<UITone, string> = {
  neutral: "text-zinc-100",
  positive: "text-emerald-300",
  warning: "text-amber-300",
  negative: "text-red-300",
  info: "text-sky-300",
}

export const TONE_BADGE: Record<UITone, string> = {
  neutral: "border-zinc-600 bg-zinc-800 text-zinc-300",
  positive: "border-emerald-800 bg-emerald-950/60 text-emerald-300",
  warning: "border-amber-800 bg-amber-950/60 text-amber-300",
  negative: "border-red-800 bg-red-950/60 text-red-300",
  info: "border-sky-800 bg-sky-950/60 text-sky-300",
}

export function tone(value: UITone | undefined): UITone {
  return value && value in TONE_TEXT ? value : "neutral"
}

// A card's own "level 1" is the top of the card, not of the page: the
// conversation it sits in already has the page's headings above it.
export const HEADING: Record<number, { Tag: "h2" | "h3" | "h4"; className: string }> = {
  1: { Tag: "h2", className: "text-base font-semibold text-zinc-100" },
  2: { Tag: "h3", className: "text-sm font-semibold text-zinc-100" },
  3: { Tag: "h4", className: "text-xs font-semibold uppercase tracking-wide text-zinc-400" },
}

