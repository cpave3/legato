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

// Surface finishes are shared recipes, separate from the source's reading/ANSI
// palette. Match this bytewise sRGB blend in the native asset adapter.
function mixColor(base: string, tint: string, amount: number): string {
  const bytes = [1, 3, 5].map(offset => {
    const a = parseInt(base.slice(offset, offset + 2), 16)
    const b = parseInt(tint.slice(offset, offset + 2), 16)
    return Math.round(a + (b - a) * amount).toString(16).padStart(2, "0")
  })
  return `#${bytes.join("").toUpperCase()}`
}

function themeMaterial(tokens: ThemeTokens, light: boolean, original: ThemeTokens) {
  const recipe = palettes.materials[light ? "light" : "dark"]
  const resolve = (role: string) => role.startsWith("#") ? role : tokens[role as keyof ThemeTokens]
  const colors = Object.fromEntries(Object.entries(recipe.colors).map(([role, mix]) => {
    return [role, mixColor(resolve(mix.base), resolve(mix.tint), mix.amount)]
  }))
  const contract = palettes.material_ink_contract
  const backgrounds = [tokens.canvas, tokens.selection, ...Object.entries(colors).filter(([role]) => role !== "divider").map(([, value]) => value)]
  const inks = { ...tokens }
  for (const role of contract.roles as (keyof ThemeTokens)[]) {
    for (let step = 0; step <= Math.round(1 / contract.blend_step); step++) {
      const ink = mixColor(original[role], tokens.text, Math.min(1, step * contract.blend_step))
      if (backgrounds.every(background => themeContrast(ink, background) >= contract.minimum_contrast)) {
        inks[role] = ink
        break
      }
    }
  }
  const shadow = recipe.shadow
  const ink = resolve(shadow.ink)
  const alpha = (color: string, opacity: number) => `${color}${Math.round(opacity * 255).toString(16).padStart(2, "0")}`
  const edge = `inset 0 1px 0 ${alpha("#FFFFFF", shadow.highlight)}`
  return {
    colors,
    inks,
    controlShadow: `${edge}, 0 1px 2px ${alpha(ink, shadow.contact)}`,
    panelShadow: `${edge}, 0 1px 2px ${alpha(ink, shadow.contact)}, 0 6px 18px -10px ${alpha(ink, shadow.ambient)}`,
  }
}

export type ThemeMaterial = ReturnType<typeof themeMaterial>

export const renderedThemes = palettes.themes.map(theme => {
  const tokens = readableTokens(theme.tokens)
  return { ...theme, tokens, material: themeMaterial(tokens, theme.polarity === "light", theme.tokens) }
})
