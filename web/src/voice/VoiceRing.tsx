import { useMediaQuery } from "../hooks/useMediaQuery"
import { useReeveName } from "../lib/reeveName"
import "./voice.css"
import { dialTicks, type RingMode } from "./ring"

export type { RingMode }

const LOOK: Record<RingMode, { fill: string; stroke: string; glyph: string; glow: string }> = {
  idle: { fill: "none", stroke: "#3A3946", glyph: "#6D5EF6", glow: "" },
  blocked: { fill: "rgba(232,181,58,0.08)", stroke: "#E8B53A", glyph: "#E8B53A", glow: "" },
  listening: { fill: "rgba(109,94,246,0.16)", stroke: "#6D5EF6", glyph: "#C9C0FF", glow: "rgba(109,94,246,0.45)" },
  thinking: { fill: "rgba(138,108,255,0.08)", stroke: "#8A6CFF", glyph: "#A99CFF", glow: "rgba(138,108,255,0.25)" },
  speaking: { fill: "rgba(62,207,142,0.14)", stroke: "#3ECF8E", glyph: "#B8F0D6", glow: "rgba(62,207,142,0.32)" },
}

// VoiceRing is the Reeve's voice as the seal dial of the signed approval:
// 60 ticks round a hexagon, in five modes. It keeps still when the person
// asks for reduced motion, and keeps its colours.
export function VoiceRing({ mode, size = 88, level = 0, label }: { mode: RingMode; size?: number; level?: number; label?: string }) {
  const reduced = useMediaQuery("(prefers-reduced-motion: reduce)")
  const reeve = useReeveName()
  const look = LOOK[mode]
  const c = size / 2
  const hr = c * 0.4
  const hexagon = Array.from({ length: 6 }, (_, k) => {
    const a = (k * Math.PI) / 3 - Math.PI / 2
    return `${(c + hr * Math.cos(a)).toFixed(1)},${(c + hr * Math.sin(a)).toFixed(1)}`
  }).join(" ")
  const arcR = c * 0.7
  const circ = 2 * Math.PI * arcR
  return (
    <svg className="voice-ring" data-mode={mode} data-motion={reduced ? "reduced" : "full"} viewBox={`0 0 ${size} ${size}`}
      width={size} height={size} role="img" aria-label={label ?? `${reeve.start}'s voice: ${mode === "blocked" ? "not ready" : mode}`}
      style={{ overflow: "visible", flex: "none" }}>
      <defs>
        <radialGradient id={`voice-glow-${mode}`}><stop offset="0" stopColor={look.glow || "transparent"} /><stop offset="0.68" stopColor="transparent" /></radialGradient>
      </defs>
      {look.glow && <circle className="glow" cx={c} cy={c} r={c * 1.1} fill={`url(#voice-glow-${mode})`} />}
      {dialTicks(mode, size, level).map((t, i) => (
        <line key={i} className="tick" x1={t.x1} y1={t.y1} x2={t.x2} y2={t.y2} stroke={t.color} strokeOpacity={t.opacity}
          strokeWidth={t.width} strokeLinecap="round" style={t.delay ? { animationDelay: `${t.delay.toFixed(2)}s` } : undefined} />
      ))}
      {mode === "thinking" && (
        <circle className="arc" data-testid="voice-arc" cx={c} cy={c} r={arcR} fill="none" stroke="#8A6CFF" strokeWidth={Math.max(3, size * 0.035)}
          strokeLinecap="round" strokeDasharray={`${circ * 0.3} ${circ}`} />
      )}
      <polygon points={hexagon} fill={look.fill} stroke={look.stroke} strokeWidth={size > 100 ? 3 : size > 60 ? 2 : 1.5} />
      <text x={c} y={c} textAnchor="middle" dominantBaseline="central" fill={look.glyph}
        style={{ fontFamily: "Geist Mono, ui-monospace, monospace", fontWeight: 600, fontSize: Math.round(size * 0.22) }}>
        {mode === "blocked" ? "!" : "◈"}
      </text>
    </svg>
  )
}
