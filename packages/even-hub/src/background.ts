import { stateOf } from "./glance"
import { CANVAS_WIDTH, GAP, MARGIN, PADDING, right } from "./layout"
import type { Agent } from "./legato"

// Background mode: the page folds away to one row across the top of the
// display, so the operator keeps the agents' state in sight while doing
// something else. On the left, bright, who needs the operator (empty when no
// one does); on the right, dim, how many are running, in the background and
// idle, and the glasses' battery.
export type BackgroundPage = { alert: string; counts: string }

// The row's two boxes, padded as every box is, filling the width between the
// margins with a gap between them.
export const ALERT_WIDTH = 280
export const COUNTS_WIDTH = CANVAS_WIDTH - 2 * MARGIN - GAP - ALERT_WIDTH
export const ALERT_TEXT_WIDTH = ALERT_WIDTH - 2 * PADDING
export const COUNTS_TEXT_WIDTH = COUNTS_WIDTH - 2 * PADDING


const counted = (marks: [string, number][]) =>
  marks
    .filter(([, n]) => n > 0)
    .map(([mark, n]) => `${mark} ${n}`)
    .join("  ")

// The glasses' battery runs low at this level, unless charging.
export const LOW_BATTERY = 20

export type BackgroundOptions = {
  // Why the list may be stale.
  problem?: string
  // The glasses' battery, 0–100, when the Even app has said.
  battery?: number
  charging?: boolean
}

// The row for the agents Legato listed, and the glasses' battery. The Reeve
// counts only when it needs the operator; the battery goes on the bright
// side when it runs low.
export function background(agents: Agent[], options: BackgroundOptions = {}): BackgroundPage {
  const running = agents.filter((a) => a.status === "running")
  const count = (activity: string, all = false) =>
    running.filter((a) => stateOf(a).activity === activity && (all || !a.reeve)).length
  const battery = options.battery === undefined ? "" : `G2 ${Math.round(options.battery)}%`
  const low = battery && options.battery! <= LOW_BATTERY && !options.charging
  const alert = [
    options.problem
      ? "▲ can't read"
      : counted([
          ["●", count("waiting", true)],
          ["▲", count("error", true)],
        ]),
    low ? `▲ ${battery}` : "",
  ]
    .filter(Boolean)
    .join("  ")
  // "no agents" only when the bright side says nothing either.
  const agentCounts =
    counted([
      ["▶", count("working")],
      ["▷", count("background")],
      ["○", count("")],
    ]) || (alert ? "" : "no agents")
  const counts = [agentCounts, low ? "" : battery].filter(Boolean).join("  ·  ")
  return { alert, counts: right(counts, COUNTS_TEXT_WIDTH) }
}
