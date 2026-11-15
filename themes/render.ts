import palettes from "./palettes.json"

export type ThemeTokens = (typeof palettes.themes)[number]["tokens"]

function luminance(hex: string): number {
  const [r, g, b] = [1, 3, 5].map(i => parseInt(hex.slice(i, i + 2), 16) / 255)
    .map(v => v <= .04045 ? v / 12.92 : ((v + .055) / 1.055) ** 2.4)
  return r * .2126 + g * .7152 + b * .0722
}
export function themeContrast(a: string, b: string): number {
  const values = [luminance(a), luminance(b)].sort((a, b) => a - b)
  return (values[1] + .05) / (values[0] + .05)
}

// Use the palette's own foreground when a secondary text role is too faint.
// Ocular's exact candidate values pass unchanged. Dracula's original comment
// colour stays in the source for provenance, but is not used as small UI text.
export function readableTokens(tokens: ThemeTokens): ThemeTokens {
  const t = { ...tokens }
  for (const role of ["muted", "comment"] as const) {
    if (themeContrast(t[role], t.canvas) < 4.5) t[role] = t.text
  }
  if (themeContrast(t.border, t.selection) < 3) t.border = t.focus
  return t
}

export const renderedThemes = palettes.themes.map(theme => ({ ...theme, tokens: readableTokens(theme.tokens) }))
