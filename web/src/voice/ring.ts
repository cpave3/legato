// The ring's modes: the voice states, and blocked (not ready).
export type RingMode = "idle" | "listening" | "thinking" | "speaking" | "blocked"

const ACCENT = [109, 94, 246]
const GREEN = [62, 207, 142]

function mix(t: number): string {
  return "rgb(" + ACCENT.map((v, i) => Math.round(v + (GREEN[i] - v) * t)).join(",") + ")"
}

export interface Tick {
  x1: number
  y1: number
  x2: number
  y2: number
  color: string
  opacity: number
  width: number
  delay: number
}

// A tick's own variation, so the ring is not uniform (the canvas's).
function variation(i: number): number {
  return 0.5 + 0.5 * Math.sin(i * 0.9) * Math.cos(i * 0.31)
}

// dialTicks lays out the 60 ticks of the seal dial for a mode. While
// listening each tick's length follows the microphone's level (0 to 1).
export function dialTicks(mode: RingMode, size: number, level = 0): Tick[] {
  const c = size / 2
  const R = c * 0.94
  const width = Math.max(2, Math.round(size * 0.02))
  const ticks: Tick[] = []
  for (let i = 0; i < 60; i++) {
    const long = i % 5 === 0
    const lv = variation(i)
    let len = Math.max(3, size * (long ? 0.11 : 0.075))
    let color = long ? "#3A3946" : "#2A2A33"
    let opacity = 1
    let delay = 0
    switch (mode) {
      case "idle":
        delay = (i % 12) * 0.12
        break
      case "blocked":
        color = long ? "#4A3F22" : "#2E2A1F"
        break
      case "listening": {
        const loud = Math.min(1, Math.max(0, level))
        color = lv > 0.55 ? "#8A6CFF" : "#6D5EF6"
        opacity = 0.35 + 0.65 * Math.min(1, loud * (0.6 + 0.8 * lv) + 0.15)
        len *= 0.5 + Math.min(1, loud * (0.5 + lv))
        break
      }
      case "thinking":
        color = long ? "#4A4560" : "#33303F"
        break
      case "speaking":
        color = mix(i / 59)
        delay = ((i * 7) % 13) * 0.06
        break
    }
    const a = (i / 60) * 2 * Math.PI - Math.PI / 2
    const outer = R
    const inner = R - len
    ticks.push({
      x1: c + inner * Math.cos(a), y1: c + inner * Math.sin(a),
      x2: c + outer * Math.cos(a), y2: c + outer * Math.sin(a),
      color, opacity, width, delay,
    })
  }
  return ticks
}

