// The colours a Legato can be tinted (service.Tints on the server), so the
// web UI shows at a glance which server it is on. Bright enough to read on
// the dark theme.
export const tints = [
  { name: "red", label: "Red", color: "#ef4444" },
  { name: "orange", label: "Orange", color: "#f97316" },
  { name: "yellow", label: "Yellow", color: "#eab308" },
  { name: "green", label: "Green", color: "#22c55e" },
  { name: "teal", label: "Teal", color: "#14b8a6" },
  { name: "blue", label: "Blue", color: "#3b82f6" },
  { name: "purple", label: "Purple", color: "#a855f7" },
  { name: "pink", label: "Pink", color: "#ec4899" },
] as const

// The colour of a tint; undefined for none, or one this page does not know.
export function tintColor(name?: string | null): string | undefined {
  return tints.find((tint) => tint.name === name)?.color
}

// The colour of a tint at an opacity, for a wash over a surface.
export function tintWash(color: string, alpha: number): string {
  const value = parseInt(color.slice(1), 16)
  return `rgba(${(value >> 16) & 255}, ${(value >> 8) & 255}, ${value & 255}, ${alpha})`
}
